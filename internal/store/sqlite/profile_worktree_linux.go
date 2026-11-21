//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

type protectedWorktreeManifest struct {
	FormatVersion         int    `json:"format_version"`
	ProfileInstallationID string `json:"profile_installation_id"`
	WorktreeID            string `json:"worktree_id"`
	Stage                 string `json:"stage"`
	IndexIncarnation      string `json:"index_incarnation"`
	RecordsIncarnation    string `json:"records_incarnation"`
}

func readWorktreeManifest(ctx context.Context, root *paths.Root, installation, worktree string) (protectedWorktreeManifest, [32]byte, error) {
	return readWorktreeManifestForMaintenance(ctx, root, installation, worktree, "")
}

func readWorktreeManifestForMaintenance(ctx context.Context, root *paths.Root, installation, worktree, operation string) (protectedWorktreeManifest, [32]byte, error) {
	var out protectedWorktreeManifest
	var zero [32]byte
	if rootManagedAncestors(ctx, root, true) != nil {
		return out, zero, ErrProfileDenied
	}
	f, err := root.OpenRegular("worktree.json")
	if err != nil {
		return out, zero, err
	}
	var st unix.Stat_t
	statErr := unix.Fstat(int(f.Fd()), &st)
	raw, readErr := io.ReadAll(io.LimitReader(f, profileManifestLimit+1))
	closeErr := f.Close()
	if errors.Join(statErr, readErr, closeErr) != nil || st.Uid != 0 || st.Mode&0022 != 0 || st.Nlink != 1 || model.ValidateWireJSON(raw, profileManifestLimit) != nil {
		return out, zero, ErrProfileDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || out.FormatVersion != 1 || out.ProfileInstallationID != installation || out.WorktreeID != worktree {
		return out, zero, ErrProfileDenied
	}
	if _, err := profileUUIDBytes(out.WorktreeID); err != nil {
		return out, zero, err
	}
	if out.Stage == "READY" {
		if _, err := profileUUIDBytes(out.IndexIncarnation); err != nil {
			return out, zero, err
		}
		if _, err := profileUUIDBytes(out.RecordsIncarnation); err != nil {
			return out, zero, err
		}
	} else if out.Stage != "CREATING" || out.IndexIncarnation != "" || out.RecordsIncarnation != "" {
		return out, zero, ErrProfileDenied
	}
	if err := checkMaintenanceGate(ctx, root, installation, worktree, operation); err != nil {
		return out, zero, err
	}
	return out, sha256.Sum256(raw), ctx.Err()
}

func worktreeStoreIdentity(m protectedProfileManifest, worktree, kind, incarnation string, catalogKey [32]byte) (walAuthorityIdentity, [32]byte, error) {
	var key [32]byte
	a, err := profileUUIDBytes(m.CatalogAuthorityID)
	if err != nil {
		return walAuthorityIdentity{}, key, err
	}
	inc, err := profileUUIDBytes(incarnation)
	if err != nil || kind != "index" && kind != "records" {
		return walAuthorityIdentity{}, key, ErrProfileInput
	}
	mac := hmac.New(sha256.New, catalogKey[:])
	_, _ = mac.Write([]byte("MCE4D-WORKTREE-STORE1\x00" + worktree + "\x00" + kind))
	copy(key[:], mac.Sum(nil))
	identity := walAuthorityIdentity{Authority: a, Incarnation: inc, Store: sha256.Sum256([]byte("MCE4D-PROFILE-STORE1\x00" + m.ProfileID + "\x00" + worktree + "\x00" + kind))}
	return identity, key, nil
}

// ProtectedWorktreeOwner owns native pools and their shared DB lifecycle.
// It is not a registration, target permission grant or index build request.
// A trusted caller must authorize the exact worktree/profile on every use.
type ProtectedWorktreeOwner struct {
	profile   *protectedProfile
	domain    *stateDomain
	control   *paths.Root
	lease     *runtime.Lifecycle
	manifest  protectedWorktreeManifest
	digest    [32]byte
	stores    []*nativeWALStore
	quota     *cacheBudget
	Index     *IndexOwner
	Records   *RecordsOwner
	authorize func(context.Context) error
	closed    atomic.Bool
	closeMu   sync.Mutex
}

func (o *ProtectedWorktreeOwner) Check(ctx context.Context) error { return o.check(ctx) }

func (o *ProtectedWorktreeOwner) check(ctx context.Context) error {
	if o == nil || o.closed.Load() || o.authorize == nil {
		return ErrProfileDenied
	}
	if err := o.profile.Check(ctx); err != nil {
		return err
	}
	if err := o.domain.Check(ctx); err != nil {
		return err
	}
	m, digest, err := readWorktreeManifest(ctx, o.control, o.profile.manifest.ProfileID, o.manifest.WorktreeID)
	if errors.Is(err, ErrStoreRecoveryRequired) {
		return err
	}
	if err != nil || digest != o.digest || m.Stage != "READY" {
		return ErrProfileDenied
	}
	return o.authorize(ctx)
}

func OpenProtectedWorktree(ctx context.Context, rootPath, worktree string, authorize func(context.Context) error) (_ *ProtectedWorktreeOwner, resultErr error) {
	return OpenProtectedWorktreeWithCacheLimit(ctx, rootPath, worktree, defaultWorktreeCacheBytes, authorize)
}

func OpenProtectedWorktreeWithCacheLimit(ctx context.Context, rootPath, worktree string, cacheBytes int64, authorize func(context.Context) error) (*ProtectedWorktreeOwner, error) {
	return openProtectedWorktree(ctx, rootPath, worktree, authorize, false, cacheBytes)
}

// RecoverProtectedWorktreePending is an explicitly authorized library action,
// not a transport/CLI restore endpoint. It can append ABORT only for an exact
// last-ACK image. Changed/unproven outcomes remain blocked with WAL preserved.
func RecoverProtectedWorktreePending(ctx context.Context, rootPath, worktree string, authorize func(context.Context) error) error {
	owner, err := openProtectedWorktree(ctx, rootPath, worktree, authorize, true, defaultWorktreeCacheBytes)
	if err != nil {
		return err
	}
	return owner.Close()
}

func openProtectedWorktree(ctx context.Context, rootPath, worktree string, authorize func(context.Context) error, recoverPending bool, cacheBytes int64) (_ *ProtectedWorktreeOwner, resultErr error) {
	if authorize == nil || cacheBytes < 1 || cacheBytes > 9007199254740991 {
		return nil, ErrProfileDenied
	}
	if _, err := profileUUIDBytes(worktree); err != nil {
		return nil, err
	}
	p, err := openProtectedProfile(ctx, rootPath, "READY")
	if err != nil {
		return nil, err
	}
	o := &ProtectedWorktreeOwner{profile: p, authorize: authorize}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, o.Close())
		}
	}()
	_, executableHash, err := trustedProfileExecutable(ctx)
	if err != nil || executableHash != p.manifest.BootstrapExecutableSHA256 {
		return nil, ErrProfileDenied
	}
	o.control, err = p.root.OpenDirectory("control/worktrees/" + worktree)
	if err != nil {
		return nil, err
	}
	o.manifest, o.digest, err = readWorktreeManifest(ctx, o.control, p.manifest.ProfileID, worktree)
	if errors.Is(err, ErrStoreRecoveryRequired) {
		return nil, err
	}
	if err != nil || o.manifest.Stage != "READY" {
		return nil, ErrProfileIncomplete
	}
	o.domain, err = p.domain.Child(ctx, "worktrees/"+worktree)
	if err != nil {
		return nil, err
	}
	if err := o.check(ctx); err != nil {
		return nil, err
	}
	o.lease, err = runtime.AcquireLifecycle(ctx, o.domain.root, worktree, ports.SharedLease, 5*time.Second)
	if err != nil {
		return nil, err
	}
	for _, item := range []struct{ kind, incarnation string }{{"index", o.manifest.IndexIncarnation}, {"records", o.manifest.RecordsIncarnation}} {
		journal, err := o.domain.Child(ctx, ".maintenance/"+item.kind)
		if err != nil {
			return nil, err
		}
		control, err := o.control.OpenDirectory(item.kind)
		if err != nil {
			_ = journal.root.Close()
			return nil, err
		}
		identity, key, err := worktreeStoreIdentity(p.manifest, worktree, item.kind, item.incarnation, p.key)
		if err != nil {
			_ = journal.root.Close()
			_ = control.Close()
			return nil, err
		}
		name := item.kind + ".sqlite"
		store := &nativeWALStore{domain: o.domain, journal: journal, control: control, lease: o.lease, scope: worktree, name: name, identity: identity, key: key, authorize: o.check,
			capture: func(ctx context.Context) (nativeWALCapture, error) { return p.captureWorker(ctx, worktree, name) }}
		o.stores = append(o.stores, store)
	}
	quotaRoot, err := o.domain.Child(ctx, ".maintenance")
	if err != nil {
		return nil, err
	}
	o.quota = &cacheBudget{owner: o, gate: quotaRoot, limit: cacheBytes}
	o.stores[0].quota = o.quota
	if recoverPending {
		for _, store := range o.stores {
			if err := store.recoverUnchangedPending(ctx); err != nil {
				return nil, err
			}
		}
	}
	o.Index, err = openNativeIndexOwner(ctx, o.stores[0])
	if err != nil {
		return nil, err
	}
	o.Records, err = openNativeRecordsOwner(ctx, o.stores[1])
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (o *ProtectedWorktreeOwner) Close() error {
	if o == nil {
		return ErrOwnerClosed
	}
	o.closeMu.Lock()
	defer o.closeMu.Unlock()
	if o.closed.Load() {
		return ErrOwnerClosed
	}
	var err error
	if o.Index != nil {
		err = errors.Join(err, o.Index.Close())
		if err == nil {
			o.Index = nil
		}
	}
	if o.Records != nil {
		closeErr := o.Records.Close()
		err = errors.Join(err, closeErr)
		if closeErr == nil {
			o.Records = nil
		}
	}
	if err != nil {
		return err // Retain roots/keys/lifecycle until every native pool closes.
	}
	if o.lease != nil {
		if err := o.lease.Close(); err != nil {
			return err
		}
		o.lease = nil
	}
	for _, store := range o.stores {
		clear(store.key[:])
		err = errors.Join(err, store.journal.root.Close(), store.control.Close())
	}
	if o.quota != nil {
		err = errors.Join(err, o.quota.gate.root.Close())
	}
	if o.domain != nil {
		err = errors.Join(err, o.domain.root.Close())
	}
	if o.control != nil {
		err = errors.Join(err, o.control.Close())
	}
	if o.profile != nil {
		err = errors.Join(err, o.profile.Close())
	}
	o.closed.Store(true)
	return err
}

func worktreeStatePath(root, worktree string) string {
	return filepath.Join(profileStatePath(root), "worktrees", worktree)
}
