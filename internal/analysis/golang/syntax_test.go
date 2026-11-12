package golang

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func syntaxFixture(t *testing.T, filename, content string) SyntaxInput {
	t.Helper()
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, TestsEnabled: true, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	profileID, err := model.ProfileDigest(profile, 8192)
	if err != nil {
		t.Fatal(err)
	}
	file, err := snapshot.NewFile(filename, []byte(content), 65536)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: profileID, PolicyDigest: strings.Repeat("c", 64), ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{{Path: filename, Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 65536)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	return SyntaxInput{Snapshot: captured, Profile: profile, Path: filename, PackageKey: "fixture-package", ImportPath: "example.invalid/fixture", ModuleKey: "fixture-module", PackageName: "sample", Variant: "production", Scanner: scanner, MaxManifestBytes: 8192, MaxFileBytes: 65536, MaxNodes: 10000, MaxDepth: 256, MaxFacts: 1000, MaxOutputBytes: 4 * 1024 * 1024}
}

func TestSyntaxDeclarationsScopesAndCapturedRanges(t *testing.T) {
	const source = "package sample\nimport f \"fmt\"\ntype Box[T any] struct { Field string `json:\"field\"`; *Other }\nfunc (b *Box[T]) Use(p int) (result int) {\n x := p\n x, y := x, 1\n { x := y; _ = x }\n for k, v := range []int{x, y} { _ = k; _ = v }\n f := func(p int) int { return p }\n g := func(p int) int { return p }\n switch x := any(x).(type) { case int: _ = x; case string: _ = x }\n _ = f; _ = g; return x\n}\n"
	input := syntaxFixture(t, "pkg/main.go", source)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Package.SemanticStatus != "partial" || result.BuildSelectionVerified || len(result.Diagnostics) != 0 {
		t.Fatalf("syntax overclaimed coverage: %+v", result)
	}
	if len(result.Imports) != 1 || result.Imports[0].ImportPath != "fmt" || result.Imports[0].Alias != "f" || len(result.Tags) != 1 || result.Tags[0].Literal != "`json:\"field\"`" {
		t.Fatalf("syntax metadata mismatch: %+v %+v", result.Imports, result.Tags)
	}
	counts := map[string]int{}
	identities := map[string]bool{}
	scopes := map[string]bool{}
	xScopes := map[string]bool{}
	for _, scope := range result.Scopes {
		if scope.ID == "" || scopes[scope.ID] {
			t.Fatalf("duplicate or empty scope: %+v", scope)
		}
		if scope.ParentID != "" && !scopes[scope.ParentID] {
			t.Fatalf("missing lexical parent: %+v", scope)
		}
		scopes[scope.ID] = true
	}
	for _, symbol := range result.Symbols {
		if identities[symbol.SymbolID] || !scopes[symbol.ScopeKey] || symbol.Path == nil || *symbol.Path != input.Path || symbol.Range == nil {
			t.Fatalf("bad symbol identity: %+v", symbol)
		}
		identities[symbol.SymbolID] = true
		counts[symbol.Kind+":"+symbol.Name]++
		if symbol.Kind == "field" && symbol.Name == "Field" && symbol.Signature != "string `json:\"field\"`" {
			t.Fatalf("field signature lost its tag: %q", symbol.Signature)
		}
		start, end := int(symbol.Range.StartByte), int(symbol.Range.EndByte)
		if start < 0 || end > len(source) || start > end {
			t.Fatalf("bad source range: %+v", symbol)
		}
		if symbol.Kind != "file" && symbol.Kind != "closure" && source[start:end] != symbol.Name {
			t.Fatalf("range does not identify declaration: %+v %q", symbol, source[start:end])
		}
		if symbol.Kind == "variable" && symbol.Name == "x" {
			xScopes[symbol.ScopeKey] = true
		}
	}
	for name, want := range map[string]int{"type:Box": 1, "type:T": 2, "field:Field": 1, "field:Other": 1, "method:Use": 1, "receiver:b": 1, "parameter:p": 3, "result:result": 1, "variable:x": 4, "variable:y": 1, "variable:k": 1, "variable:v": 1, "closure:": 2} {
		if counts[name] != want {
			t.Errorf("%s count=%d want=%d", name, counts[name], want)
		}
	}
	if len(xScopes) != 4 {
		t.Fatalf("shadowing/type-switch scopes collapsed: %d", len(xScopes))
	}
	if !identities[result.Tags[0].SymbolID] {
		t.Fatal("struct tag lost field identity")
	}
	again, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("syntax output is not deterministic: %v", err)
	}
}

