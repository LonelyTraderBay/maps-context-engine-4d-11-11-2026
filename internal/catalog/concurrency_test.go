package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"modernc.org/sqlite"
)

func TestPolicyCASAcrossIndependentConnections(t *testing.T) {
	file := filepath.Join(t.TempDir(), "catalog.sqlite")
	dsn := fixtureSQLiteURI(file) + "?_pragma=busy_timeout%281000%29&_pragma=journal_mode%28WAL%29&_pragma=synchronous%28FULL%29"
	stores := make([]*Store, 2)
	dbs := make([]*sql.DB, 2)
	for i := range dbs {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = db.Close() })
		dbs[i] = db
		store, err := Open(context.Background(), db)
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = store
	}
	if _, err := dbs[0].Exec(`INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,?,?,?)`, fixtureRepo, "/fixture/git", "fixture-repo", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := dbs[0].Exec(`INSERT INTO worktrees(worktree_id,repo_id,canonical_root,filesystem_identity,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, fixtureWorktree, fixtureRepo, "/fixture/worktree", "fixture-worktree", "registered", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	request := body(t, model.PolicyAdminRequest{WorktreeID: fixtureWorktree, Policy: policyFixture("/fixture/worktree")})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, len(stores))
	var wg sync.WaitGroup
	for _, store := range stores {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			<-start
			_, err := store.SetPolicy(ctx, request)
			errs <- err
		}(store)
	}
	close(start)
	wg.Wait()
	close(errs)
	succeeded, conflicted, busy := 0, 0, 0
	for err := range errs {
		var sqliteErr *sqlite.Error
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrRevisionConflict):
			conflicted++
		case errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == 5:
			busy++
		default:
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if succeeded != 1 || conflicted+busy != 1 {
		t.Fatalf("concurrent results: success=%d conflict=%d busy=%d", succeeded, conflicted, busy)
	}
	t.Logf("concurrent results: success=%d conflict=%d busy=%d", succeeded, conflicted, busy)
	if busy != 0 {
		// The transaction that returned BUSY did not commit; re-evaluating the
		// full request after both workers stop must see the committed revision.
		if _, err := stores[1].SetPolicy(ctx, request); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("retry after busy did not observe winner: %v", err)
		}
	}
	var policies, heads int
	if err := dbs[0].QueryRowContext(ctx, `SELECT count(*) FROM policies WHERE worktree_id=?`, fixtureWorktree).Scan(&policies); err != nil {
		t.Fatal(err)
	}
	if err := dbs[0].QueryRowContext(ctx, `SELECT count(*) FROM worktree_policy_heads WHERE worktree_id=? AND policy_epoch=1`, fixtureWorktree).Scan(&heads); err != nil || policies != 1 || heads != 1 {
		t.Fatalf("partial or duplicate policy state: policies=%d heads=%d err=%v", policies, heads, err)
	}
}
