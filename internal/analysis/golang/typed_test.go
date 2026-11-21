package golang

import (
	"context"
	"errors"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestTypedCapturedIdentifierRejectsSourceDisagreement(t *testing.T) {
	const source = "package fixture\nvar Tên = 1\n"
	overlay, filename := overlayFixture(t, source)
	fset := token.NewFileSet()
	file := fset.AddFile(filename, -1, len(source))
	collector := typedCollector{pkg: &packages.Package{Fset: fset}, overlay: overlay, selected: map[string]bool{"p.go": true},
		input: TypedInput{Loader: CapturedLoaderInput{Preflight: LoaderPreflightInput{Snapshot: overlay.snapshot}}}}
	start := strings.Index(source, "Tên")
	valid := &ast.Ident{NamePos: file.Pos(start), Name: "Tên"}
	if site, err := collector.identSite(valid); err != nil || site.path != "p.go" || site.start != int64(start) {
		t.Fatalf("captured identifier rejected: %+v %v", site, err)
	}
	wrong := &ast.Ident{NamePos: file.Pos(start), Name: "Têx"}
	if _, err := collector.identSite(wrong); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("changed source identifier accepted: %v", err)
	}
	split := &ast.Ident{NamePos: file.Pos(start + 2), Name: "x"}
	if _, err := collector.identSite(split); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("middle of UTF-8 identifier accepted: %v", err)
	}
}

func TestTypedPartialAndCancellationReturnNoFacts(t *testing.T) {
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": "package fixture\nvar X int\n"})
	request := TypedInput{Loader: CapturedLoaderInput{Preflight: input, MaxOutputBytes: 4096}, PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 1000, MaxDepth: 100, MaxFacts: 500, MaxOccurrences: 500, MaxOutputBytes: 65536}
	result, err := AnalyzeTypedCaptured(preflightContext(t), request)
	if err != nil || result.Status != "partial" || result.Loader.BuildSelectionVerified || len(result.Syntax) != 0 || len(result.Occurrences) != 0 || !containsReason(result.Reasons, "TOOLCHAIN_UNVERIFIED") {
		t.Fatalf("unverified loader returned typed facts: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(preflightContext(t))
	cancel()
	if result, err := AnalyzeTypedCaptured(ctx, request); !errors.Is(err, context.Canceled) || len(result.Occurrences) != 0 {
		t.Fatalf("canceled typed load returned facts: %+v %v", result, err)
	}
}

func TestTypedScopeLookupBoundsAndNesting(t *testing.T) {
	var scopes typedFileScopes
	scopes.add(Scope{ID: "package", Kind: "package"}, 0)
	scopes.add(Scope{ID: "function", Kind: "function", StartByte: 10, EndByte: 50}, 1)
	scopes.add(Scope{ID: "nested", Kind: "block", StartByte: 20, EndByte: 30}, 2)
	scopes.index()
	collector := typedCollector{ctx: context.Background(), fileScopes: map[string]typedFileScopes{"p.go": scopes}}
	if scope, err := collector.scopeFor(typedSite{path: "p.go", start: 22, end: 23}); err != nil || scope != "nested" {
		t.Fatalf("nested scope: %q %v", scope, err)
	}
	if scope, err := collector.scopeFor(typedSite{path: "p.go", start: 12, end: 13}); err != nil || scope != "function" {
		t.Fatalf("function scope: %q %v", scope, err)
	}
	if _, err := collector.scopeFor(typedSite{path: "missing.go", start: 1, end: 2}); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("missing scope accepted: %v", err)
	}
	collector.scopeChecks = 2000000
	if _, err := collector.scopeFor(typedSite{path: "p.go", start: 22, end: 23}); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("scope lookup budget ignored: %v", err)
	}
	collector.scopeChecks = 0
	scopes.add(Scope{ID: "overlap", Kind: "block", StartByte: 19, EndByte: 30}, 2)
	scopes.index()
	collector.fileScopes["p.go"] = scopes
	if _, err := collector.scopeFor(typedSite{path: "p.go", start: 22, end: 23}); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("ambiguous scope accepted: %v", err)
	}
}

func TestTypedScopeLookupManySiblings(t *testing.T) {
	const count = 1025
	var scopes typedFileScopes
	scopes.add(Scope{ID: "package", Kind: "package"}, 0)
	for i := count - 1; i >= 0; i-- {
		start := int64(i * 20)
		id := strconv.Itoa(i)
		scopes.add(Scope{ID: "func" + id, StartByte: start, EndByte: start + 20}, 1)
		scopes.add(Scope{ID: "body" + id, StartByte: start + 10, EndByte: start + 20}, 2)
	}
	scopes.index()
	collector := typedCollector{ctx: context.Background(), fileScopes: map[string]typedFileScopes{"p.go": scopes}}
	for i := 0; i < count; i++ {
		start := int64(i*20 + 11)
		got, err := collector.scopeFor(typedSite{path: "p.go", start: start, end: start + 1})
		if want := "body" + strconv.Itoa(i); err != nil || got != want {
			t.Fatalf("scope %d: wanted %q, got %q: %v", i, want, got, err)
		}
	}
	if collector.scopeChecks != 2*count {
		t.Fatalf("lookups inspected unrelated scopes: %d", collector.scopeChecks)
	}
}
