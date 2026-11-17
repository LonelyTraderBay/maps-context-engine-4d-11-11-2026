//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// Test-only re-exec dispatch lets the real provisioner bootstrap a root-owned
// test executable. Production tools contain neither this dispatcher nor the
// synthetic mutation/crash callbacks below.
func TestMain(m *testing.M) {
	if handled, code := RunProtectedWorker(context.Background(), os.Args[1:], os.Stdout); handled {
		os.Exit(code)
	}
	if len(os.Args) == 3 && (os.Args[1] == "__bootstrap-profile-v1" || os.Args[1] == "__check-profile-v1") {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if err := HardenProtectedProfileWorker(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(5)
		}
		report, err := ProtectedProfileWorker(ctx, os.Args[2], os.Args[1] == "__bootstrap-profile-v1")
		if err == nil {
			err = EncodeProfileSummary(os.Stdout, report)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(5)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestWALAuthorityLogQuiescedCommit(t *testing.T) {
	f := newAuthorityLogFixture(t)
	log, err := f.open(&f.image, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	op := sha256.Sum256([]byte("quiesced operation"))
	image, prefix := authorityFixtureCommit()
	if err := log.CommitQuiesced(f.ctx, op, image); err == nil {
		t.Fatal("committed without a prepare")
	}
	if err := log.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := log.append(f.ctx, walAuthorityRecord{kind: walAuthorityCommitQuiesced, walAuthorityReceipt: walAuthorityReceipt{Sequence: 1, Operation: op, Image: image, Prefix: prefix}}); err == nil {
		t.Fatal("quiesced record accepted an active WAL prefix")
	}
	if err := log.CommitQuiesced(f.ctx, op, image); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	log, err = f.open(nil, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	head, err := log.Head(f.ctx)
	if err != nil || head.Sequence != 1 || head.Image != image || head.Prefix != (walCommitPrefixReceipt{}) {
		t.Fatalf("quiesced receipt was not retained: %+v %v", head, err)
	}
}

func TestProtectedCatalogRequiresTrustedOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	called := false
	value, err := ReadProtectedCatalog(ctx, "/not-created", nil, func(*catalog.Store) (int, error) { called = true; return 123, nil })
	if value != 0 || err == nil || called {
		t.Fatal("missing authorizer executed callback or returned data")
	}
	value, err = MutateProtectedCatalog(ctx, "/not-created", func(context.Context) error { return nil }, func(*catalog.Store) (int, error) { called = true; return 123, nil })
	if value != 0 || err == nil || called {
		t.Fatal("ordinary process or absent profile executed a mutation")
	}
}

func profileCatalogFixture(t *testing.T, worker bool) (context.Context, string) {
	t.Helper()
	if os.Getenv("MCE4D_PROFILE_CATALOG_FIXTURE") != "synthetic-v1" {
		t.Skip("requires separately provisioned root-owned synthetic profile")
	}
	marker, err := os.ReadFile("/fixture.owner")
	root := os.Getenv("MCE4D_PROFILE_TEST_ROOT")
	if err != nil || string(marker) != "mce4d-profile-catalog-fixture-v1\n" || !strings.HasPrefix(root, "/profiles/") || !validProfilePath(root) {
		t.Fatal("unrecognized fixture")
	}
	if worker {
		if err := HardenProtectedProfileWorker(); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	t.Cleanup(cancel)
	return ctx, root
}

func TestProtectedProfileCatalogProvisionFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	if _, err := ProvisionProtectedProfile(ctx, ProtectedProfileOptions{Root: root, EngineUID: 1000, EngineGID: 1000, ClientUID: 1001, TargetRoots: []string{"/target"}}); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedProfileCatalogCheckFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, false)
	if _, err := CheckProtectedProfile(ctx, root); err != nil {
		t.Fatal(err)
	}
}

func fixtureCatalogJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureCatalogRegistration(t *testing.T, root string) catalog.Registration {
	t.Helper()
	raw, err := os.ReadFile("/result/" + filepath.Base(root) + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var registration catalog.Registration
	if err := json.Unmarshal(raw, &registration); err != nil {
		t.Fatal(err)
	}
	return registration
}

func TestProtectedProfileCatalogRoundtripFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	worktree, err := paths.OpenRoot("/target")
	if err != nil {
		t.Fatal(err)
	}
	defer worktree.Close()
	common, err := runtime.OpenLocalGitMetadata(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	policy := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/target"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 1024, MaxSourceBytes: 8192, MaxOutputBytes: 4096, SecretRulesVersion: "v1"}
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	denied := errors.New("synthetic authorization revoked")
	allow := true
	authorize := func(context.Context) error {
		if !allow {
			return denied
		}
		return nil
	}
	registration, err := MutateProtectedCatalog(ctx, root, authorize, func(s *catalog.Store) (catalog.Registration, error) {
		return s.Register(ctx, worktree, common, "", "default", fixtureCatalogJSON(t, policy), fixtureCatalogJSON(t, profile))
	})
	if err != nil || registration.WorktreeID == "" {
		t.Fatalf("real catalog registration: %+v %v", registration, err)
	}
	profile.TestsEnabled = true
	raw := fixtureCatalogJSON(t, model.ProfileAdminRequest{WorktreeID: registration.WorktreeID, ProfileName: "default", ExpectedRevision: 1, Profile: profile})
	updated, err := MutateProtectedCatalog(ctx, root, authorize, func(s *catalog.Store) (model.ProfileAdminData, error) { return s.SetProfile(ctx, raw) })
	if err != nil || updated.Revision != 2 {
		t.Fatalf("profile write receipt: %+v %v", updated, err)
	}
	registration.ProfileID = updated.ProfileID
	policy.MaxFileBytes = 2048
	rawPolicy := fixtureCatalogJSON(t, model.PolicyAdminRequest{WorktreeID: registration.WorktreeID, ExpectedEpoch: 1, Policy: policy})
	changed, err := MutateProtectedCatalog(ctx, root, authorize, func(s *catalog.Store) (model.PolicyAdminData, error) { return s.SetPolicy(ctx, rawPolicy) })
	if err != nil || changed.PolicyEpoch != 2 {
		t.Fatalf("policy write receipt: %+v %v", changed, err)
	}
	stale, err := MutateProtectedCatalog(ctx, root, authorize, func(s *catalog.Store) (model.PolicyAdminData, error) { return s.SetPolicy(ctx, rawPolicy) })
	if !errors.Is(err, catalog.ErrRevisionConflict) || stale != (model.PolicyAdminData{}) {
		t.Fatalf("CAS did not roll back without data: %+v %v", stale, err)
	}
	allow = false
	called := false
	if _, err := MutateProtectedCatalog(ctx, root, authorize, func(*catalog.Store) (int, error) { called = true; return 123, nil }); !errors.Is(err, denied) || called {
		t.Fatal("revoked owner entered SQL")
	}
	allow = true
	data, err := MutateProtectedCatalog(ctx, root, authorize, func(*catalog.Store) (int, error) { return 123, denied })
	if !errors.Is(err, denied) || data != 0 {
		t.Fatal("rollback exposed callback data")
	}
	read := func(s *catalog.Store) (catalog.Binding, error) {
		return s.ReadBinding(ctx, registration.WorktreeID, registration.ProfileID, func(context.Context, string, string) error { return nil })
	}
	binding, err := ReadProtectedCatalog(ctx, root, authorize, read)
	if err != nil || binding.PolicyEpoch != 2 || !binding.Profile.TestsEnabled {
		t.Fatalf("read after pool close/reopen: %+v %v", binding, err)
	}
	value, err := ReadProtectedCatalog(ctx, root, authorize, func(s *catalog.Store) (catalog.Binding, error) {
		result, err := read(s)
		allow = false
		return result, err
	})
	if !errors.Is(err, denied) || errors.Is(err, catalog.ErrCatalogCommitUnknown) || value.WorktreeID != "" {
		t.Fatal("post-read revocation exposed data")
	}
	if err := os.WriteFile("/result/"+filepath.Base(root)+".json", fixtureCatalogJSON(t, registration), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedProfileCatalogHeldWriterFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	registration := fixtureCatalogRegistration(t, root)
	request := fixtureCatalogJSON(t, model.PolicyAdminRequest{WorktreeID: registration.WorktreeID, ExpectedEpoch: 2, Policy: model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/target"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 8192, MaxSourceBytes: 8192, MaxOutputBytes: 4096, SecretRulesVersion: "v1"}})
	data, err := MutateProtectedCatalog(ctx, root, func(context.Context) error { return nil }, func(s *catalog.Store) (model.PolicyAdminData, error) {
		data, err := s.SetPolicy(ctx, request)
		if err != nil {
			return data, err
		}
		fmt.Fprintln(os.Stdout, "SQL_COMMITTED_WAITING_FOR_ACK")
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if raw, err := os.ReadFile("/result/release"); err == nil && string(raw) == "synthetic-release\n" {
				return data, nil
			}
			select {
			case <-ctx.Done():
				return data, ctx.Err()
			case <-ticker.C:
			}
		}
	})
	if err != nil || data.PolicyEpoch != 3 {
		t.Fatalf("held writer result: %+v %v", data, err)
	}
}

func TestProtectedProfileCatalogConcurrentReadFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	registration := fixtureCatalogRegistration(t, root)
	data, err := ReadProtectedCatalog(ctx, root, func(context.Context) error { return nil }, func(s *catalog.Store) (catalog.Binding, error) {
		return s.ReadBinding(ctx, registration.WorktreeID, registration.ProfileID, func(context.Context, string, string) error { return nil })
	})
	if err != nil || data.PolicyEpoch != 3 {
		t.Fatalf("reader observed unacknowledged policy: epoch=%d err=%v", data.PolicyEpoch, err)
	}
}

func TestProtectedProfileCatalogUnknownFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	registration := fixtureCatalogRegistration(t, root)
	request := fixtureCatalogJSON(t, model.PolicyAdminRequest{WorktreeID: registration.WorktreeID, ExpectedEpoch: 2, Policy: model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/target"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 4096, MaxSourceBytes: 8192, MaxOutputBytes: 4096, SecretRulesVersion: "v1"}})
	value, err := MutateProtectedCatalog(ctx, root, func(context.Context) error { return nil }, func(s *catalog.Store) (model.PolicyAdminData, error) {
		data, err := s.SetPolicy(ctx, request)
		if err != nil {
			return data, err
		}
		if os.Getenv("MCE4D_PROFILE_CRASH") == "after-sql" {
			fmt.Fprintln(os.Stdout, "SQL_COMMITTED_PENDING")
			<-ctx.Done() // The isolated broker SIGKILLs this test process.
		}
		return data, errors.New("synthetic result lost after SQL commit")
	})
	if !errors.Is(err, catalog.ErrCatalogCommitUnknown) || value != (model.PolicyAdminData{}) {
		t.Fatalf("unknown outcome was acknowledged: %+v %v", value, err)
	}
	called := false
	if _, err := ReadProtectedCatalog(ctx, root, func(context.Context) error { return nil }, func(*catalog.Store) (int, error) { called = true; return 123, nil }); !errors.Is(err, errWALAuthorityPending) || called {
		t.Fatal("pending transaction exposed data or entered callback")
	}
}
