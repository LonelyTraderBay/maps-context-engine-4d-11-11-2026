package golang

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func fallbackRequest(input LoaderPreflightInput) CapturedSyntaxFallback {
	return CapturedSyntaxFallback{Loader: CapturedLoaderInput{Preflight: input, MaxOutputBytes: 65536}, MaxOutputBytes: 65536,
		Files: []SyntaxInput{{Path: "p.go", PackageKey: "fixture-package", ImportPath: "example.test/fixture", ModuleKey: "fixture-module", PackageName: "fixture", Variant: "production", MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOutputBytes: 65536}}}
}

func TestLoaderSyntaxFallbackForMissingDependencyAndToolchain(t *testing.T) {
	ctx := preflightContext(t)
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\nrequire example.test/missing v1.0.0\n", "p.go": "package fixture\nimport \"example.test/missing\"\nfunc Present() {}\n"})
	request := fallbackRequest(input)
	result, err := LoadOrAnalyzeCaptured(ctx, request)
	if err != nil || result.Loader.Status != "partial" || !containsReason(result.Loader.Reasons, "GO_DEPENDENCIES_UNVERIFIED") || !containsReason(result.Loader.Reasons, "TOOLCHAIN_UNVERIFIED") || result.Loader.BuildSelectionVerified || len(result.Syntax) != 1 || result.Syntax[0].BuildSelectionVerified || result.Syntax[0].Package.SemanticStatus != "partial" {
		t.Fatalf("missing dependency did not preserve syntax fallback: %+v %v", result, err)
	}
	found := false
	for _, symbol := range result.Syntax[0].Symbols {
		found = found || symbol.Name == "Present"
	}
	if !found || result.Syntax[0].SnapshotDigest != input.Snapshot.Manifest().SnapshotDigest {
		t.Fatal("fallback omitted captured syntax or changed snapshot identity")
	}
	withoutTool := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": "package fixture\nfunc Present() {}\n"})
	withoutTool.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	withoutTool.VerifyToolchain = func(context.Context, *model.GoProfile) (bool, error) { return true, nil }
	result, err = LoadOrAnalyzeCaptured(ctx, fallbackRequest(withoutTool))
	if err != nil || !containsReason(result.Loader.Reasons, "TOOLCHAIN_UNVERIFIED") || len(result.Syntax) != 1 || result.Syntax[0].BuildSelectionVerified {
		t.Fatalf("unavailable executable did not degrade: %+v %v", result, err)
	}
}

func TestLoaderSyntaxFallbackRejectsRevocationDuplicatesAndBudgets(t *testing.T) {
	ctx := preflightContext(t)
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": "package fixture\nfunc Present() {}\n"})
	checks := 0
	input.AuthorizePath = func(context.Context, string, string, string, int64) error {
		checks++
		if checks == 7 {
			return errors.New("private authorization detail")
		}
		return nil
	}
	result, err := LoadOrAnalyzeCaptured(ctx, fallbackRequest(input))
	if !errors.Is(err, ErrLoaderAuthority) || len(result.Syntax) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("final revocation leaked facts: %+v %v", result, err)
	}
	input.AuthorizePath = func(context.Context, string, string, string, int64) error { return nil }
	request := fallbackRequest(input)
	request.Files = append(request.Files, request.Files[0])
	if _, err := LoadOrAnalyzeCaptured(ctx, request); !errors.Is(err, ErrLoaderInput) {
		t.Fatalf("duplicate fallback file accepted: %v", err)
	}
	request = fallbackRequest(input)
	request.Files[0].Path = "../private.go"
	if _, err := LoadOrAnalyzeCaptured(ctx, request); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("uncaptured path accepted: %v", err)
	}
	request = fallbackRequest(input)
	request.MaxOutputBytes = 1024
	if result, err := LoadOrAnalyzeCaptured(ctx, request); !errors.Is(err, ErrSyntaxLimit) || len(result.Syntax) != 0 {
		t.Fatalf("fallback exceeded budget: %+v %v", result, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := LoadOrAnalyzeCaptured(canceled, fallbackRequest(input)); !errors.Is(err, context.Canceled) {
		t.Fatalf("fallback ignored cancellation: %v", err)
	}
}
