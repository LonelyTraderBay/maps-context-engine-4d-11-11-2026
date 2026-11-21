//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// CreateProtectedBackup is an explicit local administrator capability. The
// trusted caller authenticates the actor/scope and, for a worktree, observes
// current catalog revisions under its own shared catalog owner. Those handles
// must remain alive until return. Catalog backup must have no caller catalog
// pool/lease, because this operation needs its exclusive lifecycle.
//
// Output is a fresh directory immediately below the selected store's private
// backups directory. Existing output is never adopted, overwritten or cleaned.
// Failure retains incomplete or unacknowledged private artifacts; file presence
// alone never proves the command completed or grants cleanup/recovery authority.
func CreateProtectedBackup(ctx context.Context, root string, options ProtectedBackupOptions, authorize func(context.Context) error, observeRevisions func(context.Context) (catalog.BackupRevisions, error)) (summary ProtectedBackupSummary, resultErr error) {
	if ctx == nil || authorize == nil || !validProfilePath(root) || options.Wait <= 0 || options.Wait > time.Hour || options.MaxDatabaseBytes < 1 || options.MaxDatabaseBytes > backupImageMaxBytes || !filepath.IsAbs(options.Output) || filepath.Clean(options.Output) != options.Output {
		return summary, ErrProfileInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return summary, ErrProfileInput
	}
	scope := "catalog"
	base := profileStatePath(root)
	switch options.Target {
	case "catalog":
		if options.WorktreeID != "" || observeRevisions != nil {
			return summary, ErrProfileInput
		}
	case "worktree":
		if _, err := profileUUIDBytes(options.WorktreeID); err != nil || observeRevisions == nil {
			return summary, ErrProfileInput
		}
		scope = options.WorktreeID
		base = worktreeStatePath(root, scope)
	default:
		return summary, ErrProfileInput
	}
	leaf := filepath.Base(options.Output)
	if filepath.Dir(options.Output) != filepath.Join(base, "backups") || len(leaf) > 128 || strings.Contains(leaf, "/") || model.ValidateRelativePath(leaf, false) != nil {
		return summary, ErrProfileInput
	}
	if err := authorize(ctx); err != nil {
		return summary, err
	}
	var revisions catalog.BackupRevisions
	if observeRevisions != nil {
		var err error
		revisions, err = observeRevisions(ctx)
		if err != nil {
			return summary, err
		}
		if !revisions.ValidFor(scope) {
			return summary, ErrProfileInput
		}
		if raw, err := json.Marshal(revisions); err != nil || len(raw) > backupManifestMaxBytes {
			return summary, ErrStateLimit
		}
	}
	check := func(ctx context.Context) error {
		if err := authorize(ctx); err != nil {
			return err
		}
		if observeRevisions != nil {
			current, err := observeRevisions(ctx)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, revisions) {
				return ErrStateChanged
			}
		}
		return ctx.Err()
	}
	owner, err := openQuiescentOwner(ctx, root, scope, options.Wait, check)
	if err != nil {
		return summary, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, owner.Close())
		if resultErr != nil {
			summary = ProtectedBackupSummary{}
		}
	}()
	if options.Target == "worktree" && revisions.CatalogIncarnation != owner.profile.manifest.CatalogIncarnation {
		return summary, ErrStateChanged
	}
	before := make(map[*nativeWALStore]walLogicalImage)
	metadata := make(map[*nativeWALStore]backupSourceMetadata)
	var databaseBytes int64
	// Validate every source and its exact latest acknowledged image before
	// creating any destination. All SQL handles close between byte captures.
	for _, store := range owner.stores {
		image, err := owner.authenticatedImage(ctx, store)
		if err != nil {
			return summary, err
		}
		if image.PageSize == 0 || int64(image.PageCount) > (options.MaxDatabaseBytes-databaseBytes)/int64(image.PageSize) {
			return summary, ErrStateLimit
		}
		databaseBytes += int64(image.PageCount) * int64(image.PageSize)
		before[store] = image
		if err := owner.openReadPool(ctx, store); err != nil {
			return summary, err
		}
		meta, err := owner.sourceMetadata(ctx, store)
		if err != nil {
			return summary, errors.Join(err, owner.closeReadPool())
		}
		metadata[store] = meta
		if options.Target == "catalog" {
			catalogStore, err := catalog.OpenExistingLeased(ctx, owner.pool.db, owner.lease)
			if err != nil {
				return summary, errors.Join(err, owner.closeReadPool())
			}
			revisions, err = catalogStore.ReadBackupRevisions(ctx, "", check)
			if err != nil {
				return summary, errors.Join(err, owner.closeReadPool())
			}
			if raw, err := json.Marshal(revisions); err != nil || len(raw) > backupManifestMaxBytes {
				return summary, errors.Join(ErrStateLimit, owner.closeReadPool())
			}
		}
		if err := owner.closeReadPool(); err != nil {
			return summary, err
		}
		after, err := owner.authenticatedImage(ctx, store)
		if err != nil || after != image {
			return summary, errors.Join(ErrStateChanged, err)
		}
	}
	if err := backupCapacity(ctx, owner.domain.root, databaseBytes); err != nil {
		return summary, err
	}
	if err := owner.check(ctx); err != nil {
		return summary, err
	}
	// Source and destination share the same verified owned filesystem. A
	// failed mkdir or unknown preexisting entry grants no removal authority.
	if err := profileDirectory(owner.domain.root, "backups", uint32(unix.Geteuid()), uint32(unix.Getegid()), 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return summary, err
	}
	if err := profileDirectory(owner.domain.root, "backups/"+leaf, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0700); err != nil {
		return summary, err
	}
	destination, err := owner.domain.Child(ctx, "backups/"+leaf)
	if err != nil {
		return summary, err
	}
	defer func() { resultErr = errors.Join(resultErr, destination.root.Close()) }()
	path, err := destination.root.CanonicalPath()
	if err != nil || path != options.Output {
		return summary, ErrStateUnsafe
	}
	manifest := StoreBackupManifest{FormatVersion: 1, OperationID: owner.operation, ProfileInstallationID: owner.profile.manifest.ProfileID, Target: options.Target, WorktreeID: options.WorktreeID, Consistency: "single_store", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Revisions: revisions, Stores: []StoreBackupFile{}}
	if options.Target == "worktree" {
		manifest.Consistency = "quiesced_worktree_set"
	}
	for _, store := range owner.stores {
		if err := owner.check(ctx); err != nil {
			return summary, err
		}
		if err := profileFile(destination.root, store.name, nil, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0600, false); err != nil {
			return summary, err
		}
		if err := owner.openReadPool(ctx, store); err != nil {
			return summary, err
		}
		conn, err := owner.pool.db.Conn(ctx)
		if err != nil {
			return summary, err
		}
		image, copyErr := copyPinnedSQLiteBackup(ctx, conn, profileNativeDSN(filepath.Join(path, store.name)), options.MaxDatabaseBytes)
		if err := errors.Join(copyErr, conn.Close(), owner.closeReadPool()); err != nil {
			return summary, err
		}
		head := before[store]
		if image.Bytes != int64(head.PageCount)*int64(head.PageSize) {
			return summary, ErrStateChanged
		}
		if err := verifyBackupArtifact(ctx, owner, store, destination, metadata[store]); err != nil {
			return summary, err
		}
		hash, err := hashBackupArtifact(ctx, destination, store.name, image.Bytes)
		if err != nil {
			return summary, err
		}
		meta := metadata[store]
		entry := StoreBackupFile{Kind: meta.Kind, WorktreeID: meta.WorktreeID, SourceIncarnation: meta.Incarnation, SchemaVersion: meta.SchemaVersion, MinReaderVersion: meta.MinReaderVersion, FileName: store.name, RawSHA256: hash, LogicalSHA256: hex.EncodeToString(head.SHA256[:]), ByteLength: image.Bytes, SQLiteVersion: image.SQLiteVersion}
		if meta.Kind == "records" {
			epoch := meta.RecordEpoch
			entry.RecordEpoch = &epoch
		}
		manifest.Stores = append(manifest.Stores, entry)
	}
	for _, store := range owner.stores {
		image, err := owner.authenticatedImage(ctx, store)
		if err != nil || image != before[store] {
			return summary, errors.Join(ErrStateChanged, err)
		}
	}
	if err := owner.check(ctx); err != nil {
		return summary, err
	}
	raw, err := json.Marshal(manifest)
	if err != nil || len(raw) > backupManifestMaxBytes || model.ValidateWireJSON(raw, backupManifestMaxBytes) != nil {
		return summary, ErrStateLimit
	}
	if _, err := DecodeStoreBackupManifest(raw); err != nil {
		return summary, err
	}
	raw = append(raw, '\n')
	if err := profileFile(destination.root, "backup.json", raw, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0600, false); err != nil {
		return summary, err
	}
	if err := errors.Join(owner.check(ctx), destination.Check(ctx)); err != nil {
		return summary, err
	}
	digest := sha256.Sum256(raw)
	return ProtectedBackupSummary{OperationID: owner.operation, Target: options.Target, WorktreeID: options.WorktreeID, ManifestSHA256: hex.EncodeToString(digest[:]), FileCount: len(manifest.Stores) + 1, TotalBytes: databaseBytes + int64(len(raw))}, nil
}

