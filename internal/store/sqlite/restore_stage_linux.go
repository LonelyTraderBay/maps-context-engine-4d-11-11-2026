//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
	"golang.org/x/sys/unix"
)

// A private, unserved staging image. This does not install a trusted WAL head
// or make a replacement visible. The offline controller must subsequently
// verify/bootstrap its new authority and journal every replacement boundary.
func (o *quiescentOwner) stageRestoreDatabase(ctx context.Context, source, destination *stateDomain, entry StoreBackupFile, newIncarnation string) (out walLogicalImage, resultErr error) {
	if o == nil || o.pool != nil || source == nil || destination == nil || !model.ValidUUID(newIncarnation) || !model.ValidUUID(entry.SourceIncarnation) || newIncarnation == entry.SourceIncarnation || entry.FileName != entry.Kind+".sqlite" || entry.ByteLength < 512 || entry.ByteLength > 2<<30 || entry.ByteLength%512 != 0 || !model.ValidDigest(entry.RawSHA256) || !model.ValidDigest(entry.LogicalSHA256) || !supportedBackupSchema(entry) || !backupSQLiteVersion(entry.SQLiteVersion) {
		return out, ErrProfileInput
	}
	if entry.Kind == "records" {
		if entry.RecordEpoch == nil || *entry.RecordEpoch < 0 || *entry.RecordEpoch > 9007199254740991 {
			return out, ErrProfileInput
		}
	} else if entry.RecordEpoch != nil {
		return out, ErrProfileInput
	}
	var original *nativeWALStore
	for _, store := range o.stores {
		if store.name == entry.FileName {
			original = store
		}
	}
	if original == nil || entry.WorktreeID != "" && entry.WorktreeID != o.scope || entry.Kind == "catalog" && o.scope != "catalog" || entry.Kind != "catalog" && entry.WorktreeID != o.scope {
		return out, ErrProfileInput
	}
	nextID, _ := profileUUIDBytes(newIncarnation)
	if nextID == original.identity.Incarnation {
		return out, ErrProfileInput
	}
	if err := errors.Join(o.check(ctx), source.Check(ctx), destination.Check(ctx)); err != nil {
		return out, err
	}
	if source.root == destination.root {
		return out, ErrProfileInput
	}
	before, err := o.authenticatedImage(ctx, original)
	if err != nil {
		return out, err
	}
	_, digest, err := readRestoreInputFile(ctx, source.root, entry.FileName, entry.ByteLength, entry.ByteLength, o.profile.manifest.EngineUID, o.profile.manifest.EngineGID, false)
	if err != nil || digest != entry.RawSHA256 {
		return out, errors.Join(ErrStateChanged, err)
	}
	if err := backupCapacity(ctx, destination.root, entry.ByteLength); err != nil {
		return out, err
	}
	sourcePath, err := source.root.CanonicalPath()
	if err != nil {
		return out, err
	}
	destinationPath, err := destination.root.CanonicalPath()
	if err != nil {
		return out, err
	}
	if sourcePath == destinationPath {
		return out, ErrProfileInput
	}
	if err := profileFile(destination.root, entry.FileName, nil, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0600, false); err != nil {
		return out, err
	}
	// Source is a closed standalone backup; URI mode=ro prevents schema writes.
	uri, err := url.Parse(profileNativeDSN(filepath.Join(sourcePath, entry.FileName)))
	if err != nil {
		return out, err
	}
	q := uri.Query()
	q.Set("mode", "ro")
	uri.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return out, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return out, errors.Join(err, db.Close())
	}
	_, copyErr := copyPinnedSQLiteBackup(ctx, conn, profileNativeDSN(filepath.Join(destinationPath, entry.FileName)), entry.ByteLength)
	if err := errors.Join(copyErr, conn.Close(), db.Close()); err != nil {
		return out, err
	}
	// All source SQL handles have closed before any ordinary input FD is read.
	_, afterHash, err := readRestoreInputFile(ctx, source.root, entry.FileName, entry.ByteLength, entry.ByteLength, o.profile.manifest.EngineUID, o.profile.manifest.EngineGID, false)
	if err != nil || afterHash != digest {
		return out, errors.Join(ErrStateChanged, err)
	}
	if err := o.rotateStagedIncarnation(ctx, destination, entry, newIncarnation); err != nil {
		return out, err
	}
	// Validation and checkpoint used the same owned writable staging pool;
	// close removed its sidecars. A second readonly WAL opener could recreate
	// sidecars without removing them at close. Never erase them by filename.
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		f, err := destination.root.OpenRegular(entry.FileName + suffix)
		if err == nil {
			return out, errors.Join(ErrStateUnsafe, f.Close())
		}
		if !errors.Is(err, os.ErrNotExist) {
			return out, err
		}
	}
	f, err := destination.root.OpenRegular(entry.FileName)
	if err != nil {
		return out, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, f.Close())
		if resultErr != nil {
			out = walLogicalImage{}
		}
	}()
	if stateOwner(f, false) != nil {
		return out, ErrStateUnsafe
	}
	st, err := f.Stat()
	if err != nil {
		return out, err
	}
	header := make([]byte, 100)
	if _, err := f.ReadAt(header, 0); err != nil {
		return out, err
	}
	out, err = digestCheckpointedMain(ctx, f, st.Size(), binary.BigEndian.Uint32(header[28:32]), 2<<30)
	if err != nil {
		return out, err
	}
	if _, err := hashBackupArtifact(ctx, destination, entry.FileName, st.Size()); err != nil {
		return out, err
	}
	current, err := o.authenticatedImage(ctx, original)
	if err != nil || current != before {
		return out, errors.Join(ErrStateChanged, err)
	}
	return out, errors.Join(o.check(ctx), source.Check(ctx), destination.Check(ctx))
}

