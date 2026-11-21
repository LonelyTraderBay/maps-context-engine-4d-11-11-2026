package sqlite

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
)

const (
	windowsCatalogWALStage = "catalog-wal"
	windowsCatalogWALMain  = "catalog.sqlite"
	windowsCatalogHeadKey  = "wal-head.key"
)

type WindowsCatalogWALCheck struct {
	ProfileID           string `json:"profile_id"`
	Stage               string `json:"stage"`
	CatalogIncarnation  string `json:"catalog_incarnation"`
	HeadGenesisVerified bool   `json:"head_genesis_verified"`
}

// BootstrapWindowsCatalogWAL prepares a fresh native SQLite WAL file set.
// The controller owns this stage and its independent first head. It grants
// neither a live engine pool nor permission to restore an existing store.
func BootstrapWindowsCatalogWAL(ctx context.Context, root string, enginePID, clientPID uint32) (WindowsCatalogWALCheck, error) {
	return withWindowsProfileControl(ctx, root, func(m windowsProfilePreparation, files windowsMaintenanceFiles) (out WindowsCatalogWALCheck, resultErr error) {
		err := withVerifiedWindowsRoles(ctx, m, enginePID, clientPID, func(recheck func() error) error {
			var err error
			out, err = files.bootstrapCatalogWAL(ctx, m, recheck)
			return err
		})
		return out, err
	})
}

func (j windowsMaintenanceFiles) bootstrapCatalogWAL(ctx context.Context, m windowsProfilePreparation, recheckRoles func() error) (out WindowsCatalogWALCheck, resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	lease, err := runtime.AcquireLifecycle(ctx, j.control, "catalog", ports.ExclusiveLease, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	if err := j.requireAbsent(windowsCatalogHeadKey, walHeadName, windowsMaintenanceName); err != nil {
		return out, err
	}
	seed, err := j.readCatalogSeed(ctx, dir, m)
	if err != nil || !seed.CatalogVerified {
		return out, errors.Join(ErrProfileIncomplete, err)
	}
	image, found, err := j.readEntryLimit(ctx, dir, windowsCatalogSeedName, windowsCatalogSeedMaxSize)
	if err != nil || !found {
		return out, errors.Join(ErrProfileIncomplete, err)
	}
	if err := recheckRoles(); err != nil {
		return out, err
	}
	if err := createWindowsProfileDirectory(ctx, j.control, j.acl, windowsCatalogWALStage); err != nil {
		return out, err
	}
	staged, err := j.openCatalogWALStage()
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, staged.control.Close()) }()
	stageDir, err := staged.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, stageDir.Close()) }()
	descriptor, err := j.acl.descriptor()
	if err != nil {
		return out, err
	}
	if err := staged.publish(ctx, stageDir, descriptor, windowsCatalogWALMain, image, false, nil); err != nil {
		return out, err
	}
	path, err := staged.control.CanonicalPath()
	if err != nil {
		return out, err
	}
	if err := checkpointWindowsCatalogWAL(ctx, filepath.Join(path, windowsCatalogWALMain)); err != nil {
		return out, err
	}
	observed, err := staged.checkpointedCatalogWAL(ctx, stageDir, seed.CatalogIncarnation)
	if err != nil {
		return out, err
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return out, err
	}
	defer clear(key[:])
	receipt, head, err := windowsCatalogGenesis(m.ProfileID, seed.CatalogIncarnation, key, observed)
	if err != nil {
		return out, err
	}
	if err := staged.publish(ctx, stageDir, descriptor, walAuthorityName, receipt[:], false, nil); err != nil {
		return out, err
	}
	if err := j.publish(ctx, dir, descriptor, windowsCatalogHeadKey, key[:], false, nil); err != nil {
		return out, err
	}
	// Publish the head last. A partial file set is preserved and rejected.
	if err := errors.Join(recheckRoles(), lease.CheckRoot(ctx, j.control, "catalog", ports.ExclusiveLease)); err != nil {
		return out, err
	}
	if err := j.publish(ctx, dir, descriptor, walHeadName, head, false, nil); err != nil {
		return out, err
	}
	if err := j.checkCatalogWAL(ctx, dir, seed.CatalogIncarnation); err != nil {
		return out, err
	}
	return WindowsCatalogWALCheck{ProfileID: m.ProfileID, Stage: m.Stage,
		CatalogIncarnation: seed.CatalogIncarnation, HeadGenesisVerified: true}, nil
}

func checkpointWindowsCatalogWAL(ctx context.Context, file string) (resultErr error) {
	// The caller created and pinned the owner-only stage, installed the
	// verified catalog into its fixed name, and holds exclusive lifecycle.
	values := url.Values{"mode": {"rw"}, "_pragma": {"synchronous(FULL)", "busy_timeout(5000)", "foreign_keys(1)", "temp_store(MEMORY)"}}
	uri := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(file), RawQuery: values.Encode()}
	db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: uri.String()})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		return errors.Join(ErrProfileIncomplete, err)
	}
	var busy, logFrames, checkpointed int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil ||
		busy != 0 || logFrames != 0 || checkpointed != 0 {
		return errors.Join(ErrProfileIncomplete, err)
	}
	return ctx.Err()
}