func backupCapacity(ctx context.Context, root *paths.Root, bytes int64) (resultErr error) {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if bytes < 1 || bytes > backupImageMaxBytes {
		return ErrStateLimit
	}
	f, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &fs); err != nil {
		return err
	}
	if fs.Bsize < 1 || uint64(fs.Bavail) > (^uint64(0))/uint64(fs.Bsize) {
		return ErrStateUnsafe
	}
	// Budget both standalone main and potential destination WAL/checkpoint
	// overlap, metadata and a small reserve. This is a preflight, not OS quota.
	need := uint64(bytes)*2 + backupManifestMaxBytes + (64 << 20)
	if uint64(fs.Bavail)*uint64(fs.Bsize) < need {
		return ErrStateLimit
	}
	return nil
}

func verifyBackupArtifact(ctx context.Context, owner *quiescentOwner, store *nativeWALStore, destination *stateDomain, expected backupSourceMetadata) (resultErr error) {
	path, err := destination.root.CanonicalPath()
	if err != nil {
		return err
	}
	u, err := url.Parse(profileNativeDSN(filepath.Join(path, store.name)))
	if err != nil {
		return err
	}
	values := u.Query()
	values.Set("mode", "ro")
	u.RawQuery = values.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	pool, err := OpenMaintenanceOwner(ctx, owner.lease, db, owner.scope)
	if err != nil {
		return errors.Join(err, db.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, pool.Close()) }()
	return verifyBackupContents(ctx, owner, destination, expected, db)
}