func (o *quiescentOwner) rotateStagedIncarnation(ctx context.Context, destination *stateDomain, entry StoreBackupFile, next string) (resultErr error) {
	path, err := destination.root.CanonicalPath()
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(path, entry.FileName)))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	pool, err := OpenMaintenanceOwner(ctx, o.lease, db, o.scope)
	if err != nil {
		return errors.Join(err, db.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, pool.Close()) }()
	var mode string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		return errors.Join(ErrStateUnsafe, err)
	}
	// Exact adapter schema validation before any identity/data mutation. A
	// malformed or future schema only affects the new unserved destination.
	table := ""
	switch entry.Kind {
	case "catalog":
		if _, err := catalog.OpenExistingLeased(ctx, db, o.lease); err != nil {
			return err
		}
		table = "catalog_identity"
	case "index":
		if _, err := openMaintenanceIndex(ctx, db, o.scope, o.lease); err != nil {
			return err
		}
		table = "store_identity"
	case "records":
		if _, err := records.Open(ctx, db, o.scope, o.lease); err != nil {
			return err
		}
		table = "records_meta"
	default:
		return ErrProfileInput
	}
	// Recheck the backed-up identity, not just a manifest's claim.
	var current string
	if err := db.QueryRowContext(ctx, `SELECT incarnation FROM `+table+` WHERE singleton=1`).Scan(&current); err != nil || current != entry.SourceIncarnation {
		return errors.Join(ErrStateChanged, err)
	}
	expectedOld := backupSourceMetadata{Kind: entry.Kind, WorktreeID: entry.WorktreeID, Incarnation: current, SchemaVersion: entry.SchemaVersion, MinReaderVersion: entry.MinReaderVersion}
	if entry.RecordEpoch != nil {
		expectedOld.RecordEpoch = *entry.RecordEpoch
	}
	if err := verifyBackupContents(ctx, o, destination, expectedOld, db); err != nil {
		return err
	}
	if err := o.check(ctx); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE `+table+` SET incarnation=? WHERE singleton=1 AND incarnation=?`, next, current)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return errors.Join(ErrStateChanged, err)
	}
	if err := o.check(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	expected := backupSourceMetadata{Kind: entry.Kind, WorktreeID: entry.WorktreeID, Incarnation: next, SchemaVersion: entry.SchemaVersion, MinReaderVersion: entry.MinReaderVersion}
	if entry.RecordEpoch != nil {
		expected.RecordEpoch = *entry.RecordEpoch
	}
	if err := verifyBackupContents(ctx, o, destination, expected, db); err != nil {
		return err
	}
	var busy, pages, done int
	if err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &pages, &done); err != nil || busy != 0 || pages != done {
		return errors.Join(ErrStateUnsafe, err)
	}
	return errors.Join(o.check(ctx), destinationIdentity(ctx, db, table, next))
}

func destinationIdentity(ctx context.Context, db *sql.DB, table, next string) error {
	var actual string
	if err := db.QueryRowContext(ctx, `SELECT incarnation FROM `+table+` WHERE singleton=1`).Scan(&actual); err != nil {
		return err
	}
	if actual != next {
		return ErrStateChanged
	}
	return nil
}