func (j windowsMaintenanceFiles) requireAbsent(names ...string) error {
	for _, name := range names {
		f, err := j.control.OpenRegular(name)
		if err == nil {
			return errors.Join(ErrProfileExists, f.Close())
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (j windowsMaintenanceFiles) openCatalogWALStage() (windowsMaintenanceFiles, error) {
	root, err := j.control.OpenDirectory(windowsCatalogWALStage)
	return windowsMaintenanceFiles{control: root, acl: j.acl, profile: j.profile, scope: j.scope}, err
}

func (j windowsMaintenanceFiles) checkpointedCatalogWAL(ctx context.Context, dir *os.File, incarnation string) (walLogicalImage, error) {
	if err := j.requireAbsent(windowsCatalogWALMain+"-wal", windowsCatalogWALMain+"-shm", windowsCatalogWALMain+"-journal"); err != nil {
		return walLogicalImage{}, errors.Join(ErrProfileIncomplete, err)
	}
	image, present, err := j.readEntryLimit(ctx, dir, windowsCatalogWALMain, windowsCatalogSeedMaxSize)
	if err != nil || !present || len(image) < 100 {
		return walLogicalImage{}, errors.Join(ErrProfileIncomplete, err)
	}
	pages := binary.BigEndian.Uint32(image[28:32])
	observed, err := digestCheckpointedMain(ctx, bytes.NewReader(image), int64(len(image)), pages, windowsCatalogSeedMaxSize)
	if err != nil {
		return walLogicalImage{}, err
	}
	if err := verifyWindowsCatalogImage(ctx, image, incarnation); err != nil {
		return walLogicalImage{}, err
	}
	return observed, nil
}

func windowsCatalogGenesis(profile, incarnation string, key [32]byte, image walLogicalImage) (raw [walAuthorityRecordSize]byte, head []byte, err error) {
	identity, err := windowsCatalogAuthorityIdentity(profile, incarnation)
	if err != nil || key == [32]byte{} {
		return raw, nil, errors.Join(ErrProfileIncomplete, err)
	}
	raw, head = windowsWALGenesis(identity, key, image)
	return raw, head, nil
}

func windowsWALGenesis(identity walAuthorityIdentity, key [32]byte, image walLogicalImage) (raw [walAuthorityRecordSize]byte, head []byte) {
	raw = encodeWALAuthorityRecord(identity, key, [32]byte{}, walAuthorityRecord{
		kind: walAuthorityBootstrap, walAuthorityReceipt: walAuthorityReceipt{Image: image},
	})
	return raw, encodeWALHeadRecord(identity, key, [32]byte{},
		walJournalPosition{Size: walAuthorityRecordSize, SHA256: sha256.Sum256(raw[:])})
}

func windowsCatalogAuthorityIdentity(profile, incarnation string) (walAuthorityIdentity, error) {
	if !model.ValidUUID(profile) || !model.ValidUUID(incarnation) {
		return walAuthorityIdentity{}, ErrProfileIncomplete
	}
	var identity walAuthorityIdentity
	if _, err := hex.Decode(identity.Authority[:], []byte(strings.ReplaceAll(profile, "-", ""))); err != nil {
		return walAuthorityIdentity{}, err
	}
	if _, err := hex.Decode(identity.Incarnation[:], []byte(strings.ReplaceAll(incarnation, "-", ""))); err != nil {
		return walAuthorityIdentity{}, err
	}
	identity.Store = sha256.Sum256([]byte("MCE4D-PROFILE-STORE1\x00" + profile + "\x00catalog"))
	return identity, nil
}

func (j windowsMaintenanceFiles) checkCatalogWAL(ctx context.Context, dir *os.File, incarnation string) (resultErr error) {
	staged, err := j.openCatalogWALStage()
	if errors.Is(err, os.ErrNotExist) {
		return j.requireAbsent(windowsCatalogHeadKey, walHeadName)
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, staged.control.Close()) }()
	stageDir, err := staged.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, stageDir.Close()) }()
	identity, err := windowsCatalogAuthorityIdentity(j.profile, incarnation)
	if err != nil {
		return err
	}
	authority, err := readWindowsWALAuthority(ctx, j, dir, staged, stageDir, identity)
	if err != nil {
		return err
	}
	defer clear(authority.key[:])
	if authority.pending != nil {
		return ErrStoreRecoveryRequired
	}
	image, err := staged.checkpointedCatalogWAL(ctx, stageDir, incarnation)
	if err != nil {
		return err
	}
	if image != authority.head.Image {
		return ErrStoreRecoveryRequired
	}
	return errors.Join(ctx.Err(), j.control.Verify(), staged.control.Verify())
}
