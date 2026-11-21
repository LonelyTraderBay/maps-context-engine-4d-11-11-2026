package sqlite

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
)

type windowsWorktreeStage struct {
	FormatVersion int    `json:"format_version"`
	ProfileID     string `json:"profile_id"`
	Stage         string `json:"stage"`
	ProtectedWorktreeSummary
}

// ProvisionProtectedWorktree creates a fresh, controller-owned worktree
// staging pair after validating the registered scope in the protected catalog.
// The index and records images have separate receipts and head authorities.
func ProvisionProtectedWorktree(ctx context.Context, installation, worktree string) (ProtectedWorktreeSummary, error) {
	if !model.ValidUUID(worktree) {
		return ProtectedWorktreeSummary{}, ErrProfileInput
	}
	return withWindowsProfileControl(ctx, installation, func(m windowsProfilePreparation, control windowsMaintenanceFiles) (out ProtectedWorktreeSummary, resultErr error) {
		err := control.withStagedWorktreeRegistration(ctx, m, worktree, func() error {
			return withWindowsRoleIPC(ctx, m, control, windowsIPCProvisionWorktree, worktree, func(checkRoles func() error) error {
				var err error
				out, err = control.createStagedWorktree(ctx, worktree, checkRoles)
				return err
			})
		})
		return out, err
	})
}

func (j windowsMaintenanceFiles) withStagedWorktreeRegistration(ctx context.Context, m windowsProfilePreparation, worktree string, use func() error) (resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	pinned, err := j.control.OpenRegular("lifecycle.lock")
	if err != nil {
		return errors.Join(ErrProfileIncomplete, err)
	}
	defer func() { resultErr = errors.Join(resultErr, pinned.Close()) }()
	lease, err := runtime.AcquireLifecycle(ctx, j.control, "catalog", ports.SharedLease, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	if err := j.checkServe(ctx, ""); err != nil {
		return err
	}
	seed, err := j.readCatalogSeed(ctx, dir, m)
	if err != nil || !seed.CatalogVerified {
		return errors.Join(ErrProfileIncomplete, err)
	}
	if err := j.checkCatalogWAL(ctx, dir, seed.CatalogIncarnation); err != nil {
		return err
	}
	stage, err := j.openCatalogWALStage()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, stage.control.Close()) }()
	stageDir, err := stage.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, stageDir.Close()) }()
	data, found, err := stage.readEntryLimit(ctx, stageDir, windowsCatalogWALMain, windowsCatalogSeedMaxSize)
	if err != nil || !found {
		return errors.Join(ErrProfileIncomplete, err)
	}
	err = withWindowsSQLiteImage(ctx, data, func(db *sql.DB) error {
		id, err := catalog.VerifyReadOnlyImage(ctx, db)
		if err != nil || id != seed.CatalogIncarnation {
			return errors.Join(ErrProfileIncomplete, err)
		}
		var path string
		err = db.QueryRowContext(ctx, `SELECT canonical_root FROM worktrees WHERE worktree_id=? AND status='registered'`, worktree).Scan(&path)
		if err != nil {
			return errors.Join(ErrProfileDenied, err)
		}
		root, err := openWindowsApprovedRegistrationRoot(path, m.TargetRoots)
		if err != nil {
			return err
		}
		return root.Close()
	})
	if err != nil {
		return err
	}
	if err := lease.CheckRoot(ctx, j.control, "catalog", ports.SharedLease); err != nil {
		return err
	}
	return use()
}

