//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

func TestProtectedWorktreeProvisionFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	first, err := ProvisionProtectedWorktree(ctx, root, ownerWorktree)
	if err != nil || !first.Verified || first.WorktreeID != ownerWorktree || first.IndexIncarnation == first.RecordsIncarnation {
		t.Fatalf("worktree slot bootstrap: %+v %v", first, err)
	}
	if again, err := ProvisionProtectedWorktree(ctx, root, ownerWorktree); !errors.Is(err, ErrProfileExists) || again != (ProtectedWorktreeSummary{}) {
		t.Fatal("existing worktree slot was adopted/overwritten", err)
	}
	if _, err := ProvisionProtectedWorktree(ctx, root, "../target"); err == nil {
		t.Fatal("invalid worktree path accepted")
	}
}

func TestProtectedWorktreeRoundtripFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	var allowed atomic.Bool
	allowed.Store(true)
	authorize := func(context.Context) error {
		if !allowed.Load() {
			return ErrProfileDenied
		}
		return nil
	}
	owner, err := OpenProtectedWorktree(ctx, root, ownerWorktree, authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	indexStore, err := owner.Index.Store()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := indexStore.Identity(ctx)
	if err != nil || identity.Incarnation != owner.manifest.IndexIncarnation {
		t.Fatal("protected index identity", err)
	}
	recordStore, err := owner.Records.Store()
	if err != nil {
		t.Fatal(err)
	}
	state, err := recordStore.ReadState(ctx, 100, func(context.Context, records.Identity) error { return nil })
	if err != nil || state.Identity.Incarnation != owner.manifest.RecordsIncarnation || state.RecordEpoch != 0 {
		t.Fatal("protected durable records", err)
	}
	if err := owner.lease.Check(ctx, ownerWorktree, ports.SharedLease); err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, owner.lease, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	readers, writes := owner.Index.lifetime.pools[0], owner.Index.lifetime.pools[1]
	read, err := readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := read.QueryRowContext(ctx, "SELECT count(*) FROM generations").Scan(&before); err != nil {
		t.Fatal(err)
	}
	write, err := writes.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := write.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES('77777777-7777-4777-8777-777777777777',?,'BUILDING',?,?,1,?,'2026-11-14T00:00:00Z')`, strings.Repeat("a", 64), ownerJob, strings.Repeat("b", 64), strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(); err != nil {
		t.Fatal("actual profile capture/receipt worker", err)
	}
	var pinned int
	if err := read.QueryRowContext(ctx, "SELECT count(*) FROM generations").Scan(&pinned); err != nil || pinned != before {
		t.Fatal("reader snapshot changed through native WAL", err)
	}
	if err := read.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	allowed.Store(false)
	if _, err := indexStore.Identity(ctx); err == nil {
		t.Fatal("revoked owner read data")
	}
	// Permission revocation must not prevent closing native connections.
	if err := owner.Close(); err != nil {
		t.Fatal("revoked owner stranded native pools", err)
	}
	allowed.Store(true)
	owner, err = OpenProtectedWorktree(ctx, root, ownerWorktree, authorize)
	if err != nil {
		t.Fatal("reopen authenticated store after checkpoint", err)
	}
	defer owner.Close()
	tx, err := owner.Index.lifetime.pools[0].BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var after int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM generations").Scan(&after); err != nil || after != before+1 {
		t.Fatal("acknowledged generation lost on reopen", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("PROTECTED_WORKTREE_ROUNDTRIP_PASS")
}
