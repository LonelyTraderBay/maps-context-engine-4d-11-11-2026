package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func bindingFixture(t *testing.T) (context.Context, *Store, *sql.DB, string, *lifecycleFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	db := leasedCatalogDB(t)
	base, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := db.Exec(`INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,?,?,?)`, fixtureRepo, filepath.Join(root, "git"), "fixture-repo", "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO worktrees(worktree_id,repo_id,canonical_root,filesystem_identity,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, fixtureWorktree, fixtureRepo, filepath.Join(root, "worktree"), "fixture-worktree", "registered", "now", "now"); err != nil {
		t.Fatal(err)
	}
	policy := model.PolicyAdminRequest{WorktreeID: fixtureWorktree, Policy: policyFixture(root)}
	if _, err := base.SetPolicy(ctx, body(t, policy)); err != nil {
		t.Fatal(err)
	}
	profile := model.ProfileAdminRequest{WorktreeID: fixtureWorktree, ProfileName: "default", Profile: profileFixture()}
	profileData, err := base.SetProfile(ctx, body(t, profile))
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL: %q %v", mode, err)
	}
	lease := &lifecycleFixture{mode: ports.SharedLease}
	store, err := OpenExistingLeased(ctx, db, lease)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, db, profileData.ProfileID, lease
}

func bindingAllowed(context.Context, string, string) error { return nil }

func TestReadBindingVerifiesCurrentCatalogAndAuthorization(t *testing.T) {
	ctx, store, db, profile, lease := bindingFixture(t)
	result, err := store.ReadBinding(ctx, fixtureWorktree, profile, bindingAllowed)
	if err != nil || result.WorktreeID != fixtureWorktree || result.RepoID != fixtureRepo || result.ProfileID != profile || result.PolicyEpoch != 1 || result.BindingRevision != 1 || result.Profile == nil || result.CatalogIncarnation == "" {
		t.Fatalf("binding: %+v %v", result, err)
	}
	if _, err := store.ReadBinding(ctx, fixtureWorktree, strings.Repeat("e", 64), bindingAllowed); !errors.Is(err, ErrProfileUnavailable) {
		t.Fatalf("unknown profile accepted: %v", err)
	}
	calls := 0
	result, err = store.ReadBinding(ctx, fixtureWorktree, profile, func(context.Context, string, string) error {
		calls++
		if calls == 2 {
			return errFixtureRevoked
		}
		return nil
	})
	if !errors.Is(err, errFixtureRevoked) || result.WorktreeID != "" || result.Profile != nil {
		t.Fatalf("late revocation leaked binding: %+v %v", result, err)
	}
	lease.failAt = lease.calls + 2
	if result, err := store.ReadBinding(ctx, fixtureWorktree, profile, bindingAllowed); !errors.Is(err, errFixtureRevoked) || result.WorktreeID != "" {
		t.Fatalf("lost lease leaked binding: %+v %v", result, err)
	}
	lease.failAt = 0
	if _, err := db.Exec(`UPDATE worktrees SET status='removed' WHERE worktree_id=?`, fixtureWorktree); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadBinding(ctx, fixtureWorktree, profile, bindingAllowed); !errors.Is(err, ErrWorktreeUnavailable) {
		t.Fatalf("removed worktree accepted: %v", err)
	}
}

func TestReadBindingRejectsOversizedCorruptPolicy(t *testing.T) {
	ctx, store, db, profile, _ := bindingFixture(t)
	digest := strings.Repeat("c", 64)
	large := `{"x":"` + strings.Repeat("a", maxAdminBytes) + `"}`
	if _, err := db.Exec(`INSERT INTO policies(worktree_id,policy_digest,config_json,created_at) VALUES(?,?,?,'now')`, fixtureWorktree, digest, large); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE worktree_policy_heads SET policy_digest=?,policy_epoch=2`, digest); err != nil {
		t.Fatal(err)
	}
	result, err := store.ReadBinding(ctx, fixtureWorktree, profile, bindingAllowed)
	if !errors.Is(err, ErrUnrecognizedStore) || result.WorktreeID != "" || result.Profile != nil {
		t.Fatalf("corrupt policy accepted: %+v %v", result, err)
	}
}

func TestReadBindingRejectsOversizedCorruptStatus(t *testing.T) {
	ctx, store, db, profile, _ := bindingFixture(t)
	if _, err := db.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE worktrees SET status=? WHERE worktree_id=?`, strings.Repeat("x", 70000), fixtureWorktree); err != nil {
		t.Fatal(err)
	}
	result, err := store.ReadBinding(ctx, fixtureWorktree, profile, bindingAllowed)
	if !errors.Is(err, ErrUnrecognizedStore) || result.WorktreeID != "" || result.Profile != nil {
		t.Fatalf("corrupt status accepted: %+v %v", result, err)
	}
}

func TestBindingStampReadsLiveHeadAndRejectsLateAuthorityLoss(t *testing.T) {
	ctx, store, _, profile, lease := bindingFixture(t)
	binding, err := store.ReadBinding(ctx, fixtureWorktree, profile, bindingAllowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CheckBindingStamp(ctx, binding, bindingAllowed); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := store.CheckBindingStamp(ctx, binding, func(context.Context, string, string) error {
		calls++
		if calls == 2 {
			return errFixtureRevoked
		}
		return nil
	}); !errors.Is(err, errFixtureRevoked) {
		t.Fatal("stamp survived an authorization loss during the transaction")
	}
	lease.failAt = lease.calls + 2
	if err := store.CheckBindingStamp(ctx, binding, bindingAllowed); err == nil {
		t.Fatal("stamp survived a lost lifecycle lease")
	}
}

func TestBindingStampRejectsChangedHeadOrMissingRegistration(t *testing.T) {
	for _, query := range []string{
		`UPDATE worktree_policy_heads SET policy_epoch=policy_epoch+1`,
		`UPDATE worktrees SET binding_revision=binding_revision+1`,
		`UPDATE worktrees SET status='removed'`,
		`UPDATE catalog_identity SET incarnation='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
	} {
		t.Run(query, func(t *testing.T) {
			ctx, store, db, profile, _ := bindingFixture(t)
			binding, err := store.ReadBinding(ctx, fixtureWorktree, profile, bindingAllowed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := store.CheckBindingStamp(ctx, binding, bindingAllowed); err == nil {
				t.Fatal("stale binding stamp was accepted")
			}
		})
	}
}
