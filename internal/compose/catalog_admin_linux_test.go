//go:build linux

package compose

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func TestCatalogAdminCLIRechecksAuthorityUnderRealLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := filepath.Join(t.TempDir(), "catalog")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	path := filepath.Join(dir, "catalog.sqlite")
	exclusive, err := fsruntime.AcquireLifecycle(ctx, root, "catalog", ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := fsruntime.AcquireCatalogMutation(ctx, exclusive, scopeJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	db := scopeDB(t, path, 1)
	maintenance, err := sqliteowner.OpenMaintenanceOwner(ctx, exclusive, db, "catalog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance.InitializeCatalog(ctx, mutation, scopeJob); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mutation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	const worktree = "11111111-1111-4111-8111-111111111111"
	const repo = "22222222-2222-4222-8222-222222222222"
	seed := scopeDB(t, path, 1)
	if _, err := seed.ExecContext(ctx, `INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,?,?,?)`, repo, "/fixture/git", "fixture-repo", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ExecContext(ctx, `INSERT INTO worktrees(worktree_id,repo_id,canonical_root,filesystem_identity,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, worktree, repo, "/fixture/worktree", "fixture-worktree", "registered", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	shared, err := fsruntime.AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	db = scopeDB(t, path, 1)
	owner, err := sqliteowner.OpenCatalogOwner(ctx, shared, db)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	mutation, err = fsruntime.AcquireCatalogMutation(ctx, shared, scopeJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer mutation.Close()
	checks, revokeAt := 0, 2
	admin := &CatalogAdmin{Store: store, Mutation: mutation, OperationID: scopeJob, WorktreeID: worktree, Authorize: func(ctx context.Context, kind, actualWorktree string) error {
		checks++
		if kind != "policy" || actualWorktree != worktree || checks == revokeAt {
			return errors.New("private denial")
		}
		return ctx.Err()
	}}
	policy := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/fixture/worktree"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 1024, MaxSourceBytes: 8192, MaxOutputBytes: 4096, SecretRulesVersion: "v1"}
	request := model.PolicyAdminRequest{WorktreeID: worktree, ExpectedEpoch: 0, Policy: policy}
	raw, _ := json.Marshal(request)
	if _, err := admin.SetPolicy(ctx, raw); !errors.Is(err, app.ErrAdminDenied) || checks != 2 {
		t.Fatalf("pre-commit revocation: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM policies`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("revoked policy persisted: %d %v", count, err)
	}
	checks, revokeAt = 0, 0
	result, err := cli.ExecuteAdmin(ctx, "policy", "", raw, 4, "", func(context.Context, string, string, string) (cli.AdminOwner, func() error, error) {
		return admin, func() error { return nil }, nil
	})
	if err != nil || result.ExitCode != 0 || checks != 2 {
		t.Fatalf("CLI policy mutation failed: %v", err)
	}
	registry, _ := contracts.LoadRegistry()
	response, err := registry.DecodeResponse(contracts.CLI, "CLI policy set", result.JSON, 131072, 4)
	if err != nil || response.Data.(model.PolicyAdminData).PolicyEpoch != 1 {
		t.Fatalf("CLI policy response: %v", err)
	}
	if _, err := admin.SetPolicy(ctx, raw); !errors.Is(err, app.ErrAdminConflict) {
		t.Fatalf("stale epoch accepted: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM policies`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected policy count: %d %v", count, err)
	}
}
