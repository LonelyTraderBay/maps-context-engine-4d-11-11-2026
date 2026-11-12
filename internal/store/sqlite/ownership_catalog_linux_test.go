//go:build linux

package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestCatalogOwnerUsesRealScopedLeases(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalog")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	path := filepath.Join(dir, "catalog.sqlite")
	maintenance, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := runtime.AcquireCatalogMutation(ctx, maintenance, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.AcquireWriter(ctx, maintenance, ownerJob, time.Second); !errors.Is(err, runtime.ErrInvalidLease) {
		t.Fatalf("catalog accepted an index writer: %v", err)
	}
	db := ownerDB(t, path, 1)
	maintenanceOwner, err := OpenMaintenanceOwner(ctx, maintenance, db, "catalog")
	if err != nil {
		t.Fatal(err)
	}
	incarnation, err := maintenanceOwner.InitializeCatalog(ctx, mutation, ownerJob)
	if err != nil || incarnation == "" {
		t.Fatalf("leased catalog initialization: %q %v", incarnation, err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL initialization: %q %v", mode, err)
	}
	if err := maintenanceOwner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mutation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatal(err)
	}
	shared, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	db = ownerDB(t, path, 1)
	owner, err := OpenCatalogOwner(ctx, shared, db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GroupMetadata(ctx, ownerWorktree); !errors.Is(err, catalog.ErrGroupUnavailable) {
		t.Fatalf("read without writer lock returned unexpected result: %v", err)
	}
	if _, err := store.Remove(ctx, ownerWorktree, 1, true); !errors.Is(err, catalog.ErrCatalogWriterRequired) {
		t.Fatalf("catalog mutation without writer accepted: %v", err)
	}
	writer, err := runtime.AcquireCatalogMutation(ctx, shared, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := store.WithMutation(ctx, writer, ownerJob)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Remove(ctx, ownerWorktree, 1, true); !errors.Is(err, catalog.ErrWorktreeUnavailable) {
		t.Fatalf("bound mutation returned unexpected result: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Remove(ctx, ownerWorktree, 1, true); !errors.Is(err, runtime.ErrInvalidLease) {
		t.Fatalf("released catalog writer accepted: %v", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); !errors.Is(err, ErrPoolBusy) {
		t.Fatalf("open catalog connection ignored: %v", err)
	}
	if err := shared.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("open catalog pool lost lifecycle: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := shared.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogSharedLeaseCannotInitializeEmptyStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalog")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	shared, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	writer, err := runtime.AcquireCatalogMutation(ctx, shared, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	db := ownerDB(t, filepath.Join(dir, "catalog.sqlite"), 1)
	defer db.Close()
	if _, err := catalog.InitializeLeased(ctx, db, shared, writer, ownerJob); !errors.Is(err, runtime.ErrInvalidLease) {
		t.Fatalf("shared catalog lease permitted initialization: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected initialization changed schema: %d %v", count, err)
	}
}
