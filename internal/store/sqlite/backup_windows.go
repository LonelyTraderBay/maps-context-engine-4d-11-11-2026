package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
)

// BackupWindowsProtectedStore publishes a closed SQLite backup and manifest
// under the selected controller-owned scope. The caller presents explicit
// output and scope through the packaged CLI.
func BackupWindowsProtectedStore(ctx context.Context, installation, target, worktree, output string) (ProtectedBackupSummary, error) {
	leaf := filepath.Base(output)
	if !windowsOperationLeaf(leaf) || output != filepath.Join(windowsMaintenancePath(installation, target, worktree), "backups", leaf) {
		return ProtectedBackupSummary{}, ErrProfileInput
	}
	return withWindowsStoreMaintenance(ctx, installation, target, worktree, "backup", 5*time.Second, func(o windowsStoreMaintenance) (ProtectedBackupSummary, error) {
		return o.backup(ctx, target, worktree, leaf)
	})
}

func windowsMaintenancePath(installation, target, worktree string) string {
	path := filepath.Join(installation, "control")
	if target == "worktree" {
		return filepath.Join(path, "worktrees", worktree)
	}
	return path
}

func (o windowsStoreMaintenance) backup(ctx context.Context, target, worktree, leaf string) (out ProtectedBackupSummary, resultErr error) {
	if err := o.files.checkServe(ctx, ""); err != nil {
		return out, err
	}
	ids, err := o.incarnations(ctx)
	if err != nil {
		return out, err
	}
	stores, release, err := o.openStores()
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	backup, err := o.newBackupDirectory(ctx, leaf)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, backup.control.Close()) }()
	operation, err := windowsMaintenanceID()
	if err != nil {
		return out, err
	}
	manifest := StoreBackupManifest{FormatVersion: 1, OperationID: operation, ProfileInstallationID: o.profile.ProfileID,
		Target: target, WorktreeID: worktree, Consistency: "single_store",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Revisions: o.revisions, Stores: []StoreBackupFile{}}
	if target == "worktree" {
		manifest.Consistency = "quiesced_worktree_set"
	}
	var countBytes int64
	for i, store := range stores {
		image, observed, key, err := store.capture(ctx, ids[i])
		clear(key[:])
		if err != nil {
			return out, err
		}
		file, err := o.backupOne(ctx, backup, store, ids[i], image, observed, &manifest)
		if err != nil {
			return out, err
		}
		manifest.Stores = append(manifest.Stores, file)
		countBytes += file.ByteLength
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return out, err
	}
	if _, err := DecodeStoreBackupManifest(raw); err != nil {
		return out, err
	}
	if err := errors.Join(o.check(), o.files.checkServe(ctx, "")); err != nil {
		return out, err
	}
	if err := backup.publishStagedWorktreeFile(ctx, "backup.json", raw); err != nil {
		return out, err
	}
	if err := o.check(); err != nil {
		return out, err
	}
	return ProtectedBackupSummary{OperationID: operation, Target: target, WorktreeID: worktree,
		ManifestSHA256: windowsBytesDigest(raw), FileCount: len(stores) + 1, TotalBytes: countBytes + int64(len(raw))}, nil
}

func (o windowsStoreMaintenance) newBackupDirectory(ctx context.Context, leaf string) (out windowsMaintenanceFiles, resultErr error) {
	err := createWindowsProfileDirectory(ctx, o.files.control, o.files.acl, "backups")
	if err != nil && !errors.Is(err, ErrProfileExists) {
		return out, err
	}
	backups, err := o.files.openWindowsPoolDirectory("backups")
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, backups.control.Close()) }()
	fresh, err := backups.newStagedWorktreeDirectory(ctx, leaf)
	if errors.Is(err, ErrProfileExists) {
		return out, errors.Join(os.ErrExist, err)
	}
	return fresh, err
}

func (o windowsStoreMaintenance) backupOne(ctx context.Context, backup windowsMaintenanceFiles, store windowsMaintenanceStore, incarnation string,
	image []byte, observed walLogicalImage, manifest *StoreBackupManifest) (file StoreBackupFile, resultErr error) {
	version := 1
	if store.kind == "index" {
		version = 2
	}
	file = StoreBackupFile{Kind: store.kind, WorktreeID: manifest.WorktreeID, SourceIncarnation: incarnation,
		SchemaVersion: version, MinReaderVersion: version, FileName: store.main,
		LogicalSHA256: hex.EncodeToString(observed.SHA256[:])}
	hold, err := o.lease.Hold(ctx)
	if err != nil {
		return file, err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	err = withWindowsSQLiteImage(ctx, image, func(db *sql.DB) error {
		if err := store.verifyImage(ctx, db, o.lease, incarnation); err != nil {
			return err
		}
		return windowsBackupMetadata(ctx, o, db, &file, manifest)
	})
	if err != nil {
		return file, err
	}
	limit := windowsStoreImageLimit(store.main)
	copied, err := windowsStageBackupCopy(ctx, o.lease, image, backup, store.main, limit)
	if err != nil {
		return file, err
	}
	if err := backup.flushArtifact(ctx, store.main); err != nil {
		return file, err
	}
	raw, err := backup.requiredBytes(ctx, store.main, limit)
	if err != nil || int64(len(raw)) != copied.Bytes || !windowsStandaloneImage(raw) {
		return file, errors.Join(ErrStateChanged, err)
	}
	file.ByteLength, file.RawSHA256, file.SQLiteVersion = copied.Bytes, windowsBytesDigest(raw), copied.SQLiteVersion
	err = withWindowsSQLiteImage(ctx, raw, func(db *sql.DB) error {
		return o.verifyBackupImage(ctx, store, db, file, *manifest)
	})
	return file, errors.Join(err, o.check())
}

func windowsBackupMetadata(ctx context.Context, o windowsStoreMaintenance, db *sql.DB, entry *StoreBackupFile, manifest *StoreBackupManifest) error {
	switch entry.Kind {
	case "catalog":
		store, err := catalog.OpenAuthenticatedImage(ctx, db, o.lease)
		if err != nil {
			return err
		}
		manifest.Revisions, err = store.ReadBackupRevisions(ctx, "", func(context.Context) error { return o.check() })
		return err
	case "records":
		var epoch int64
		if err := db.QueryRowContext(ctx, "SELECT record_epoch FROM records_meta WHERE singleton=1").Scan(&epoch); err != nil {
			return err
		}
		entry.RecordEpoch = &epoch
	}
	return nil
}

func windowsBytesDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func windowsStandaloneImage(raw []byte) bool {
	return len(raw) >= 512 && string(raw[:16]) == "SQLite format 3\x00" && raw[18] == 1 && raw[19] == 1
}

func (o windowsStoreMaintenance) verifyBackupImage(ctx context.Context, store windowsMaintenanceStore, db *sql.DB,
	file StoreBackupFile, manifest StoreBackupManifest) error {
	if err := store.verifyImage(ctx, db, o.lease, file.SourceIncarnation); err != nil {
		return err
	}
	expectedEpoch := file.RecordEpoch
	expectedRevisions := manifest.Revisions
	if err := windowsBackupMetadata(ctx, o, db, &file, &manifest); err != nil {
		return err
	}
	switch store.kind {
	case "catalog":
		if !reflect.DeepEqual(expectedRevisions, manifest.Revisions) {
			return ErrStateChanged
		}
	case "records":
		if expectedEpoch == nil || file.RecordEpoch == nil || *expectedEpoch != *file.RecordEpoch {
			return ErrStateChanged
		}
	}
	return nil
}