// Caller owns the single pool and closes it before any ordinary file read.
func verifyBackupContents(ctx context.Context, owner *quiescentOwner, destination *stateDomain, expected backupSourceMetadata, db *sql.DB) error {
	var integrity, incarnation string
	var version, minReader int
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return errors.Join(ErrStateUnsafe, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&version, &minReader); err != nil {
		return err
	}
	table := ""
	switch expected.Kind {
	case "catalog":
		table = "catalog_identity"
	case "index":
		table = "store_identity"
	case "records":
		table = "records_meta"
	default:
		return ErrStateUnsafe
	}
	if err := db.QueryRowContext(ctx, `SELECT incarnation FROM `+table+` WHERE singleton=1`).Scan(&incarnation); err != nil {
		return err
	}
	if version != expected.SchemaVersion || minReader != expected.MinReaderVersion || incarnation != expected.Incarnation {
		return ErrStateChanged
	}
	if expected.Kind != "catalog" {
		var worktree string
		if err := db.QueryRowContext(ctx, `SELECT worktree_id FROM `+table+` WHERE singleton=1`).Scan(&worktree); err != nil || worktree != expected.WorktreeID {
			return errors.Join(ErrStateChanged, err)
		}
	}
	if expected.Kind == "records" {
		var epoch int64
		if err := db.QueryRowContext(ctx, `SELECT record_epoch FROM records_meta WHERE singleton=1`).Scan(&epoch); err != nil || epoch != expected.RecordEpoch {
			return errors.Join(ErrStateChanged, err)
		}
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	bad := rows.Next()
	err = errors.Join(rows.Err(), rows.Close())
	if bad || err != nil {
		return errors.Join(ErrStateUnsafe, err)
	}
	return errors.Join(owner.check(ctx), destination.Check(ctx))
}

func hashBackupArtifact(ctx context.Context, destination *stateDomain, name string, bytes int64) (digest string, resultErr error) {
	if destination.Check(ctx) != nil || bytes < 1 || bytes > backupImageMaxBytes {
		return "", ErrStateUnsafe
	}
	f, err := destination.root.OpenRegular(name)
	if err != nil {
		return "", err
	}
	defer func() {
		resultErr = errors.Join(resultErr, f.Close())
		if resultErr != nil {
			digest = ""
		}
	}()
	before, err := f.Stat()
	if err != nil || before.Size() != bytes || stateOwner(f, false) != nil {
		return "", ErrStateUnsafe
	}
	h := sha256.New()
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buffer)
		total += int64(n)
		if total > bytes {
			return "", ErrStateLimit
		}
		_, _ = h.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", io.ErrNoProgress
		}
	}
	after, err := f.Stat()
	if err != nil || total != bytes || after.Size() != bytes || !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) {
		return "", ErrStateChanged
	}
	if err := errors.Join(paths.VerifyRegularFile(f), f.Sync(), syncWALDirectory(destination.root), destination.Check(ctx)); err != nil {
		return "", err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sidecar, err := destination.root.OpenRegular(name + suffix)
		if err == nil {
			return "", errors.Join(ErrStateUnsafe, sidecar.Close())
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
