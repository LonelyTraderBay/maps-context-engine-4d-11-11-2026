//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProtectedRestoreStageCatalogFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	raw, err := os.ReadFile(filepath.Join(profileStatePath(root), "backups/public-catalog/backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := DecodeStoreBackupManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openQuiescentOwner(ctx, root, "catalog", time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	source, err := owner.domain.Child(ctx, "backups/public-catalog")
	if err != nil {
		t.Fatal(err)
	}
	defer source.root.Close()
	id, err := profileUUID()
	if err != nil {
		t.Fatal(err)
	}
	next, err := profileUUID()
	if err != nil {
		t.Fatal(err)
	}
	relative := "backups/stage-" + id
	if err := profileDirectory(owner.domain.root, relative, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0700); err != nil {
		t.Fatal(err)
	}
	stage, err := owner.domain.Child(ctx, relative)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.root.Close()
	path, err := stage.root.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Stores[0]
	for _, bad := range []struct {
		entry StoreBackupFile
		next  string
	}{{entry, "not uuid"}, {entry, entry.SourceIncarnation}, {func() StoreBackupFile { copy := entry; copy.SchemaVersion = 3; return copy }(), next}} {
		if _, err := owner.stageRestoreDatabase(ctx, source, stage, bad.entry, bad.next); err == nil {
			t.Fatal("invalid typed staging request accepted")
		}
		if _, err := os.Lstat(filepath.Join(path, "catalog.sqlite")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid typed input created staging DB")
		}
	}
	before, err := owner.authenticatedImage(ctx, owner.stores[0])
	if err != nil {
		t.Fatal(err)
	}
	image, err := owner.stageRestoreDatabase(ctx, source, stage, entry, next)
	if err != nil {
		t.Fatal(err)
	}
	if image.SHA256 == before.SHA256 || image.PageCount == 0 {
		t.Fatal("staging did not rotate the image")
	}
	after, err := owner.authenticatedImage(ctx, owner.stores[0])
	if err != nil || before != after {
		t.Fatalf("staging changed current source: %v", err)
	}
	// Reusing a staging directory never adopts/overwrites an uncertain result.
	if _, err := owner.stageRestoreDatabase(ctx, source, stage, entry, next); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("existing staging adopted: %v", err)
	}
	afterRaw, err := os.ReadFile(filepath.Join(profileStatePath(root), "backups/public-catalog/backup.json"))
	if err != nil || string(afterRaw) != string(raw) {
		t.Fatal("backup manifest changed")
	}
	summary, _ := json.Marshal(map[string]any{"scope": "unserved catalog staging only", "new_incarnation": next, "logical_sha256": fmt.Sprintf("%x", image.SHA256)})
	fmt.Println("NATIVE_RESTORE_STAGE_NEW_CATALOG_IDENTITY_SOURCE_AND_BACKUP_UNCHANGED_PASS " + string(summary))
}

func TestProtectedRestoreStageWorktreePairFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	owner, err := openQuiescentOwner(ctx, root, ownerWorktree, time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	sourceName, err := profileUUID()
	if err != nil {
		t.Fatal(err)
	}
	stageName, err := profileUUID()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{sourceName, stageName} {
		if err := profileDirectory(owner.domain.root, "backups/"+name, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0700); err != nil {
			t.Fatal(err)
		}
	}
	source, err := owner.domain.Child(ctx, "backups/"+sourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer source.root.Close()
	stage, err := owner.domain.Child(ctx, "backups/"+stageName)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.root.Close()
	sourcePath, err := source.root.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	stagePath, err := stage.root.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]walLogicalImage{}
	entries := []StoreBackupFile{}
	// Backup the actual native pair under this one exclusive lease. Metadata
	// below comes from these real stores, not fabricated catalog head revisions.
	for _, store := range owner.stores {
		image, err := owner.authenticatedImage(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		before[store.name] = image
		if err := owner.openReadPool(ctx, store); err != nil {
			t.Fatal(err)
		}
		meta, err := owner.sourceMetadata(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		if err := profileFile(source.root, store.name, nil, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0600, false); err != nil {
			t.Fatal(err)
		}
		conn, err := owner.pool.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		copied, copyErr := copyPinnedSQLiteBackup(ctx, conn, profileNativeDSN(filepath.Join(sourcePath, store.name)), 2<<30)
		if err := errors.Join(copyErr, conn.Close(), owner.closeReadPool()); err != nil {
			t.Fatal(err)
		}
		rawHash, err := hashBackupArtifact(ctx, source, store.name, copied.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		entry := StoreBackupFile{Kind: meta.Kind, WorktreeID: meta.WorktreeID, SourceIncarnation: meta.Incarnation, SchemaVersion: meta.SchemaVersion, MinReaderVersion: meta.MinReaderVersion, FileName: store.name, RawSHA256: rawHash, LogicalSHA256: fmt.Sprintf("%x", image.SHA256), ByteLength: copied.Bytes, SQLiteVersion: copied.SQLiteVersion}
		if meta.Kind == "records" {
			epoch := meta.RecordEpoch
			entry.RecordEpoch = &epoch
		}
		entries = append(entries, entry)
	}
	for i, entry := range entries {
		next, err := profileUUID()
		if err != nil {
			t.Fatal(err)
		}
		image, err := owner.stageRestoreDatabase(ctx, source, stage, entry, next)
		if err != nil || image.SHA256 == before[entry.FileName].SHA256 {
			t.Fatalf("stage %s: %v", entry.Kind, err)
		}
		db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(stagePath, entry.FileName)))
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		table := "store_identity"
		if entry.Kind == "records" {
			table = "records_meta"
		}
		var actual string
		if err := db.QueryRowContext(ctx, `SELECT incarnation FROM `+table+` WHERE singleton=1`).Scan(&actual); err != nil || actual != next {
			t.Fatalf("stage identity %s: %v", entry.Kind, err)
		}
		if i == 0 {
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM generations WHERE generation_id='77777777-7777-4777-8777-777777777777'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("actual captured generation lost: %v", err)
			}
		} else {
			var epoch int64
			if err := db.QueryRowContext(ctx, `SELECT record_epoch FROM records_meta WHERE singleton=1`).Scan(&epoch); err != nil || epoch != *entry.RecordEpoch {
				t.Fatalf("record epoch changed: %v", err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		// Close SQL before reading any main inode in this process.
		after, err := owner.authenticatedImage(ctx, owner.stores[i])
		if err != nil || after != before[entry.FileName] {
			t.Fatalf("current %s changed: %v", entry.Kind, err)
		}
	}
	fmt.Println("NATIVE_UNSERVED_INDEX_RECORDS_STAGE_NEW_IDS_GENERATION_AND_NUMERIC_EPOCH_PRESERVED_PASS")
}
