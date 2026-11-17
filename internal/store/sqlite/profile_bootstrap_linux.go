//go:build linux

package sqlite

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func profileNativeDSN(name string) string {
	u := url.URL{Scheme: "file", Path: name}
	q := url.Values{"mode": {"rw"}, "_pragma": {"foreign_keys=ON", "synchronous=FULL", "busy_timeout=5000", "temp_store=MEMORY"}}
	u.RawQuery = q.Encode()
	return u.String()
}

func profileMainImage(ctx context.Context, root *paths.Root, maxBytes int64) (image walLogicalImage, resultErr error) {
	f, err := root.OpenRegular("catalog.sqlite")
	if err != nil {
		return walLogicalImage{}, ErrProfileDenied
	}
	defer func() {
		resultErr = errors.Join(resultErr, f.Close())
		if resultErr != nil {
			image = walLogicalImage{}
		}
	}()
	if stateOwner(f, false) != nil {
		return walLogicalImage{}, ErrProfileDenied
	}
	var header [100]byte
	if err := readLogicalAt(ctx, f, header[:], 0); err != nil {
		return walLogicalImage{}, err
	}
	info, err := f.Stat()
	if err != nil {
		return walLogicalImage{}, err
	}
	return digestCheckpointedMain(ctx, f, info.Size(), binary.BigEndian.Uint32(header[28:32]), maxBytes)
}

// ProtectedProfileWorker is an internal role of the root-owned provisioning
// tool. The caller must drop UID/GID/groups before exec and harden every Go
// thread before calling it. It never accepts arbitrary SQL or target reads.
func ProtectedProfileWorker(ctx context.Context, rootPath string, bootstrap bool) (ProtectedProfileSummary, error) {
	stage := "READY"
	if bootstrap {
		stage = "CREATING"
	}
	p, err := openProtectedProfile(ctx, rootPath, stage)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	defer p.Close()
	_, digest, err := trustedProfileExecutable(ctx)
	if err != nil || digest != p.manifest.BootstrapExecutableSHA256 {
		return ProtectedProfileSummary{}, ErrProfileDenied
	}
	lease, err := runtime.AcquireLifecycle(ctx, p.state, "catalog", ports.ExclusiveLease, 5*time.Second)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	defer lease.Close()
	incarnation := p.manifest.CatalogIncarnation
	if bootstrap {
		observation, err := p.domain.Observe(ctx, lease, "catalog", "catalog.sqlite", checkpointedSnapshotMaxBytes)
		if err != nil {
			return ProtectedProfileSummary{}, err
		}
		fresh := len(observation.files) == 4
		for i, f := range observation.files {
			if i == 0 {
				info, e := f.file.Stat()
				fresh = fresh && e == nil && info.Size() == 0
			} else {
				fresh = fresh && f.file == nil
			}
		}
		closeErr := observation.Close()
		if !fresh || closeErr != nil {
			return ProtectedProfileSummary{}, ErrProfileIncomplete
		}
		// No database handle existed before this explicit fresh-only check.
		writer, err := runtime.AcquireCatalogMutation(ctx, lease, p.manifest.ProfileID, 5*time.Second)
		if err != nil {
			return ProtectedProfileSummary{}, err
		}
		db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(profileStatePath(rootPath), "catalog.sqlite")))
		if err != nil {
			_ = writer.Close()
			return ProtectedProfileSummary{}, err
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		owner, err := OpenMaintenanceOwner(ctx, lease, db, "catalog")
		if err != nil {
			_ = db.Close()
			_ = writer.Close()
			return ProtectedProfileSummary{}, err
		}
		incarnation, err = owner.InitializeCatalog(ctx, writer, p.manifest.ProfileID)
		err = errors.Join(err, owner.Close(), writer.Close())
		if err != nil {
			return ProtectedProfileSummary{}, ErrProfileIncomplete
		}
		// The closed native pool has checkpointed. Ordinary inode reads now
		// cannot remove this process's SQLite POSIX locks: none remain.
		image, err := profileMainImage(ctx, p.state, checkpointedSnapshotMaxBytes)
		if err != nil {
			return ProtectedProfileSummary{}, err
		}
		log, head, err := p.openLog(ctx, incarnation, &image)
		if err != nil {
			return ProtectedProfileSummary{}, err
		}
		if err := errors.Join(log.Close(), head.Close()); err != nil {
			return ProtectedProfileSummary{}, err
		}
	}
	if err := verifyProtectedCatalog(ctx, p, lease, incarnation); err != nil {
		return ProtectedProfileSummary{}, err
	}
	return ProtectedProfileSummary{ProfileID: p.manifest.ProfileID, Stage: stage, CatalogIncarnation: incarnation, CatalogVerified: true, TrustedHead: true}, ctx.Err()
}