func (j windowsMaintenanceFiles) createStagedWorktree(ctx context.Context, worktree string, checkRoles func() error) (out ProtectedWorktreeSummary, resultErr error) {
	if err := checkRoles(); err != nil {
		return out, err
	}
	if err := createWindowsProfileDirectory(ctx, j.control, j.acl, "worktrees"); err != nil && !errors.Is(err, ErrProfileExists) {
		return out, err
	}
	parent, err := j.control.OpenDirectory("worktrees")
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	if err := createWindowsProfileDirectory(ctx, parent, j.acl, worktree); err != nil {
		return out, err
	}
	scope, err := parent.OpenDirectory(worktree)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, scope.Close()) }()
	owner := windowsMaintenanceFiles{control: scope, acl: j.acl, profile: j.profile, scope: worktree}
	lease, err := runtime.AcquireLifecycle(ctx, scope, worktree, ports.ExclusiveLease, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	writer, err := runtime.AcquireWriter(ctx, lease, worktree, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	index, err := owner.createStagedWorktreeStore(ctx, "index", lease, writer, checkRoles)
	if err != nil {
		return out, err
	}
	records, err := owner.createStagedWorktreeStore(ctx, "records", lease, writer, checkRoles)
	if err != nil {
		return out, err
	}
	if err := checkRoles(); err != nil {
		return out, err
	}
	out = ProtectedWorktreeSummary{WorktreeID: worktree, IndexIncarnation: index, RecordsIncarnation: records, Verified: true}
	raw, err := json.Marshal(windowsWorktreeStage{FormatVersion: 1, ProfileID: j.profile, Stage: "CREATING", ProtectedWorktreeSummary: out})
	if err != nil {
		return ProtectedWorktreeSummary{}, err
	}
	if err := owner.publishStagedWorktreeFile(ctx, "worktree.json", raw); err != nil {
		return ProtectedWorktreeSummary{}, err
	}
	return out, nil
}

func (j windowsMaintenanceFiles) createStagedWorktreeStore(ctx context.Context, kind string, lease *runtime.Lifecycle,
	writer *runtime.Writer, checkRoles func() error) (incarnation string, resultErr error) {
	if err := checkRoles(); err != nil {
		return "", err
	}
	// The authority is isolated from the data+receipt directory for this store.
	data, err := j.newStagedWorktreeDirectory(ctx, kind+"-wal")
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, data.control.Close()) }()
	control, err := j.newStagedWorktreeDirectory(ctx, kind+"-control")
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, control.control.Close()) }()
	name := kind + ".sqlite"
	if err := data.publishStagedWorktreeFile(ctx, name, nil); err != nil {
		return "", err
	}
	path, err := data.control.CanonicalPath()
	if err != nil {
		return "", err
	}
	incarnation, err = initializeWindowsWorktreeStore(ctx, filepath.Join(path, name), kind, j.scope, lease, writer)
	if err != nil {
		return "", err
	}
	dataDir, err := data.directory(ctx)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, dataDir.Close()) }()
	controlDir, err := control.directory(ctx)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, controlDir.Close()) }()
	image, err := data.closedWorktreeImage(ctx, dataDir, name, kind, incarnation)
	if err != nil {
		return "", err
	}
	identity, err := j.windowsWorktreeAuthorityIdentity(kind, incarnation)
	if err != nil {
		return "", err
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	defer clear(key[:])
	receipt, head := windowsWALGenesis(identity, key, image)
	if err := data.publishStagedWorktreeFile(ctx, walAuthorityName, receipt[:]); err != nil {
		return "", err
	}
	if err := control.publishStagedWorktreeFile(ctx, windowsCatalogHeadKey, key[:]); err != nil {
		return "", err
	}
	if err := checkRoles(); err != nil {
		return "", err
	}
	if err := control.publishStagedWorktreeFile(ctx, walHeadName, head); err != nil {
		return "", err
	}
	authority, err := readWindowsWALAuthority(ctx, control, controlDir, data, dataDir, identity)
	defer clear(authority.key[:])
	if err != nil || authority.records != 1 || authority.head.Image != image || authority.pending != nil {
		return "", errors.Join(ErrStoreRecoveryRequired, err)
	}
	return incarnation, nil
}

