//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// RunProtectedWorker dispatches only fixed installation/capture roles. It
// never accepts SQL, a command path, environment additions or source paths.
// Both the runtime binary and the offline tool use this same implementation.
func RunProtectedWorker(ctx context.Context, args []string, output io.Writer) (bool, int) {
	if len(args) == 0 || !strings.HasPrefix(args[0], "__") {
		return false, 0
	}
	role := args[0]
	if strings.HasPrefix(role, restoreWorkerRole) {
		if len(args) != 2 || output == nil || HardenProtectedProfileWorker() != nil {
			return true, 3
		}
		if serveRestoreWorker(ctx, args[1], role, os.Stdin, output) != nil {
			return true, 5
		}
		return true, 0
	}
	if role != "__capture-server-v1" && role != "__bootstrap-profile-v1" && role != "__check-profile-v1" && role != "__capture-catalog-v1" && !strings.HasPrefix(role, "__bootstrap-worktree-v1:") && !strings.HasPrefix(role, "__capture-store-v1:") {
		return false, 0
	}
	if len(args) != 2 || output == nil || HardenProtectedProfileWorker() != nil {
		return true, 3
	}
	if role == "__capture-server-v1" {
		if serveProfileCaptures(ctx, args[1], os.Stdin, output) != nil {
			return true, 5
		}
		return true, 0
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result any
	var err error
	switch {
	case role == "__bootstrap-profile-v1" || role == "__check-profile-v1":
		result, err = ProtectedProfileWorker(ctx, args[1], role == "__bootstrap-profile-v1")
	case role == "__capture-catalog-v1":
		result, err = captureWorktreeWorker(ctx, args[1], "", "catalog")
	case strings.HasPrefix(role, "__bootstrap-worktree-v1:"):
		worktree := strings.TrimPrefix(role, "__bootstrap-worktree-v1:")
		if _, err = profileUUIDBytes(worktree); err == nil {
			result, err = bootstrapWorktreeWorker(ctx, args[1], worktree)
		}
	default:
		parts := strings.Split(strings.TrimPrefix(role, "__capture-store-v1:"), ":")
		if len(parts) != 2 || parts[0] != "index" && parts[0] != "records" {
			err = ErrProfileInput
		} else if _, err = profileUUIDBytes(parts[1]); err == nil {
			result, err = captureWorktreeWorker(ctx, args[1], parts[1], parts[0])
		}
	}
	if err != nil || json.NewEncoder(output).Encode(result) != nil {
		return true, 5
	}
	return true, 0
}

func (p *protectedProfile) captureWorker(ctx context.Context, worktree, name string) (nativeWALCapture, error) {
	if p.Check(ctx) != nil {
		return nativeWALCapture{}, ErrProfileDenied
	}
	kind := strings.TrimSuffix(name, ".sqlite")
	if name != "index.sqlite" && name != "records.sqlite" && !(name == "catalog.sqlite" && worktree == "") {
		return nativeWALCapture{}, ErrProfileInput
	}
	p.captureOnce.Do(func() { p.captureClient, p.captureError = startProfileCaptureClient(ctx, p) })
	if p.captureError != nil {
		return nativeWALCapture{}, p.captureError
	}
	out, err := p.captureClient.capture(ctx, profileCaptureRequest{Kind: kind, WorktreeID: worktree})
	return out, errors.Join(err, p.Check(ctx))
}

func captureWorktreeWorker(ctx context.Context, rootPath, worktree, kind string) (nativeWALCapture, error) {
	p, err := openProtectedProfile(ctx, rootPath, "READY")
	if err != nil {
		return nativeWALCapture{}, err
	}
	defer p.Close()
	_, digest, err := trustedProfileExecutable(ctx)
	if err != nil || digest != p.manifest.BootstrapExecutableSHA256 {
		return nativeWALCapture{}, ErrProfileDenied
	}
	return captureWithProfile(ctx, p, worktree, kind)
}

func captureWithProfile(ctx context.Context, p *protectedProfile, worktree, kind string) (nativeWALCapture, error) {
	if err := p.Check(ctx); err != nil {
		return nativeWALCapture{}, err
	}
	if kind == "catalog" && worktree == "" {
		lease, err := runtime.AcquireLifecycle(ctx, p.state, "catalog", ports.SharedLease, 5*time.Second)
		if err != nil {
			return nativeWALCapture{}, err
		}
		defer lease.Close()
		return captureNativeWALImage(ctx, p.domain, lease, "catalog", "catalog.sqlite")
	}
	control, err := p.root.OpenDirectory("control/worktrees/" + worktree)
	if err != nil {
		return nativeWALCapture{}, err
	}
	defer control.Close()
	m, _, err := readWorktreeManifest(ctx, control, p.manifest.ProfileID, worktree)
	if err != nil || m.Stage != "READY" {
		return nativeWALCapture{}, ErrProfileIncomplete
	}
	domain, err := p.domain.Child(ctx, "worktrees/"+worktree)
	if err != nil {
		return nativeWALCapture{}, err
	}
	defer domain.root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, domain.root, worktree, ports.SharedLease, 5*time.Second)
	if err != nil {
		return nativeWALCapture{}, err
	}
	defer lease.Close()
	return captureNativeWALImage(ctx, domain, lease, worktree, kind+".sqlite")
}

