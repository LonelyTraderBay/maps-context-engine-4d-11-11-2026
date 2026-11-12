package golang

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func loaderPreflightFixture(t *testing.T, contents map[string]string) LoaderPreflightInput {
	t.Helper()
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "typed_when_available", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 32768)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(contents))
	for name := range contents {
		names = append(names, name)
	}
	sort.Strings(names)
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: profileID, PolicyDigest: strings.Repeat("c", 64), ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	var files []snapshot.File
	fileMap := inventory.FileMap{Entries: []inventory.FileMapEntry{}}
	for _, name := range names {
		file, err := snapshot.NewFile(name, []byte(contents[name]), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &length})
		files = append(files, file)
		fileMap.Entries = append(fileMap.Entries, inventory.FileMapEntry{Path: name, Origin: "untracked", State: "candidate", Stages: []uint8{}})
	}
	captured, err := snapshot.Freeze(manifest, files, 32768, 65536)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	return LoaderPreflightInput{Snapshot: captured, Profile: profile, Inventory: fileMap, PolicyEpoch: 1, Scanner: scanner, AuthorizeScope: authorize, AuthorizePath: authorize, MaxManifestBytes: 32768, MaxFileBytes: 65536, MaxSourceBytes: 65536}
}

func preflightContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestLoaderPreflightRequiresExplicitIndependentProofs(t *testing.T) {
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "p.go": "package fixture\nvar X = 1\n"})
	ctx := preflightContext(t)
	result, err := PreflightLoader(ctx, input)
	if err != nil || result.Status != "partial" || !reflect.DeepEqual(result.Reasons, []string{"CLOSURE_UNVERIFIED", "TOOLCHAIN_UNVERIFIED"}) {
		t.Fatalf("missing proof accepted: %+v %v", result, err)
	}
	input.VerifyClosure = func(_ context.Context, captured *snapshot.Snapshot, observed inventory.FileMap) (bool, error) {
		if captured.Manifest().SnapshotDigest != input.Snapshot.Manifest().SnapshotDigest || len(observed.Entries) != 2 {
			t.Fatal("wrong closure input")
		}
		observed.Entries[0].Path = "changed"
		return true, nil
	}
	input.VerifyToolchain = func(_ context.Context, profile *model.GoProfile) (bool, error) {
		if profile.ToolchainDigest != input.Profile.ToolchainDigest {
			t.Fatal("wrong toolchain scope")
		}
		profile.ModuleRoots[0] = "changed"
		return true, nil
	}
	result, err = PreflightLoader(ctx, input)
	if err != nil || result.Status != "eligible_for_guarded_load" || len(result.Reasons) != 0 || !reflect.DeepEqual(result.Modules, []LoaderModule{{Root: ".", ImportPath: "example.test/fixture"}}) {
		t.Fatalf("valid preflight: %+v %v", result, err)
	}
	if input.Profile.ModuleRoots[0] != "." || input.Inventory.Entries[0].Path != "go.mod" {
		t.Fatal("callback mutated caller inputs")
	}
}