func (j windowsMaintenanceFiles) newStagedWorktreeDirectory(ctx context.Context, name string) (windowsMaintenanceFiles, error) {
	if err := createWindowsProfileDirectory(ctx, j.control, j.acl, name); err != nil {
		return windowsMaintenanceFiles{}, err
	}
	child, err := j.control.OpenDirectory(name)
	return windowsMaintenanceFiles{control: child, acl: j.acl, profile: j.profile, scope: j.scope}, err
}

func (j windowsMaintenanceFiles) publishStagedWorktreeFile(ctx context.Context, name string, data []byte) (resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	descriptor, err := j.acl.descriptor()
	if err != nil {
		return err
	}
	return j.publish(ctx, dir, descriptor, name, data, false, nil)
}

func initializeWindowsWorktreeStore(ctx context.Context, path, kind, worktree string, lease *runtime.Lifecycle, writer *runtime.Writer) (incarnation string, resultErr error) {
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path), RawQuery: "mode=rw"}
	db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: u.String()})
	db.SetMaxOpenConns(1)
	owner, err := OpenMaintenanceOwner(ctx, lease, db, worktree)
	if err != nil {
		return "", errors.Join(err, db.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	switch kind {
	case "index":
		value, err := owner.InitializeIndex(ctx, writer, worktree)
		return value.Incarnation, err
	case "records":
		value, err := owner.InitializeRecords(ctx, writer, worktree)
		return value.Incarnation, err
	default:
		return "", ErrProfileInput
	}
}

func (j windowsMaintenanceFiles) closedWorktreeImage(ctx context.Context, dir *os.File, name, kind, incarnation string) (walLogicalImage, error) {
	if err := j.requireAbsent(name+"-wal", name+"-shm", name+"-journal"); err != nil {
		return walLogicalImage{}, errors.Join(ErrProfileIncomplete, err)
	}
	data, found, err := j.readEntryLimit(ctx, dir, name, windowsCatalogSeedMaxSize)
	if err != nil || !found || len(data) < 100 {
		return walLogicalImage{}, errors.Join(ErrProfileIncomplete, err)
	}
	image, err := digestCheckpointedMain(ctx, bytes.NewReader(data), int64(len(data)), binary.BigEndian.Uint32(data[28:32]), windowsCatalogSeedMaxSize)
	if err != nil {
		return walLogicalImage{}, err
	}
	err = withWindowsSQLiteImage(ctx, data, func(db *sql.DB) error {
		return j.verifyWorktreePool(ctx, db, kind, incarnation)
	})
	return image, err
}

func (j windowsMaintenanceFiles) windowsWorktreeAuthorityIdentity(kind, incarnation string) (walAuthorityIdentity, error) {
	identity, err := windowsCatalogAuthorityIdentity(j.profile, incarnation)
	if err != nil {
		return walAuthorityIdentity{}, err
	}
	identity.Store = sha256.Sum256([]byte("MCE4D-WINDOWS-WORKTREE-STORE1\x00" + j.profile + "\x00" + j.scope + "\x00" + kind))
	return identity, nil
}

func (j windowsMaintenanceFiles) verifyWorktreePool(ctx context.Context, db *sql.DB, kind, incarnation string) error {
	table, wantVersion := "store_identity", 2
	if kind == "records" {
		table, wantVersion = "records_meta", 1
	}
	var worktree, stored, integrity string
	var version int
	if err := db.QueryRowContext(ctx, "SELECT worktree_id,incarnation FROM "+table+" WHERE singleton=1").Scan(&worktree, &stored); err != nil ||
		worktree != j.scope || stored != incarnation {
		return errors.Join(ErrProfileIncomplete, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT schema_version FROM schema_meta WHERE singleton=1").Scan(&version); err != nil || version != wantVersion {
		return errors.Join(ErrProfileIncomplete, err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return errors.Join(ErrProfileIncomplete, err)
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	if err := errors.Join(rows.Err(), rows.Close()); err != nil || invalid {
		return errors.Join(ErrProfileIncomplete, err)
	}
	return ctx.Err()
}