func bootstrapWorktreeWorker(ctx context.Context, rootPath, worktree string) (out ProtectedWorktreeSummary, resultErr error) {
	p, err := openProtectedProfile(ctx, rootPath, "READY")
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, p.Close()) }()
	_, executableHash, err := trustedProfileExecutable(ctx)
	if err != nil || executableHash != p.manifest.BootstrapExecutableSHA256 {
		return out, ErrProfileDenied
	}
	control, err := p.root.OpenDirectory("control/worktrees/" + worktree)
	if err != nil {
		return out, err
	}
	defer control.Close()
	m, digest, err := readWorktreeManifest(ctx, control, p.manifest.ProfileID, worktree)
	if err != nil || m.Stage != "CREATING" {
		return out, ErrProfileIncomplete
	}
	domain, err := p.domain.Child(ctx, "worktrees/"+worktree)
	if err != nil {
		return out, err
	}
	defer domain.root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, domain.root, worktree, ports.ExclusiveLease, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer lease.Close()
	job, err := profileUUID()
	if err != nil {
		return out, err
	}
	writer, err := runtime.AcquireWriter(ctx, lease, job, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer writer.Close()
	check := func(ctx context.Context) error {
		_, current, err := readWorktreeManifest(ctx, control, p.manifest.ProfileID, worktree)
		if err != nil || current != digest {
			return ErrProfileDenied
		}
		return p.Check(ctx)
	}
	for _, kind := range []string{"index", "records"} {
		name := kind + ".sqlite"
		observation, err := domain.Observe(ctx, lease, worktree, name, checkpointedSnapshotMaxBytes)
		if err != nil {
			return out, err
		}
		fresh := true
		for i, item := range observation.files {
			if i == 0 {
				info, err := item.file.Stat()
				fresh = fresh && err == nil && info.Size() == 0
			} else {
				fresh = fresh && item.file == nil
			}
		}
		if err := observation.Close(); err != nil || !fresh {
			return out, ErrProfileIncomplete
		}
		if err := check(ctx); err != nil {
			return out, err
		}
		db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(worktreeStatePath(rootPath, worktree), name)))
		if err != nil {
			return out, err
		}
		db.SetMaxOpenConns(1)
		owner, err := OpenMaintenanceOwner(ctx, lease, db, worktree)
		if err != nil {
			return out, errors.Join(err, db.Close())
		}
		incarnation := ""
		if kind == "index" {
			identity, initErr := owner.InitializeIndex(ctx, writer, job)
			incarnation, err = identity.Incarnation, initErr
			out.IndexIncarnation = incarnation
		} else {
			identity, initErr := owner.InitializeRecords(ctx, writer, job)
			incarnation, err = identity.Incarnation, initErr
			out.RecordsIncarnation = incarnation
		}
		if err = errors.Join(err, owner.Close()); err != nil {
			return out, err
		}
		image, err := captureNativeWALImage(ctx, domain, lease, worktree, name)
		if err != nil || image.Prefix != (walCommitPrefixReceipt{}) {
			return out, ErrProfileIncomplete
		}
		journal, err := domain.Child(ctx, ".maintenance/"+kind)
		if err != nil {
			return out, err
		}
		headRoot, err := control.OpenDirectory(kind)
		if err != nil {
			_ = journal.root.Close()
			return out, err
		}
		identity, key, err := worktreeStoreIdentity(p.manifest, worktree, kind, incarnation, p.key)
		if err != nil {
			return out, errors.Join(err, journal.root.Close(), headRoot.Close())
		}
		store := &nativeWALStore{domain: domain, journal: journal, control: headRoot, lease: lease, scope: worktree, name: name, identity: identity, key: key, authorize: check,
			capture: func(context.Context) (nativeWALCapture, error) { return nativeWALCapture{}, ErrStateUnsupported }}
		log, head, err := store.openLog(ctx, &image.Image)
		if err == nil {
			err = errors.Join(log.Close(), head.Close())
		}
		clear(store.key[:])
		err = errors.Join(err, journal.root.Close(), headRoot.Close())
		if err != nil {
			return out, err
		}
	}
	out.WorktreeID, out.Verified = worktree, true
	return out, check(ctx)
}