func TestSyntaxSeparatesVariantsAndRejectsWrongProfile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	input := syntaxFixture(t, "pkg/main.go", "package sample\nfunc Same() {}\n")
	production, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Variant = "internal_test"
	internal, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	find := func(result SyntaxResult) (string, string) {
		for _, symbol := range result.Symbols {
			if symbol.Kind == "function" && symbol.Name == "Same" {
				for _, occurrence := range result.Occurrences {
					if occurrence.SymbolID == symbol.SymbolID {
						return symbol.SymbolID, occurrence.OccurrenceID
					}
				}
			}
		}
		return "", ""
	}
	productionID, productionOccurrence := find(production)
	internalID, internalOccurrence := find(internal)
	if production.ContentHash != internal.ContentHash || productionID == "" || internalID == "" || productionID == internalID || productionOccurrence == "" || productionOccurrence != internalOccurrence {
		t.Fatal("package variants did not retain distinct symbol IDs and common occurrence provenance")
	}
	input.Profile.Goos = "windows"
	if _, err := AnalyzeSyntaxFile(ctx, input); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("profile mismatch accepted: %v", err)
	}
	testInput := syntaxFixture(t, "pkg/api_test.go", "package sample_test\nfunc TestAPI() {}\n")
	testInput.Variant = "external_test"
	if _, err := AnalyzeSyntaxFile(ctx, testInput); err != nil {
		t.Fatalf("external test rejected: %v", err)
	}
	testInput.Variant = "internal_test"
	if _, err := AnalyzeSyntaxFile(ctx, testInput); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("external package accepted as internal: %v", err)
	}
	testInput.Variant = "production"
	if _, err := AnalyzeSyntaxFile(ctx, testInput); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("test file accepted as production: %v", err)
	}
	internalTest := syntaxFixture(t, "pkg/internal_test.go", "package sample\nfunc TestInternal() {}\n")
	internalTest.Variant = "internal_test"
	if _, err := AnalyzeSyntaxFile(ctx, internalTest); err != nil {
		t.Fatalf("internal test rejected: %v", err)
	}
	internalTest.Profile.TestsEnabled = false
	manifest := internalTest.Snapshot.Manifest()
	profileID, err := model.ProfileDigest(internalTest.Profile, 8192)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ProfileID, manifest.SnapshotDigest = profileID, ""
	file, _ := internalTest.Snapshot.File(internalTest.Path)
	internalTest.Snapshot, err = snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AnalyzeSyntaxFile(ctx, internalTest); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("test variant accepted in tests-disabled profile: %v", err)
	}
}