func verifyProtectedCatalog(ctx context.Context, p *protectedProfile, lease *runtime.Lifecycle, incarnation string) (resultErr error) {
	log, head, err := p.openLog(ctx, incarnation, nil)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, log.Close(), head.Close()) }()
	proof := func(ctx context.Context, digest [32]byte) error {
		if p.Check(ctx) != nil {
			return ErrProfileDenied
		}
		receipt, err := log.Head(ctx)
		if err != nil {
			return err
		}
		if receipt.Image.SHA256 != digest {
			return ErrStateChanged
		}
		return nil
	}
	snapshot, err := OpenCheckpointedSnapshot(ctx, p.state, p.targets, lease, "catalog", "catalog.sqlite", checkpointedSnapshotMaxBytes, p.Check, proof)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, snapshot.Close()) }()
	actual, err := catalog.VerifyReadOnlyImage(ctx, snapshot.db)
	if err != nil || actual != incarnation {
		return ErrProfileIncomplete
	}
	return snapshot.verifySource(ctx)
}

// The administrator verifies only fixed bootstrap artifacts after the worker
// exits. CREATING profiles cannot be served, and another bootstrap refuses a
// nonempty DB. No SQLite connection or source repository is opened as root.
func verifyProfileBootstrap(ctx context.Context, root *paths.Root, m protectedProfileManifest, masterKey []byte) error {
	state, err := root.OpenDirectory("home/.local/state/mce4d")
	if err != nil {
		return err
	}
	defer state.Close()
	main, err := state.OpenRegular("catalog.sqlite")
	if err != nil {
		return err
	}
	var st unix.Stat_t
	statErr := unix.Fstat(int(main.Fd()), &st)
	h := sha256.New()
	n, readErr := io.Copy(h, io.LimitReader(main, checkpointedSnapshotMaxBytes+1))
	closeErr := main.Close()
	if errors.Join(statErr, readErr, closeErr) != nil || st.Uid != m.EngineUID || st.Gid != m.EngineGID || st.Mode&0077 != 0 || n > checkpointedSnapshotMaxBytes {
		return ErrProfileIncomplete
	}
	mac := hmac.New(sha256.New, masterKey)
	_, _ = mac.Write([]byte("MCE4D-PROFILE-CATALOG1\x00" + m.ProfileID))
	var key [32]byte
	copy(key[:], mac.Sum(nil))
	defer clear(key[:])
	a, err := profileUUIDBytes(m.CatalogAuthorityID)
	if err != nil {
		return err
	}
	inc, err := profileUUIDBytes(m.CatalogIncarnation)
	if err != nil {
		return err
	}
	identity := walAuthorityIdentity{Authority: a, Incarnation: inc, Store: sha256.Sum256([]byte("MCE4D-PROFILE-STORE1\x00" + m.ProfileID + "\x00catalog"))}
	journalRoot, err := state.OpenDirectory(".maintenance/catalog")
	if err != nil {
		return err
	}
	defer journalRoot.Close()
	f, err := journalRoot.OpenRegular(walAuthorityName)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, walAuthorityRecordSize+1))
	closeErr = f.Close()
	if errors.Join(readErr, closeErr) != nil || len(raw) != walAuthorityRecordSize {
		return ErrProfileIncomplete
	}
	decoder := walAuthorityLog{identity: identity, key: key}
	record, err := decoder.decode(raw)
	if err != nil || decoder.apply(record, true) != nil || !hmac.Equal(record.Image.SHA256[:], h.Sum(nil)) {
		return ErrProfileIncomplete
	}
	control, err := root.OpenDirectory("control/catalog")
	if err != nil {
		return err
	}
	defer control.Close()
	f, err = control.OpenRegular(walHeadName)
	if err != nil {
		return err
	}
	flags, flagErr := unix.IoctlGetInt(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	headBytes, readErr := io.ReadAll(io.LimitReader(f, walHeadRecordSize+1))
	closeErr = f.Close()
	encoder := walHeadAuthority{identity: identity, key: key}
	expected := encoder.encode(walJournalPosition{Size: walAuthorityRecordSize, SHA256: sha256.Sum256(raw)})
	if errors.Join(flagErr, readErr, closeErr) != nil || flags&linuxAppendFlag == 0 || !hmac.Equal(headBytes, expected) {
		return ErrProfileIncomplete
	}
	return ctx.Err()
}

// EncodeProfileSummary never serializes profile paths, target roots or keys.
func EncodeProfileSummary(w io.Writer, s ProtectedProfileSummary) error {
	return json.NewEncoder(w).Encode(s)
}
