package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const fixtureWorktree = "11111111-1111-4111-8111-111111111111"
const fixtureRepo = "22222222-2222-4222-8222-222222222222"

func seededStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db := testDB(t)
	s, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,?,?,?)`, fixtureRepo, "/fixture/git", "fixture-repo", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO worktrees(worktree_id,repo_id,canonical_root,filesystem_identity,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, fixtureWorktree, fixtureRepo, "/fixture/worktree", "fixture-worktree", "registered", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	return s, db
}

func policyFixture(root string) model.PolicyConfig {
	return model.PolicyConfig{
		SchemaVersion: 1, AllowedRoots: []string{root}, DeniedGlobs: []string{},
		TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{},
		MaxFileBytes: 1024, MaxSourceBytes: 8192, MaxOutputBytes: 4096, SecretRulesVersion: "v1",
	}
}

func profileFixture() *model.GoProfile {
	return &model.GoProfile{
		SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{"z", "a"},
		ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64),
		ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{},
	}
}

func body(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPolicyCASAndImmutableVersions(t *testing.T) {
	s, db := seededStore(t)
	ctx := context.Background()
	request := model.PolicyAdminRequest{WorktreeID: fixtureWorktree, Policy: policyFixture("/fixture/worktree")}
	first, err := s.SetPolicy(ctx, body(t, request))
	if err != nil || first.PolicyEpoch != 1 {
		t.Fatalf("initial policy: %+v %v", first, err)
	}
	request.Policy.AllowedRoots = []string{"/fixture/other"}
	if _, err := s.SetPolicy(ctx, body(t, request)); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale policy accepted: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM policies`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("stale request persisted a policy: %d %v", count, err)
	}
	request.ExpectedEpoch = 1
	second, err := s.SetPolicy(ctx, body(t, request))
	if err != nil || second.PolicyEpoch != 2 || second.PolicyDigest == first.PolicyDigest {
		t.Fatalf("policy advance: %+v %v", second, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM policies`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("immutable history missing: %d %v", count, err)
	}
	if _, err := db.Exec(`UPDATE policies SET config_json='{}' WHERE worktree_id=?`, fixtureWorktree); err == nil {
		t.Fatal("immutable policy was updated")
	}
}

func TestProfileCASAndNormalizedIdentity(t *testing.T) {
	s, db := seededStore(t)
	ctx := context.Background()
	profile := profileFixture()
	request := model.ProfileAdminRequest{WorktreeID: fixtureWorktree, ProfileName: "default", Profile: profile}
	first, err := s.SetProfile(ctx, body(t, request))
	if err != nil || first.Revision != 1 {
		t.Fatalf("initial profile: %+v %v", first, err)
	}
	profile.BuildTags = []string{"a", "z"}
	request.ExpectedRevision = 1
	second, err := s.SetProfile(ctx, body(t, request))
	if err != nil || second.Revision != 2 || second.ProfileID != first.ProfileID {
		t.Fatalf("normalized identity: %+v %v", second, err)
	}
	profile.TestsEnabled = true
	if _, err := s.SetProfile(ctx, body(t, request)); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale profile accepted: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM profiles`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("stale request persisted a profile: %d %v", count, err)
	}
	request.ExpectedRevision = 2
	third, err := s.SetProfile(ctx, body(t, request))
	if err != nil || third.Revision != 3 || third.ProfileID == first.ProfileID {
		t.Fatalf("profile advance: %+v %v", third, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM profiles`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("immutable profile history missing: %d %v", count, err)
	}
}

func TestAdminRejectsUnavailableAndChangedCatalog(t *testing.T) {
	s, db := seededStore(t)
	req := body(t, model.PolicyAdminRequest{WorktreeID: fixtureWorktree, Policy: policyFixture("/fixture/worktree")})
	if _, err := db.Exec(`UPDATE worktrees SET status='removed' WHERE worktree_id=?`, fixtureWorktree); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPolicy(context.Background(), req); !errors.Is(err, ErrWorktreeUnavailable) {
		t.Fatalf("removed worktree accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE catalog_identity SET incarnation='33333333-3333-4333-8333-333333333333'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPolicy(context.Background(), req); !errors.Is(err, ErrCatalogChanged) {
		t.Fatalf("changed incarnation accepted: %v", err)
	}
}

func TestWriteRollsBackOnFailure(t *testing.T) {
	s, db := seededStore(t)
	injected := errors.New("fixture failure")
	err := s.write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES('temporary','/temporary','temporary','2026-01-01T00:00:00Z')`)
		if err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("original failure was lost: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM repositories WHERE repo_id='temporary'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed transaction persisted: %d %v", count, err)
	}
}

func TestConcurrentPolicyCASHasSingleWinner(t *testing.T) {
	s, db := seededStore(t)
	req := body(t, model.PolicyAdminRequest{WorktreeID: fixtureWorktree, Policy: policyFixture("/fixture/worktree")})
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.SetPolicy(context.Background(), req)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrRevisionConflict):
			conflict++
		default:
			t.Fatalf("unexpected CAS error: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS results: success=%d conflict=%d", success, conflict)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM worktree_policy_heads WHERE policy_epoch=1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected head state: %d %v", count, err)
	}
}

func TestRemoveRequiresConfirmationAndPreservesCatalogHistory(t *testing.T) {
	s, db := seededStore(t)
	ctx := context.Background()
	policy := body(t, model.PolicyAdminRequest{WorktreeID: fixtureWorktree, Policy: policyFixture("/fixture/worktree")})
	profile := body(t, model.ProfileAdminRequest{WorktreeID: fixtureWorktree, ProfileName: "default", Profile: profileFixture()})
	if _, err := s.SetPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove(ctx, fixtureWorktree, 1, false); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed removal accepted: %v", err)
	}
	if _, err := s.Remove(ctx, fixtureWorktree, 2, true); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	revision, err := s.Remove(ctx, fixtureWorktree, 1, true)
	if err != nil || revision != 2 {
		t.Fatalf("remove: %d %v", revision, err)
	}
	if _, err := s.SetPolicy(ctx, policy); !errors.Is(err, ErrWorktreeUnavailable) {
		t.Fatalf("removed worktree accepted mutation: %v", err)
	}
	var status string
	var storedRevision, policies, profiles int
	if err := db.QueryRow(`SELECT status,binding_revision FROM worktrees WHERE worktree_id=?`, fixtureWorktree).Scan(&status, &storedRevision); err != nil || status != "removed" || storedRevision != 2 {
		t.Fatalf("removed worktree state: %q %d %v", status, storedRevision, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM policies WHERE worktree_id=?`, fixtureWorktree).Scan(&policies); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM profiles WHERE worktree_id=?`, fixtureWorktree).Scan(&profiles); err != nil || policies != 1 || profiles != 1 {
		t.Fatalf("history was removed: %d %d %v", policies, profiles, err)
	}
}
