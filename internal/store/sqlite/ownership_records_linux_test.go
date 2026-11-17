//go:build linux

package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

func TestRecordsOwnerHoldsRealLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exclusive, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, exclusive, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "records.sqlite")
	db := ownerDB(t, path, 1)
	maintenance, err := OpenMaintenanceOwner(ctx, exclusive, db, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := maintenance.InitializeRecords(ctx, writer, ownerJob)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	shared, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	db = ownerDB(t, path, 1)
	owner, err := OpenRecordsOwner(ctx, shared, db, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.ReadState(ctx, 100, func(context.Context, records.Identity) error { return nil })
	if err != nil || state.Identity != identity || state.RecordEpoch != 0 || state.ApprovedHeads != 0 {
		t.Fatalf("records state: %+v %v", state, err)
	}
	if err := shared.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("owner did not retain lifecycle: %v", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); !errors.Is(err, ErrPoolBusy) {
		t.Fatalf("owner released borrowed connection: %v", err)
	}
	if _, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, 100*time.Millisecond); !errors.Is(err, runtime.ErrStoreMaintenance) {
		t.Fatalf("maintenance bypassed shared owner: %v", err)
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