func TestLoaderPreflightFailsClosedOnUnverifiedDependenciesAndInputs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contents map[string]string
		modify   func(*LoaderPreflightInput)
		reason   string
	}{
		{"replace", map[string]string{"go.mod": "module example.test/x\nreplace example.test/dep => ../outside\n", "x.go": "package x\n"}, nil, "GO_REPLACE_UNVERIFIED"},
		{"require", map[string]string{"go.mod": "module example.test/x\nrequire example.test/dep v1.0.0\n", "x.go": "package x\n"}, nil, "GO_DEPENDENCIES_UNVERIFIED"},
		{"missing-module", map[string]string{"x.go": "package x\n"}, nil, "GO_MOD_MISSING"},
		{"invalid-module", map[string]string{"go.mod": "not a go module\n", "x.go": "package x\n"}, nil, "GO_MOD_INVALID"},
		{"cgo", map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"}, func(in *LoaderPreflightInput) { in.Profile.CgoEnabled = true; resetLoaderProfile(t, in) }, "CGO_UNVERIFIED"},
		{"inventory-gap", map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"}, func(in *LoaderPreflightInput) { in.Inventory.Entries[1].State = "sparse" }, "INVENTORY_INCOMPLETE"},
		{"closure", map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"}, func(in *LoaderPreflightInput) {
			in.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return false, nil }
		}, "CLOSURE_UNVERIFIED"},
		{"toolchain", map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"}, func(in *LoaderPreflightInput) {
			in.VerifyToolchain = func(context.Context, *model.GoProfile) (bool, error) { return false, nil }
		}, "TOOLCHAIN_UNVERIFIED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := loaderPreflightFixture(t, tc.contents)
			input.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
			input.VerifyToolchain = func(context.Context, *model.GoProfile) (bool, error) { return true, nil }
			if tc.modify != nil {
				tc.modify(&input)
			}
			result, err := PreflightLoader(preflightContext(t), input)
			if err != nil || result.Status != "partial" || !containsReason(result.Reasons, tc.reason) {
				t.Fatalf("unverified input accepted: %+v %v", result, err)
			}
		})
	}
}

