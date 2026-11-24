package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"golang.org/x/sys/windows"
	modsqlite "modernc.org/sqlite"
)

// The native fixture re-execs this test binary under each AppContainer role.
// The role dispatcher and crash hook below are reachable only in test builds.
func TestMain(m *testing.M) {
	if handled, code := RunProtectedWorker(context.Background(), os.Args[1:], os.Stdout); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func TestWindowsRestoreCrashWorker(t *testing.T) {
	point := os.Getenv("MCE4D_WINDOWS_RESTORE_TEST_BOUNDARY")
	if point == "" {
		t.Skip("requires the disposable Windows restore fixture")
	}
	switch point {
	case "PREPARED", "catalog_main", "records_main", "VERIFYING":
	default:
		t.Fatalf("unknown restore crash boundary %q", point)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := restoreWindowsProtectedStore(ctx, os.Getenv("MCE4D_WINDOWS_RESTORE_TEST_ROOT"),
		os.Getenv("MCE4D_WINDOWS_RESTORE_TEST_MANIFEST"), true, 4*time.Second, func(at string) {
			if at != point {
				return
			}
			if err := windows.TerminateProcess(windows.CurrentProcess(), 79); err != nil {
				t.Fatal(err)
			}
		})
	t.Fatalf("restore did not terminate at %s: %v", point, err)
}

func TestWindowsInterruptedIndexWorker(t *testing.T) {
	root, worktree := os.Getenv("MCE4D_WINDOWS_INDEX_TEST_ROOT"), os.Getenv("MCE4D_WINDOWS_INDEX_TEST_WORKTREE")
	if root == "" || worktree == "" {
		t.Skip("requires the disposable Windows index fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	_, err := withWindowsStoreWriter(ctx, root, "worktree", worktree, "index-fixture", ports.SharedLease, 4*time.Second,
		func(o windowsStoreMaintenance) (struct{}, error) {
			return runWindowsIndexWAL(ctx, o, func(context.Context) error { return o.check() }, windowsCatalogSeedMaxSize,
				func(*index.Store, *runtime.Writer, string) (_ struct{}, resultErr error) {
					stores, release, err := o.openStores()
					if err != nil {
						return struct{}{}, err
					}
					defer func() { resultErr = errors.Join(resultErr, release()) }()
					path, err := stores[0].data.control.CanonicalPath()
					if err != nil {
						return struct{}{}, err
					}
					hold, err := o.lease.Hold(ctx)
					if err != nil {
						return struct{}{}, err
					}
					db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: windowsSQLiteRW(filepath.Join(path, stores[0].main))})
					db.SetMaxOpenConns(1)
					defer func() { resultErr = errors.Join(resultErr, db.Close(), hold.Close()) }()
					if _, err := db.ExecContext(ctx, "PRAGMA user_version=42"); err != nil {
						return struct{}{}, err
					}
					if os.Getenv("MCE4D_WINDOWS_INDEX_TEST_CHECKPOINT") == "1" {
						var busy, pages, written int
						if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(FULL)").Scan(&busy, &pages, &written); err != nil ||
							busy != 0 || pages < 1 || written != pages {
							t.Fatalf("fixture checkpoint busy=%d pages=%d written=%d: %v", busy, pages, written, err)
						}
					}
					if os.Getenv("MCE4D_WINDOWS_INDEX_TEST_TORN_RECEIPT") == "1" {
						tail, err := os.OpenFile(filepath.Join(path, walAuthorityName), os.O_WRONLY|os.O_APPEND, 0)
						if err != nil {
							return struct{}{}, err
						}
						_, writeErr := tail.WriteString("MCEWAL1\x00")
						if err := errors.Join(writeErr, tail.Sync(), tail.Close()); err != nil {
							return struct{}{}, err
						}
					}
					return struct{}{}, windows.TerminateProcess(windows.CurrentProcess(), 79)
				}, nil)
		})
	t.Fatalf("index did not stop with its SQLite state pending: %v", err)
}

func TestWindowsIndexRollbackCrashWorker(t *testing.T) {
	point := os.Getenv("MCE4D_WINDOWS_INDEX_TEST_RECOVERY_BOUNDARY")
	if point == "" {
		t.Skip("requires the disposable Windows index rollback fixture")
	}
	switch point {
	case "PREPARED", "retired-wal", "index_main", "VERIFYING":
	default:
		t.Fatalf("unknown index recovery boundary %q", point)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := recoverWindowsProtectedStore(ctx, os.Getenv("MCE4D_WINDOWS_INDEX_TEST_ROOT"), "worktree",
		os.Getenv("MCE4D_WINDOWS_INDEX_TEST_WORKTREE"), true, 4*time.Second, func(at string) {
			if at == point {
				if err := windows.TerminateProcess(windows.CurrentProcess(), 79); err != nil {
					t.Fatal(err)
				}
			}
		})
	t.Fatalf("index recovery did not stop at %s: %v", point, err)
}
