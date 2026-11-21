//go:build linux

package sqlite

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"golang.org/x/sys/unix"
	modsqlite "modernc.org/sqlite"
)

const maintenanceOtherWorktree = "22222222-2222-4222-8222-222222222222"

func TestProtectedMaintenanceSecondWorktreeProvisionFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	if _, err := ProvisionProtectedWorktree(ctx, root, maintenanceOtherWorktree); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedMaintenanceProcessFixture(t *testing.T) {
	if os.Getenv("MCE4D_MAINTENANCE_CHILD") == "" {
		t.Skip("separately launched synthetic maintenance child")
	}
	ctx, root := profileCatalogFixture(t, true)
	scope := ownerWorktree
	if os.Getenv("MCE4D_MAINTENANCE_CHILD") == "other" {
		scope = maintenanceOtherWorktree
	}
	owner, err := OpenProtectedWorktree(ctx, root, scope, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !owner.closed.Load() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	store, err := owner.Index.Store()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Identity(ctx); err != nil {
		t.Fatal(err)
	}
	if scope == maintenanceOtherWorktree {
		writer, err := runtime.AcquireWriter(ctx, owner.lease, ownerJob, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := writer.Close(); err != nil {
				t.Error(err)
			}
		}()
		tx, err := owner.Index.lifetime.pools[1].BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'FAILED',?,?,1,?,'2026-01-01T00:00:00Z')`, ownerJob, strings.Repeat("a", 64), ownerJob, strings.Repeat("b", 64), strings.Repeat("c", 64)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("MCE4D_MAINTENANCE_CHILD") == "hold" {
		fmt.Println("MAINTENANCE_READER_READY")
		if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != io.EOF {
			t.Fatalf("unexpected controller data: %v", err)
		}
	}
}

func maintenanceChild(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProtectedMaintenanceProcessFixture$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "MCE4D_MAINTENANCE_CHILD="+mode)
	return cmd
}

func TestProtectedQuiescentBackupFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	reader := maintenanceChild(ctx, "hold")
	input, err := reader.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := reader.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	reader.Stderr = os.Stderr
	if err := reader.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		if reader.ProcessState == nil {
			_ = reader.Process.Kill()
			_ = reader.Wait()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "MAINTENANCE_READER_READY\n" {
		t.Fatalf("reader did not hold live pool: %q %v", line, err)
	}
	if owner, err := openQuiescentOwner(ctx, root, ownerWorktree, 50*time.Millisecond, func(context.Context) error { return nil }); !errors.Is(err, runtime.ErrStoreMaintenance) || owner != nil {
		t.Fatalf("entered maintenance with another reader: %v", err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Wait(); err != nil {
		t.Fatal(err)
	}
	owner, err := openQuiescentOwner(ctx, root, ownerWorktree, time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if !owner.closed {
			_ = owner.Close()
		}
	}()
	other := maintenanceChild(ctx, "other")
	if result, err := other.CombinedOutput(); err != nil {
		t.Fatalf("unrelated worktree blocked: %s %v", result, err)
	}
	backupQuiescentFixture(t, ctx, owner)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	// Catalog is a separate explicit maintenance scope, not part of an atomic
	// cross-store/worktree promise. No database is replaced by this fixture.
	catalog, err := openQuiescentOwner(ctx, root, "catalog", time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	backupQuiescentFixture(t, ctx, catalog)
	fmt.Println("NATIVE_QUIESCENT_PAIR_CATALOG_BACKUP_AND_SCOPED_READER_GATES_PASS")
}

func backupQuiescentFixture(t *testing.T, ctx context.Context, owner *quiescentOwner) {
	t.Helper()
	if err := profileDirectory(owner.domain.root, "backups", uint32(unix.Geteuid()), uint32(unix.Getegid()), 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		t.Fatal(err)
	}
	name := "backups/" + owner.operation
	if err := profileDirectory(owner.domain.root, name, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0700); err != nil {
		t.Fatal(err)
	}
	destination, err := owner.domain.Child(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	defer destination.root.Close()
	base, err := destination.root.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range owner.stores {
		before, err := owner.authenticatedImage(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		if err := profileFile(destination.root, store.name, nil, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0600, false); err != nil {
			t.Fatal(err)
		}
		if err := owner.openReadPool(ctx, store); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.capture(ctx, store); !errors.Is(err, runtime.ErrLifecycleInUse) {
			t.Fatalf("main byte descriptor allowed while SQL alive: %v", err)
		}
		conn, err := owner.pool.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var readonly *modsqlite.Error
		_, err = conn.ExecContext(ctx, `UPDATE schema_meta SET schema_version=schema_version`)
		if !errors.As(err, &readonly) || readonly.Code()&0xff != 8 {
			t.Fatalf("source URI allowed mutation after query_only reset: %v", err)
		}
		image, copyErr := copyPinnedSQLiteBackup(ctx, conn, profileNativeDSN(filepath.Join(base, store.name)), 16<<20)
		if err := errors.Join(copyErr, conn.Close(), owner.closeReadPool()); err != nil {
			t.Fatal(err)
		}
		after, err := owner.authenticatedImage(ctx, store)
		if err != nil || before != after {
			t.Fatalf("source authority changed: %v", err)
		}
		info, err := os.Stat(filepath.Join(base, store.name))
		if err != nil || info.Size() != image.Bytes {
			t.Fatalf("backup main incomplete: %v", err)
		}
		db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(base, store.name)))
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		var integrity string
		err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity)
		if closeErr := db.Close(); err != nil || closeErr != nil || integrity != "ok" {
			t.Fatalf("backup integrity: %s %v %v", integrity, err, closeErr)
		}
	}
}

func TestProtectedBackupFutureSchemaRefusalFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	native, err := OpenProtectedWorktree(ctx, root, ownerWorktree, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, native.lease, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := native.Index.lifetime.pools[1].BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE schema_meta SET schema_version=3,min_reader_version=3`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(writer.Close(), native.Close()); err != nil {
		t.Fatal(err)
	}
	owner, err := openQuiescentOwner(ctx, root, ownerWorktree, time.Second, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store := owner.stores[0]
	before, err := owner.authenticatedImage(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.openReadPool(ctx, store); !errors.Is(err, index.ErrSchemaTooNew) || owner.pool != nil {
		t.Fatalf("newer schema opened for backup: %v", err)
	}
	after, err := owner.authenticatedImage(ctx, store)
	if err != nil || before != after {
		t.Fatalf("schema refusal mutated source: %v", err)
	}
	fmt.Println("NATIVE_BACKUP_SCHEMA_TOO_NEW_REFUSED_WITHOUT_MUTATION_PASS")
}

func TestProtectedCreateBackupCatalogFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	observe := func() walLogicalImage {
		owner, err := openQuiescentOwner(ctx, root, "catalog", time.Second, func(context.Context) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		}()
		image, err := owner.authenticatedImage(ctx, owner.stores[0])
		if err != nil {
			t.Fatal(err)
		}
		return image
	}
	before := observe()
	output := filepath.Join(profileStatePath(root), "backups", "public-catalog")
	options := ProtectedBackupOptions{Target: "catalog", Output: output, Wait: time.Second, MaxDatabaseBytes: 16 << 20}
	result, err := CreateProtectedBackup(ctx, root, options, func(context.Context) error { return nil }, nil)
	if err != nil || result.Target != "catalog" || result.WorktreeID != "" || result.FileCount != 2 {
		t.Fatalf("public backup: %+v %v", result, err)
	}
	raw, err := os.ReadFile(filepath.Join(output, "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != result.ManifestSHA256 {
		t.Fatal("manifest receipt mismatch")
	}
	var manifest StoreBackupManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.FormatVersion != 1 || manifest.Consistency != "single_store" || len(manifest.Stores) != 1 || manifest.Stores[0].Kind != "catalog" || !manifest.Revisions.ValidFor("") {
		t.Fatalf("manifest: %+v %v", manifest, err)
	}
	files, err := os.ReadDir(output)
	if err != nil || len(files) != 2 {
		t.Fatalf("keys/sidecars/unknown data in backup: %v", err)
	}
	contents := map[string][32]byte{}
	var bytes int64
	for _, entry := range files {
		data, err := os.ReadFile(filepath.Join(output, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		contents[entry.Name()] = sha256.Sum256(data)
		bytes += int64(len(data))
	}
	if bytes != result.TotalBytes || fmt.Sprintf("%x", contents["catalog.sqlite"]) != manifest.Stores[0].RawSHA256 {
		t.Fatal("backup file hashes/counts disagree")
	}
	if again, err := CreateProtectedBackup(ctx, root, options, func(context.Context) error { return nil }, nil); !errors.Is(err, unix.EEXIST) || again != (ProtectedBackupSummary{}) {
		t.Fatalf("existing output adopted: %+v %v", again, err)
	}
	after := map[string][32]byte{}
	for _, entry := range files {
		data, err := os.ReadFile(filepath.Join(output, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		after[entry.Name()] = sha256.Sum256(data)
	}
	if !reflect.DeepEqual(contents, after) || observe() != before {
		t.Fatal("backup/retry changed old output or source")
	}
	limited := options
	limited.Output = filepath.Join(filepath.Dir(output), "too-small")
	limited.MaxDatabaseBytes = 512
	if _, err := CreateProtectedBackup(ctx, root, limited, func(context.Context) error { return nil }, nil); !errors.Is(err, ErrStateLimit) {
		t.Fatalf("source budget ignored: %v", err)
	}
	if _, err := os.Lstat(limited.Output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("budget failure created output")
	}
	denied := errors.New("synthetic administrator revoked")
	revoked := options
	revoked.Output = filepath.Join(filepath.Dir(output), "revoked-backup")
	authorize := func(context.Context) error {
		info, err := os.Stat(filepath.Join(revoked.Output, "catalog.sqlite"))
		if err == nil && info.Size() > 0 {
			return denied
		}
		return nil
	}
	if _, err := CreateProtectedBackup(ctx, root, revoked, authorize, nil); !errors.Is(err, denied) {
		t.Fatalf("revoked backup completed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(revoked.Output, "backup.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("revoked backup published manifest")
	}
	if _, err := os.Lstat(filepath.Join(revoked.Output, "catalog.sqlite")); err != nil {
		t.Fatal("uncertain artifact was deleted")
	}
	if observe() != before {
		t.Fatal("failed backup changed source")
	}
	fmt.Println("NATIVE_CATALOG_BACKUP_MANIFEST_HASHES_NO_KEYS_RETRY_CAP_AND_REVOKE_PASS")
}
