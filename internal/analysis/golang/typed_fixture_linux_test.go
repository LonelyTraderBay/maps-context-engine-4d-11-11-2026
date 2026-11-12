//go:build linux

package golang

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func runTypedBindingsFixture(t *testing.T, ctx context.Context, parent, goRoot string, tool *runtimeadapter.GoExecutable) {
	t.Helper()
	dir := filepath.Join(parent, "typed-bindings")
	for _, name := range []string{"input", "cache", "modcache", "gopath", "home", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	const sourceLF = `package fixture

type Holder[T any] struct { Field T }
func (h *Holder[T]) Method(p T) (result T) {
	local := p
	h.Field = local
	local, next := local, 1
	_ = next
	n := 1
	n += 1
	_ = n
	ptr := &local
	*ptr = p
	inner := func(p T) T {
		local := p
		return local
	}
	_ = inner
	method := h.Method
	_ = method
	expr := (*Holder[T]).Method
	_ = expr
	switch v := any(local).(type) {
	case int:
		_ = v
	default:
		_ = v
	}
	result = h.Field
	return result
}
func Điểm(giá int) int {
	cục := giá
	return cục
}
`
	source := strings.ReplaceAll(sourceLF, "\n", "\r\n")
	const otherSource = "package fixture\nfunc Other() {\n other := Holder[int]{}\n _ = other.Field\n other.Method(1)\n}\n"
	contents := map[string]string{"go.mod": "module example.test/fixture\n\ngo 1.27.4\n", "typed.go": source, "other.go": otherSource}
	for name, value := range contents {
		if err := os.WriteFile(filepath.Join(dir, "input", name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	preflight := loaderPreflightFixture(t, contents)
	preflight.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	preflight.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(context.Context, *model.GoProfile) (bool, error) { return true, nil })
	scratchRoot, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer scratchRoot.Close()
	verify := LoaderScratchVerifier(scratchRoot, func(context.Context) error { return nil })
	request := TypedInput{Loader: CapturedLoaderInput{Preflight: preflight, GoRoot: goRoot, ScratchRoot: dir, WorkspaceRoot: filepath.Join(dir, "input"), Tool: tool, MaxOutputBytes: 65536, VerifyScratch: verify},
		PackageKey: "fixture-package", ModuleKey: "fixture-module", MaxNodes: 20000, MaxDepth: 256, MaxFacts: 4000, MaxOccurrences: 4000, MaxOutputBytes: 1024 * 1024}
	result, err := AnalyzeTypedCaptured(ctx, request)
	if err != nil || result.Status != "partial" || !result.Loader.BuildSelectionVerified || len(result.Syntax) != 2 || containsReason(result.Reasons, "LOCAL_BINDING_UNMAPPED") {
		t.Fatalf("typed fixture: %+v %v", result, err)
	}
	var typedSyntax SyntaxResult
	for _, syntax := range result.Syntax {
		if syntax.Path == "typed.go" {
			typedSyntax = syntax
		}
	}
	if typedSyntax.SnapshotDigest != preflight.Snapshot.Manifest().SnapshotDigest || !typedSyntax.BuildSelectionVerified {
		t.Fatal("typed facts lost capture or build selection")
	}
	symbolAt := func(kind, name string, offset int) string {
		t.Helper()
		for _, symbol := range typedSyntax.Symbols {
			if symbol.Kind == kind && symbol.Name == name && symbol.Range != nil && symbol.Range.StartByte == int64(offset) {
				return symbol.SymbolID
			}
		}
		t.Fatalf("missing symbol %s %s at %d", kind, name, offset)
		return ""
	}
	useAt := func(name string, offset int, wantID, selection string) {
		t.Helper()
		for _, occurrence := range result.Occurrences {
			if occurrence.Path == "typed.go" && occurrence.Kind == "use" && occurrence.Name == name && occurrence.Range.StartByte == int64(offset) {
				if occurrence.Resolution != "captured" || occurrence.SymbolID == nil || *occurrence.SymbolID != wantID || occurrence.Selection != selection || occurrence.ScopeID == "" {
					t.Fatalf("incorrect binding at %d: %+v", offset, occurrence)
				}
				return
			}
		}
		t.Fatalf("missing use %s at %d", name, offset)
	}
	index := func(fragment string) int {
		t.Helper()
		value := strings.Index(source, fragment)
		if value < 0 {
			t.Fatalf("fixture marker missing %q", fragment)
		}
		return value
	}
	fieldID := symbolAt("field", "Field", index("Field T"))
	receiverID := symbolAt("receiver", "h", index("h *Holder"))
	parameterID := symbolAt("parameter", "p", index("p T) (result"))
	resultID := symbolAt("result", "result", index("result T)"))
	methodID := symbolAt("method", "Method", index("Method(p T)"))
	outerLocal := index("local := p")
	innerLocal := strings.LastIndex(source, "local := p")
	outerID := symbolAt("variable", "local", outerLocal)
	innerID := symbolAt("variable", "local", innerLocal)
	innerParameter := index("func(p T)") + len("func(")
	innerParameterID := symbolAt("parameter", "p", innerParameter)
	if outerID == innerID || parameterID == innerParameterID {
		t.Fatal("shadowed bindings share an identity")
	}
	symbolScope := func(id string) string {
		t.Helper()
		for _, symbol := range typedSyntax.Symbols {
			if symbol.SymbolID == id {
				return symbol.ScopeKey
			}
		}
		t.Fatal("symbol scope missing")
		return ""
	}
	useScope := func(name string, offset int) string {
		t.Helper()
		for _, occurrence := range result.Occurrences {
			if occurrence.Path == "typed.go" && occurrence.Kind == "use" && occurrence.Name == name && occurrence.Range.StartByte == int64(offset) {
				return occurrence.ScopeID
			}
		}
		t.Fatal("use scope missing")
		return ""
	}
	outerUse := index("h.Field = local") + len("h.Field = ")
	innerUse := index("return local") + len("return ")
	if symbolScope(outerID) == symbolScope(innerID) || useScope("local", outerUse) != symbolScope(outerID) || useScope("local", innerUse) != symbolScope(innerID) {
		t.Fatal("nested use was assigned to the wrong lexical scope")
	}
	useAt("p", outerLocal+len("local := "), parameterID, "")
	useAt("p", innerLocal+len("local := "), innerParameterID, "")
	fieldAssignment := index("h.Field = local")
	useAt("h", fieldAssignment, receiverID, "")
	useAt("Field", fieldAssignment+len("h."), fieldID, "field")
	useAt("local", fieldAssignment+len("h.Field = "), outerID, "")
	redeclare := index("local, next := local, 1")
	useAt("local", redeclare, outerID, "")
	useAt("local", redeclare+len("local, next := "), outerID, "")
	numericID := symbolAt("variable", "n", index("n := 1"))
	useAt("n", index("n += 1"), numericID, "")
	useAt("local", index("ptr := &local")+len("ptr := &"), outerID, "")
	useAt("local", index("return local")+len("return "), innerID, "")
	useAt("result", index("return result")+len("return "), resultID, "")
	useAt("Method", index("h.Method")+len("h."), methodID, "method_value")
	useAt("Method", index("(*Holder[T]).Method")+len("(*Holder[T])."), methodID, "method_expression")
	unicodeParam := index("giá int")
	unicodeLocal := index("cục := giá")
	unicodeParamID := symbolAt("parameter", "giá", unicodeParam)
	unicodeLocalID := symbolAt("variable", "cục", unicodeLocal)
	useAt("giá", unicodeLocal+len("cục := "), unicodeParamID, "")
	useAt("cục", index("return cục")+len("return "), unicodeLocalID, "")
	otherField := strings.Index(otherSource, "other.Field") + len("other.")
	otherMethod := strings.Index(otherSource, "other.Method") + len("other.")
	for _, want := range []struct {
		name          string
		offset        int
		id, selection string
	}{{"Field", otherField, fieldID, "field"}, {"Method", otherMethod, methodID, "method_value"}} {
		found := false
		for _, occurrence := range result.Occurrences {
			if occurrence.Path == "other.go" && occurrence.Kind == "use" && occurrence.Name == want.name && occurrence.Range.StartByte == int64(want.offset) {
				found = occurrence.SymbolID != nil && *occurrence.SymbolID == want.id && occurrence.Selection == want.selection && occurrence.Resolution == "captured"
			}
		}
		if !found {
			t.Fatalf("cross-file generic binding missing: %s", want.name)
		}
	}
	firstV := index("_ = v") + len("_ = ")
	lastV := strings.LastIndex(source, "_ = v") + len("_ = ")
	var vIDs, vScopes []string
	for _, occurrence := range result.Occurrences {
		if occurrence.Kind == "use" && occurrence.Name == "v" && (occurrence.Range.StartByte == int64(firstV) || occurrence.Range.StartByte == int64(lastV)) {
			if occurrence.SymbolID == nil || occurrence.Resolution != "captured" {
				t.Fatalf("type-switch variable was unresolved: %+v", occurrence)
			}
			vIDs = append(vIDs, *occurrence.SymbolID)
			vScopes = append(vScopes, occurrence.ScopeID)
		}
	}
	if len(vIDs) != 2 || vIDs[0] == vIDs[1] || len(vScopes) != 2 || vScopes[0] == "" || vScopes[0] == vScopes[1] {
		t.Fatal("type-switch cases did not get distinct binding identities")
	}
	runGraphTypedFixture(t, ctx, request, source, otherSource, methodID, numericID)
	again, err := AnalyzeTypedCaptured(ctx, request)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("separate loads did not produce deterministic source identities: %v", err)
	}
	mutating := request
	profile := *request.Loader.Preflight.Profile
	profile.BuildTags = append([]string{}, profile.BuildTags...)
	profile.ModuleRoots = append([]string{}, profile.ModuleRoots...)
	profile.DependencyRoots = append([]string{}, profile.DependencyRoots...)
	mutating.Loader.Preflight.Profile = &profile
	mutating.Loader.Preflight.AuthorizeScope = func(context.Context, string, string, string, int64) error { profile.Goos = "windows"; return nil }
	if got, err := AnalyzeTypedCaptured(ctx, mutating); err != nil || !got.Loader.BuildSelectionVerified || got.Loader.SnapshotDigest != preflight.Snapshot.Manifest().SnapshotDigest || profile.Goos != "windows" {
		t.Fatalf("profile mutation changed an in-flight typed load: %+v %v", got, err)
	}
	limited := request
	limited.MaxOccurrences = 1
	if got, err := AnalyzeTypedCaptured(ctx, limited); !errors.Is(err, ErrSyntaxLimit) || len(got.Occurrences) != 0 {
		t.Fatalf("occurrence budget leaked output: %+v %v", got, err)
	}
	limited = request
	limited.MaxOutputBytes = 1024
	if got, err := AnalyzeTypedCaptured(ctx, limited); !errors.Is(err, ErrSyntaxLimit) || len(got.Occurrences) != 0 {
		t.Fatalf("output budget leaked typed facts: %+v %v", got, err)
	}
	checks := 0
	revoked := request
	revoked.Loader.VerifyScratch = func(ctx context.Context, root, workspace string, captured *snapshot.Snapshot) (bool, error) {
		checks++
		if checks == 3 {
			return false, nil
		}
		return verify(ctx, root, workspace, captured)
	}
	if got, err := AnalyzeTypedCaptured(ctx, revoked); err != nil || got.Loader.BuildSelectionVerified || len(got.Occurrences) != 0 || !containsReason(got.Reasons, "SCRATCH_CHANGED") {
		t.Fatalf("late revocation released typed facts: %+v %v", got, err)
	}
	checks = 0
	revoked = request
	revoked.Loader.Preflight.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) {
		checks++
		return checks < 3, nil
	}
	if got, err := AnalyzeTypedCaptured(ctx, revoked); err != nil || got.Loader.BuildSelectionVerified || len(got.Occurrences) != 0 || !containsReason(got.Reasons, "CLOSURE_CHANGED") {
		t.Fatalf("changed closure released typed facts: %+v %v", got, err)
	}
}