func TestSyntaxParseFailureSecretsAndBudgets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	invalid := syntaxFixture(t, "bad.go", "package sample\nfunc broken(\n")
	result, err := AnalyzeSyntaxFile(ctx, invalid)
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "GO_PARSE_ERROR" || len(result.Symbols) != 0 || strings.Contains(result.Diagnostics[0].Message, "broken") {
		t.Fatalf("unsafe parse outcome: %+v %v", result, err)
	}
	wrongPackage := syntaxFixture(t, "bad.go", "package other\nfunc broken(\n")
	if _, err := AnalyzeSyntaxFile(ctx, wrongPackage); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("parse error hid package mismatch: %v", err)
	}
	secret := syntaxFixture(t, "secret.go", "package sample\nvar value = \"api_key = abcdefghijklmnopqrstuvwxyz\"\n")
	if result, err := AnalyzeSyntaxFile(ctx, secret); !errors.Is(err, policy.ErrSecretBlocked) || len(result.Symbols) != 0 {
		t.Fatalf("secret source exposed: %+v %v", result, err)
	}
	input := syntaxFixture(t, "a.go", "package sample\nfunc Main() { x := 1; _ = x }\n")
	input.MaxNodes = 2
	if result, err := AnalyzeSyntaxFile(ctx, input); !errors.Is(err, ErrSyntaxLimit) || len(result.Symbols) != 0 {
		t.Fatalf("node budget ignored: %+v %v", result, err)
	}
	input.MaxNodes, input.MaxFacts = 10000, 2
	if result, err := AnalyzeSyntaxFile(ctx, input); !errors.Is(err, ErrSyntaxLimit) || len(result.Symbols) != 0 {
		t.Fatalf("fact budget ignored: %+v %v", result, err)
	}
	input.MaxFacts, input.MaxOutputBytes = 1000, 1024
	if result, err := AnalyzeSyntaxFile(ctx, input); !errors.Is(err, ErrSyntaxLimit) || len(result.Symbols) != 0 {
		t.Fatalf("output budget ignored: %+v %v", result, err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if result, err := AnalyzeSyntaxFile(cancelled, input); !errors.Is(err, context.Canceled) || len(result.Symbols) != 0 {
		t.Fatalf("cancellation ignored: %+v %v", result, err)
	}
}

func TestSyntaxAnonymousStructsRangeScopeAndUTF8(t *testing.T) {
	const source = "package sample\r\ntype Outer struct { Left struct { X int }; Right struct { X int } }\r\nfunc 名(參 int) { for k := range func() []int { return []int{參} }() { _ = k }; 變 := 參; _ = 變 }\r\n"
	input := syntaxFixture(t, "other/file.go", source)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := AnalyzeSyntaxFile(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	fieldScopes := map[string]bool{}
	var rangeScope, closureOwner string
	for _, scope := range result.Scopes {
		if scope.Kind == "range" {
			rangeScope = scope.ID
		}
	}
	for _, symbol := range result.Symbols {
		if symbol.Kind == "field" && symbol.Name == "X" {
			fieldScopes[symbol.ScopeKey] = true
		}
		if symbol.Kind == "closure" {
			closureOwner = symbol.ScopeKey
		}
		if symbol.Name == "名" || symbol.Name == "參" || symbol.Name == "變" {
			if symbol.Range == nil || source[symbol.Range.StartByte:symbol.Range.EndByte] != symbol.Name {
				t.Fatalf("UTF-8 byte range wrong: %+v", symbol)
			}
		}
	}
	if len(fieldScopes) != 2 {
		t.Fatalf("anonymous fields share a scope: %v", fieldScopes)
	}
	if rangeScope == "" || closureOwner == "" || rangeScope == closureOwner {
		t.Fatalf("range expression incorrectly captured loop scope: range=%s closure=%s", rangeScope, closureOwner)
	}
}

func TestSyntaxIdentitiesDistinguishPackagesWithSameName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first := syntaxFixture(t, "one/a.go", "package sample\nfunc Same() {}\n")
	second := syntaxFixture(t, "two/a.go", "package sample\nfunc Same() {}\n")
	second.PackageKey, second.ImportPath = "other-package", "example.invalid/other"
	a, err := AnalyzeSyntaxFile(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := AnalyzeSyntaxFile(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	var aID, bID string
	for _, symbol := range a.Symbols {
		if symbol.Kind == "function" && symbol.Name == "Same" {
			aID = symbol.SymbolID
		}
	}
	for _, symbol := range b.Symbols {
		if symbol.Kind == "function" && symbol.Name == "Same" {
			bID = symbol.SymbolID
		}
	}
	if aID == "" || bID == "" || aID == bID {
		t.Fatalf("same name collapsed: %q %q", aID, bID)
	}
}
