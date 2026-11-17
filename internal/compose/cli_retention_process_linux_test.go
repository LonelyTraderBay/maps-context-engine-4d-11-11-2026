//go:build linux

package compose

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

// The child is used only by the parent-owned synthetic fixture. Its marker
// prevents an accidental test invocation from opening arbitrary state.
func TestRetainedCLIProcessChild(t *testing.T) {
	if os.Getenv("MCE4D_RETAINED_CLI_CHILD") != "1" {
		return
	}
	code, err := retainedCLIProcessChild()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "retained CLI fixture setup failed")
		os.Exit(9)
	}
	os.Exit(code)
}

func retainedFixtureDB(name string, limit int) (*sql.DB, error) {
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(name)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err == nil {
		db.SetMaxOpenConns(limit)
	}
	return db, err
}

func retainedCLIProcessChild() (code int, resultErr error) {
	base, nonce := os.Getenv("MCE4D_RETAINED_CLI_BASE"), os.Getenv("MCE4D_RETAINED_CLI_NONCE")
	worktreeID, profileID := os.Getenv("MCE4D_RETAINED_CLI_WORKTREE"), os.Getenv("MCE4D_RETAINED_CLI_PROFILE")
	action := os.Getenv("MCE4D_RETAINED_CLI_ACTION")
	planJSON := os.Getenv("MCE4D_RETAINED_CLI_PLAN")
	renameProvider := os.Getenv("MCE4D_RETAINED_CLI_RENAME_PROVIDER")
	rename := renameProvider != ""
	if !filepath.IsAbs(base) || filepath.Clean(base) != base || len(nonce) != 64 || len(planJSON) > 32768 || (!rename && action != "context" && action != "expand") || (rename && (planJSON != "" || action != "rename-preview" || renameProvider != "b94804c7a4f74bcf0765263cb022b78459fc61258fe30c67607106af96fa5029")) {
		return 9, errors.New("invalid fixture")
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return 9, err
	}
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil || resolved != base {
		return 9, errors.New("unrecognized fixture root")
	}
	markerName := ".retained-cli-fixture"
	if planJSON != "" {
		markerName = ".retained-cli-plan-fixture"
	}
	if rename {
		markerName = ".retained-cli-rename-fixture"
	}
	marker, err := os.ReadFile(filepath.Join(base, markerName))
	if err != nil || string(marker) != nonce {
		return 9, errors.New("unrecognized fixture marker")
	}
	scope := model.ScopeRequest{WorktreeID: &worktreeID, ProfileID: &profileID}
	if mode, err := scope.SelectionMode(1); err != nil || mode != "single" {
		return 9, errors.New("invalid fixture binding")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repo, catalogDir, worktreeDir := filepath.Join(base, "repo"), filepath.Join(base, "catalog"), filepath.Join(base, "worktree")
	root, err := paths.OpenRoot(repo)
	if err != nil {
		return 9, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	common, err := runtimeadapter.OpenLocalGitMetadata(root)
	if err != nil {
		return 9, err
	}
	defer func() { resultErr = errors.Join(resultErr, common.Close()) }()
	catalogRoot, err := paths.OpenRoot(catalogDir)
	if err != nil {
		return 9, err
	}
	defer func() { resultErr = errors.Join(resultErr, catalogRoot.Close()) }()
	worktreeRoot, err := paths.OpenRoot(worktreeDir)
	if err != nil {
		return 9, err
	}
	defer func() { resultErr = errors.Join(resultErr, worktreeRoot.Close()) }()
	catalogLease, err := runtimeadapter.AcquireLifecycle(ctx, catalogRoot, "catalog", ports.SharedLease, 5*time.Second)
	if err != nil {
		return 9, err
	}
	defer func() { resultErr = errors.Join(resultErr, catalogLease.Close()) }()
	worktreeLease, err := runtimeadapter.AcquireLifecycle(ctx, worktreeRoot, worktreeID, ports.SharedLease, 5*time.Second)
	if err != nil {
		return 9, err
	}
	defer func() { resultErr = errors.Join(resultErr, worktreeLease.Close()) }()
	catalogDB, err := retainedFixtureDB(filepath.Join(catalogDir, "catalog.sqlite"), 1)
	if err != nil {
		return 9, err
	}
	catalogOwner, err := sqliteowner.OpenCatalogOwner(ctx, catalogLease, catalogDB)
	if err != nil {
		return 9, errors.Join(err, catalogDB.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, catalogOwner.Close()) }()
	readDB, err := retainedFixtureDB(filepath.Join(worktreeDir, "index.sqlite"), 4)
	if err != nil {
		return 9, err
	}
	writeDB, err := retainedFixtureDB(filepath.Join(worktreeDir, "index.sqlite"), 1)
	if err != nil {
		return 9, errors.Join(err, readDB.Close())
	}
	indexOwner, err := sqliteowner.OpenIndexOwner(ctx, worktreeLease, readDB, writeDB, worktreeID)
	if err != nil {
		return 9, errors.Join(err, readDB.Close(), writeDB.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, indexOwner.Close()) }()
	recordDB, err := retainedFixtureDB(filepath.Join(worktreeDir, "records.sqlite"), 1)
	if err != nil {
		return 9, err
	}
	recordOwner, err := sqliteowner.OpenRecordsOwner(ctx, worktreeLease, recordDB, worktreeID)
	if err != nil {
		return 9, errors.Join(err, recordDB.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, recordOwner.Close()) }()
	catalog, err := catalogOwner.Store()
	if err != nil {
		return 9, err
	}
	index, err := indexOwner.Store()
	if err != nil {
		return 9, err
	}
	records, err := recordOwner.Store()
	if err != nil {
		return 9, err
	}
	principal := app.CorePrincipal{ID: "fixture", Digest: strings.Repeat("d", 64), BindingDigest: strings.Repeat("e", 64)}
	access := func(_ context.Context, candidate app.CorePrincipal, worktree, profile string) error {
		if candidate != principal || worktree != worktreeID || profile != profileID {
			return ErrAuthority
		}
		return nil
	}
	reader := &ScopeReader{Catalog: catalog, Index: index, Records: records, WorktreeRoot: root, CommonRoot: common, WorktreeID: worktreeID, ProfileID: profileID, Access: access, MaxHeads: 100, MaxManifestBytes: 8192}
	authority := &BoundCoreAuthority{Reader: reader, AuthenticatePrincipal: func(context.Context) (app.CorePrincipal, error) { return principal, nil }, AuthorizePath: func(_ context.Context, candidate app.CorePrincipal, policy model.PolicyConfig, name string) error {
		if candidate != principal || len(policy.AllowedRoots) == 0 || policy.AllowedRoots[0] != repo || name != "main.go" && name != "other.go" && name != "go.mod" && name != "design.md" && name != "vocabulary.json" {
			return ErrAuthority
		}
		return nil
	}}
	codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
	if err != nil {
		return 9, err
	}
	configStore, err := runtimeadapter.NewRuntimeConfigStore(catalogRoot, "fixture-config.json")
	if err != nil {
		return 9, err
	}
	config, err := app.NewRuntimeConfigService(ctx, configStore, 4)
	if err != nil {
		return 9, err
	}
	cache, err := app.NewFreshnessPackCache(codec, 32, 32*1024*1024, 900)
	if err != nil {
		return 9, err
	}
	backend, err := NewCoreBackend(config, reader, authority, codec, cache, nil)
	if err != nil {
		return 9, err
	}
	if planJSON != "" {
		var items []overview.DocumentDeclaration
		if json.Unmarshal([]byte(planJSON), &items) != nil || len(items) == 0 || len(items) > 512 {
			return 9, errors.New("invalid fixture declarations")
		}
		for _, item := range items {
			if item.Source.WorktreeID != worktreeID || item.Source.ProfileID != profileID || item.Source.GenerationID != scopeGeneration {
				return 9, errors.New("invalid fixture declaration scope")
			}
		}
		revision, err := DeclarationRevisionDigest("fixture-registry", "revision-1")
		if err != nil {
			return 9, err
		}
		recheck := func(ctx context.Context) error {
			value, err := os.ReadFile(filepath.Join(base, markerName))
			if err != nil || string(value) != nonce {
				return ErrAuthority
			}
			return ctx.Err()
		}
		provider := func(ctx context.Context, actor app.CorePrincipal, scope model.ScopeVector, request model.PackRequest) (PlanDeclarations, error) {
			if actor != principal || len(scope.Members) != 1 || scope.Members[0].WorktreeID != worktreeID || scope.Members[0].ProfileID != profileID || request.TaskKind != "plan" {
				return PlanDeclarations{}, ErrAuthority
			}
			if err := recheck(ctx); err != nil {
				return PlanDeclarations{}, err
			}
			return PlanDeclarations{Items: append([]overview.DocumentDeclaration(nil), items...), RetainedBytes: 4096, RevisionDigest: revision, Recheck: recheck, AuthorizeDocument: func(ctx context.Context, scope overview.Scope, declaration overview.DocumentDeclaration) error {
				if scope.Identity.WorktreeID != worktreeID || scope.Identity.ProfileID != profileID || scope.Identity.GenerationID != scopeGeneration {
					return ErrAuthority
				}
				for _, item := range items {
					if item == declaration {
						return recheck(ctx)
					}
				}
				return ErrAuthority
			}}, nil
		}
		backend, err = NewCoreBackendWithPlan(config, reader, authority, codec, cache, nil, provider)
		if err != nil {
			return 9, err
		}
	}
	if rename {
		// This child proves retained continuation only. It has neither a typed
		// loader nor a live workspace verifier; initial rename and freshness
		// claims must remain unavailable. The parent validates with a real
		// fixture workspace verifier after collecting every page.
		backend.verifyWorkspace = func(context.Context, app.CorePrincipal, model.ScopeMember, *snapshot.Snapshot) (app.WorkspaceObservation, error) {
			return app.WorkspaceUnknown, nil
		}
		scanner, _ := policy.NewScanner(32 << 20)
		check := func(ctx context.Context, actor app.CorePrincipal, asked model.ScopeVector) error {
			value, err := os.ReadFile(filepath.Join(base, markerName))
			if err != nil || string(value) != nonce || actor != principal {
				return ErrAuthority
			}
			_, err = reader.Recheck(ctx, actor, asked)
			return err
		}
		spool, err := lsp.NewBudgetedResultStore(worktreeRoot, worktreeLease, worktreeID, scanner, 2<<20, 4<<20, 900, 8,
			func(ctx context.Context) error { return access(ctx, principal, worktreeID, profileID) },
			func(context.Context, int64) (packstore.BudgetReservation, error) {
				return scopeBudgetReservation{}, nil
			})
		if err != nil {
			return 9, err
		}
		backend, err = backend.WithRenamePreview(RenameOwner{Store: spool, ProviderDigest: renameProvider, Check: check, Open: func(context.Context, app.CorePrincipal, model.ScopeVector, model.RenameInitialRequest, *snapshot.Snapshot) (lsp.SessionConfig, func() error, error) {
			return lsp.SessionConfig{}, nil, errors.New("continuation attempted to restart provider")
		}})
		if err != nil {
			return 9, err
		}
	} else {
		store, err := packstore.New(worktreeRoot, []*paths.Root{root, catalogRoot}, worktreeLease, worktreeID, 16*1024*1024, 32*1024*1024, 32,
			func(ctx context.Context) error { return access(ctx, principal, worktreeID, profileID) },
			func(context.Context, int64) (packstore.BudgetReservation, error) {
				return scopeBudgetReservation{}, nil
			})
		if err != nil {
			return 9, err
		}
		backend, err = backend.WithCLIPackRetention(store)
		if err != nil {
			return 9, err
		}
	}
	service, err := app.NewCoreService(config, authority, backend, nil)
	if err != nil {
		return 9, err
	}
	open := func(_ context.Context, configPath, worktree, profile string) (*app.CoreService, func() error, error) {
		if configPath != "" || worktree != "" || profile != "" {
			return nil, nil, cli.ErrBindingDenied
		}
		return service, func() error { return nil }, nil
	}
	code = cli.Run(ctx, []string{"query", action, "--input-json", "-"}, os.Stdin, os.Stdout, os.Stderr, cli.Dependencies{CPUs: 4, OpenRetainedCore: open})
	return code, nil
}

func runRetainedCLIProcess(t *testing.T, ctx context.Context, base, nonce, worktree, profile, action string, raw []byte, planItems []overview.DocumentDeclaration) (model.Response, int) {
	t.Helper()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRetainedCLIProcessChild$")
	cmd.Env = append(os.Environ(), "MCE4D_RETAINED_CLI_CHILD=1", "MCE4D_RETAINED_CLI_BASE="+base, "MCE4D_RETAINED_CLI_NONCE="+nonce,
		"MCE4D_RETAINED_CLI_WORKTREE="+worktree, "MCE4D_RETAINED_CLI_PROFILE="+profile, "MCE4D_RETAINED_CLI_ACTION="+action)
	planJSON := ""
	if planItems != nil {
		value, err := json.Marshal(planItems)
		if err != nil || len(value) > 32768 {
			t.Fatal("invalid fixture plan items")
		}
		planJSON = string(value)
	}
	cmd.Env = append(cmd.Env, "MCE4D_RETAINED_CLI_PLAN="+planJSON)
	renameProvider := ""
	if action == "rename-preview" {
		renameProvider = "b94804c7a4f74bcf0765263cb022b78459fc61258fe30c67607106af96fa5029"
	}
	cmd.Env = append(cmd.Env, "MCE4D_RETAINED_CLI_RENAME_PROVIDER="+renameProvider)
	cmd.Stdin = bytes.NewReader(raw)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil || cmd.ProcessState == nil || errOut.Len() != 0 {
		t.Fatalf("fixture process %s failed: %v", action, err)
	}
	code := cmd.ProcessState.ExitCode()
	if code != 0 && code != 3 && code != 6 {
		t.Fatalf("fixture process %s exit=%d", action, code)
	}
	if bytes.Contains(out.Bytes(), []byte(base)) {
		t.Fatal("fixture path appeared in CLI output")
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	name := map[string]string{"context": "mce4d_context_pack", "expand": "mce4d_expand_pack", "rename-preview": "mce4d_rename_preview"}[action]
	response, err := registry.DecodeResponse(contracts.MCP, name, bytes.TrimSpace(out.Bytes()), 131072, 4)
	if err != nil {
		t.Fatalf("fixture process %s returned invalid output: %v", action, err)
	}
	return response, code
}
