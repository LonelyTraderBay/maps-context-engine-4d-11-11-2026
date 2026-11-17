//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// A test-only process exits with a live native SQL transaction, after the
// durable PREPARE and an actual write. Production dispatchers have no such role.
func TestProtectedPendingCrashChildFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	kind := os.Getenv("MCE4D_PROFILE_PENDING_KIND")
	var pool *sql.DB
	if kind == "catalog" {
		state, err := OpenProtectedState(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := state.OpenCatalog(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.AcquireCatalogMutation(ctx, state.lease, ownerJob, time.Second); err != nil {
			t.Fatal(err)
		}
		pool = state.catalog.lifetime.pools[0]
	} else if kind == "index" || kind == "records" {
		owner, err := OpenProtectedWorktree(ctx, root, ownerWorktree, func(ctx context.Context) error { return ctx.Err() })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.AcquireWriter(ctx, owner.lease, ownerJob, time.Second); err != nil {
			t.Fatal(err)
		}
		pool = owner.Index.lifetime.pools[1]
		if kind == "records" {
			// The public R1 records owner is read-only. A private test pool
			// exercises its WAL recovery primitive without adding a product API.
			pool = owner.stores[1].pool(false, 1)
		}
	} else {
		t.Fatal("invalid synthetic pending kind")
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	switch kind {
	case "catalog":
		_, err = tx.ExecContext(ctx, `INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,'/synthetic-uncommitted','uncommitted','2026-11-14T00:00:00Z')`, ownerJob)
	case "index":
		_, err = tx.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING',?,?,1,?,'2026-11-14T00:00:00Z')`, ownerJob, strings.Repeat("a", 64), ownerJob, strings.Repeat("b", 64), strings.Repeat("c", 64))
	case "records":
		_, err = tx.ExecContext(ctx, `UPDATE records_meta SET record_epoch=1 WHERE singleton=1`)
	}
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("PREPARED_NATIVE_CRASH:" + kind)
	os.Exit(66)
}

func TestProtectedPendingRecoveryFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	for _, kind := range []string{"index", "records", "catalog"} {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.CommandContext(ctx, executable, "-test.run=^TestProtectedPendingCrashChildFixture$", "-test.timeout=10s")
		child.Env = append(os.Environ(), "MCE4D_PROFILE_PENDING_KIND="+kind)
		out, err := child.CombinedOutput()
		var exited *exec.ExitError
		if !errors.As(err, &exited) || exited.ExitCode() != 66 || !bytes.Contains(out, []byte("PREPARED_NATIVE_CRASH:"+kind)) {
			t.Fatalf("%s did not reach prepared crash: %v %s", kind, err, out)
		}
		journal := filepath.Join(worktreeStatePath(root, ownerWorktree), ".maintenance", kind, walAuthorityName)
		var recoverPending func() error
		if kind == "catalog" {
			journal = filepath.Join(profileStatePath(root), ".maintenance/catalog", walAuthorityName)
			state, err := OpenProtectedState(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if store, err := state.OpenCatalog(ctx); !errors.Is(err, errWALAuthorityPending) || store != nil {
				t.Fatal("catalog admitted an unacknowledged transaction", err)
			}
			if err := state.Close(); err != nil {
				t.Fatal(err)
			}
			recoverPending = func() error { return RecoverProtectedCatalogPending(ctx, root) }
		} else {
			if owner, err := OpenProtectedWorktree(ctx, root, ownerWorktree, func(ctx context.Context) error { return ctx.Err() }); !errors.Is(err, errWALAuthorityPending) || owner != nil {
				t.Fatal("worktree admitted an unacknowledged transaction", err)
			}
			recoverPending = func() error {
				return RecoverProtectedWorktreePending(ctx, root, ownerWorktree, func(ctx context.Context) error { return ctx.Err() })
			}
		}
		before, err := os.ReadFile(journal)
		if err != nil {
			t.Fatal(err)
		}
		if err := recoverPending(); err != nil {
			t.Fatalf("%s unchanged recovery: %v", kind, err)
		}
		after, err := os.ReadFile(journal)
		if err != nil || len(after) <= len(before) || !bytes.HasPrefix(after, before) {
			t.Fatal("recovery did not append a durable settlement", err)
		}
		if err := recoverPending(); err != nil {
			t.Fatal("repeat settled recovery", err)
		}
		repeated, err := os.ReadFile(journal)
		if err != nil || !bytes.Equal(after, repeated) {
			t.Fatal("repeat recovery changed a settled journal", err)
		}
	}
	owner, err := OpenProtectedWorktree(ctx, root, ownerWorktree, func(ctx context.Context) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	for i, query := range []string{"SELECT count(*) FROM generations", "SELECT record_epoch FROM records_meta WHERE singleton=1"} {
		pool := owner.Index.lifetime.pools[0]
		if i == 1 {
			pool = owner.Records.lifetime.pools[0]
		}
		tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := tx.QueryRowContext(ctx, query).Scan(&n); err != nil || n != 0 {
			t.Fatal("uncommitted native write survived recovery", n, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	state, err := OpenProtectedState(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if _, err := state.OpenCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := state.catalog.lifetime.pools[0].BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM repositories").Scan(&n); err != nil || n != 0 {
		t.Fatal("uncommitted catalog registration survived recovery", n, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("PROTECTED_PENDING_RECOVERY_PASS:index,records,catalog; real crash; exact image; no duplicate settlement")
}