func resetLoaderProfile(t *testing.T, in *LoaderPreflightInput) {
	t.Helper()
	manifest := in.Snapshot.Manifest()
	digest, err := model.ProfileDigest(in.Profile, in.MaxManifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ProfileID, manifest.SnapshotDigest = digest, ""
	var files []snapshot.File
	for _, entry := range manifest.Files {
		file, _ := in.Snapshot.File(entry.Path)
		files = append(files, file)
	}
	in.Snapshot, err = snapshot.Freeze(manifest, files, in.MaxManifestBytes, in.MaxSourceBytes)
	if err != nil {
		t.Fatal(err)
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func addLoaderFixtureDependency(t *testing.T, in *LoaderPreflightInput, digest string) {
	t.Helper()
	manifest := in.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	manifest.DependencyInputDigests = append(manifest.DependencyInputDigests, digest)
	files := make([]snapshot.File, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		file, ok := in.Snapshot.File(item.Path)
		if !ok {
			t.Fatal("fixture file missing")
		}
		files = append(files, file)
	}
	var err error
	in.Snapshot, err = snapshot.Freeze(manifest, files, in.MaxManifestBytes, in.MaxSourceBytes)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoaderDependencyProofIsRequiredAndReceivesIndependentInputs(t *testing.T) {
	ctx := preflightContext(t)
	in := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	digest := strings.Repeat("d", 64)
	addLoaderFixtureDependency(t, &in, digest)
	in.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	in.VerifyToolchain = func(_ context.Context, profile *model.GoProfile) (bool, error) {
		profile.ModuleRoots[0] = "mutated"
		return true, nil
	}
	result, err := PreflightLoader(ctx, in)
	if err != nil || !containsReason(result.Reasons, "DEPENDENCY_ROOTS_UNVERIFIED") {
		t.Fatalf("missing dependency proof: %+v %v", result, err)
	}
	in.VerifyDependencies = func(_ context.Context, profile *model.GoProfile, digests []string) (bool, error) {
		if profile.ModuleRoots[0] != "." || len(digests) != 1 || digests[0] != digest {
			t.Fatal("proof received mutated input")
		}
		profile.ModuleRoots[0], digests[0] = "mutated", "mutated"
		return true, nil
	}
	result, err = PreflightLoader(ctx, in)
	if err != nil || result.Status != "eligible_for_guarded_load" || in.Profile.ModuleRoots[0] != "." || in.Snapshot.Manifest().DependencyInputDigests[0] != digest {
		t.Fatalf("dependency proof did not preserve inputs: %+v %v", result, err)
	}
	in.VerifyDependencies = func(context.Context, *model.GoProfile, []string) (bool, error) { return false, nil }
	result, err = PreflightLoader(ctx, in)
	if err != nil || !containsReason(result.Reasons, "DEPENDENCY_ROOTS_UNVERIFIED") {
		t.Fatalf("false dependency proof: %+v %v", result, err)
	}
	in.VerifyDependencies = func(context.Context, *model.GoProfile, []string) (bool, error) { return false, errors.New("private") }
	if _, err := PreflightLoader(ctx, in); !errors.Is(err, ErrLoaderAuthority) || strings.Contains(err.Error(), "private") {
		t.Fatalf("dependency error leaked: %v", err)
	}
}

func TestLoaderPreflightFreezesCallerProfileAndInventory(t *testing.T) {
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": "package fixture\nvar X int\n"})
	input.AuthorizeScope = func(context.Context, string, string, string, int64) error {
		input.Profile.Goos = "windows"
		input.Inventory.Entries[0].Path = "changed"
		return nil
	}
	input.VerifyClosure = func(_ context.Context, _ *snapshot.Snapshot, observed inventory.FileMap) (bool, error) {
		return len(observed.Entries) == 2 && observed.Entries[0].Path == "go.mod", nil
	}
	input.VerifyToolchain = func(_ context.Context, profile *model.GoProfile) (bool, error) { return profile.Goos == "linux", nil }
	result, err := PreflightLoader(preflightContext(t), input)
	if err != nil || result.Status != "eligible_for_guarded_load" || input.Profile.Goos != "windows" || input.Inventory.Entries[0].Path != "changed" {
		t.Fatalf("caller mutation changed the observed profile: %+v %v", result, err)
	}
}

func TestLoaderPreflightBoundsCopiesBeforeCallbacks(t *testing.T) {
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": "package fixture\nvar X int\n"})
	input.Profile.DependencyRoots = []string{strings.Repeat("x", 16385)}
	if _, err := PreflightLoader(preflightContext(t), input); !errors.Is(err, ErrLoaderInput) {
		t.Fatalf("oversized profile copy accepted: %v", err)
	}
	input = loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": "package fixture\nvar X int\n"})
	input.Inventory.Entries[0].Stages = []uint8{0, 1, 2, 3, 4}
	if _, err := PreflightLoader(preflightContext(t), input); !errors.Is(err, ErrLoaderInput) {
		t.Fatalf("oversized index stage copy accepted: %v", err)
	}
}

func TestLoaderPreflightRechecksAuthorizationAndCaptureIdentity(t *testing.T) {
	ctx := preflightContext(t)
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	calls := 0
	input.AuthorizePath = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 3 {
			return errors.New("sensitive internal detail")
		}
		return nil
	}
	result, err := PreflightLoader(ctx, input)
	if !errors.Is(err, ErrLoaderAuthority) || result.ProfileID != "" || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("revocation leaked details: %+v %v", result, err)
	}
	input = loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	input.Profile.Goos = "windows"
	if _, err := PreflightLoader(ctx, input); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("changed profile accepted: %v", err)
	}
	input = loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	input.MaxSourceBytes = 1
	if _, err := PreflightLoader(ctx, input); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("source budget ignored: %v", err)
	}
	input = loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	input.MaxFileBytes = 1
	if _, err := PreflightLoader(ctx, input); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("per-file budget ignored: %v", err)
	}
	if _, err := PreflightLoader(context.Background(), input); !errors.Is(err, ErrLoaderInput) {
		t.Fatalf("missing deadline accepted: %v", err)
	}
}

func TestLoaderPreflightPreservesCancellation(t *testing.T) {
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	ctx, cancel := context.WithCancel(preflightContext(t))
	input.AuthorizePath = func(ctx context.Context, _, _, _ string, _ int64) error { cancel(); return ctx.Err() }
	result, err := PreflightLoader(ctx, input)
	if !errors.Is(err, context.Canceled) || result.ProfileID != "" {
		t.Fatalf("cancellation hidden: %+v %v", result, err)
	}
}
