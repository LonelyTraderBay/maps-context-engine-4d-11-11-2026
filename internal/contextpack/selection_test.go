package contextpack

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func fixture(t *testing.T) (context.Context, Input) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	files := map[string]string{
		"a.go":      "package a\n// 😀 captured\nfunc Same() { Helper(); Dynamic() }\nfunc Helper() { Same() }\nfunc Caller() { Same() }\nfunc Dynamic() {}\n",
		"b.go":      "package b\nfunc Same() {}\nfunc Method() {}\nfunc Implementation() {}\n",
		"a_test.go": "package a\nfunc TestSame() { Same() }\n",
	}
	worktree := "11111111-1111-4111-8111-111111111111"
	generation := "22222222-2222-4222-8222-222222222222"
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	captured := []snapshot.File{}
	for path, text := range files {
		f, err := snapshot.NewFile(path, []byte(text), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := f.ContentHash(), int64(f.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &size})
		captured = append(captured, f)
	}
	snap, err := snapshot.Freeze(manifest, captured, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: manifest.ProfileID, GenerationID: generation, SnapshotDigest: snap.Manifest().SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333", RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	sym := func(id, path, name string) model.SymbolFact {
		pkg := "pkg-" + path
		start := strings.Index(files[path], "func "+name) + 5
		if start < 5 {
			t.Fatal("missing fixture symbol", name)
		}
		return model.SymbolFact{SymbolID: id, Path: &path, PackageKey: &pkg, Kind: "function", Name: name, QualifiedName: pkg + "." + name, ScopeKey: "scope", Range: &model.ByteRange{StartByte: int64(start), EndByte: int64(start + len(name))}}
	}
	symbols := []model.SymbolFact{sym("target", "a.go", "Same"), sym("helper", "a.go", "Helper"), sym("caller", "a.go", "Caller"), sym("other-same", "b.go", "Same"), sym("method", "b.go", "Method"), sym("implementation", "b.go", "Implementation"), sym("test", "a_test.go", "TestSame")}
	site := func(path, needle string) model.ByteRange {
		at := strings.Index(files[path], needle)
		if at < 0 {
			t.Fatal("missing fixture site", needle)
		}
		return model.ByteRange{StartByte: int64(at), EndByte: int64(at + len(needle))}
	}
	ptr := func(s string) *string { return &s }
	relations := []model.RelationFact{
		{RelationID: "r-caller", SourceSymbolID: "caller", TargetSymbolID: ptr("target"), Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: "a.go", Range: site("a.go", "Same() }\nfunc Dynamic")},
		{RelationID: "r-helper", SourceSymbolID: "target", TargetSymbolID: ptr("helper"), Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: "a.go", Range: site("a.go", "Helper();")},
		{RelationID: "r-cycle", SourceSymbolID: "helper", TargetSymbolID: ptr("target"), Kind: "CALLS_DIRECT", EvidenceKind: "type_resolved", SitePath: "a.go", Range: site("a.go", "Same() }\nfunc Caller")},
		{RelationID: "r-interface", SourceSymbolID: "target", TargetSymbolID: ptr("method"), Kind: "CALLS_INTERFACE", EvidenceKind: "type_resolved", SitePath: "a.go", Range: site("a.go", "Dynamic()")},
		{RelationID: "r-candidate", SourceSymbolID: "method", TargetSymbolID: ptr("implementation"), Kind: "MAY_DISPATCH_TO", EvidenceKind: "type_possible", SitePath: "a.go", Range: site("a.go", "Dynamic()")},
		{RelationID: "r-unresolved", SourceSymbolID: "target", Kind: "UNRESOLVED_CALL", EvidenceKind: "unresolved", SitePath: "a.go", Range: site("a.go", "Dynamic()"), Reason: "function_value", UnresolvedLabel: ptr("Dynamic")},
		{RelationID: "r-test", SourceSymbolID: "test", TargetSymbolID: ptr("target"), Kind: "TEST_REF", EvidenceKind: "type_resolved", SitePath: "a_test.go", Range: site("a_test.go", "Same()")},
	}
	scanner, err := policy.NewScanner(4 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	packages := []model.PackageFact{{PackageKey: "pkg-a.go", ImportPath: "fixture/a", ModuleKey: "module-a", Variant: "production", InputDigest: member.SnapshotDigest, SemanticStatus: "partial"}, {PackageKey: "pkg-b.go", ImportPath: "fixture/b", ModuleKey: "module-b", Variant: "production", InputDigest: member.SnapshotDigest, SemanticStatus: "partial"}, {PackageKey: "pkg-a_test.go", ImportPath: "fixture/a", ModuleKey: "module-a", Variant: "internal_test", InputDigest: member.SnapshotDigest, SemanticStatus: "partial"}}
	return ctx, Input{Scope: scope, Target: model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: worktree, ProfileID: member.ProfileID, GenerationID: generation}, SymbolID: "target"}, Snapshot: snap, Packages: packages, Symbols: symbols, Relations: relations, Coverage: model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}, Scanner: scanner,
		AuthorizeScope: func(context.Context, model.ScopeVector, model.ScopeMember) error { return nil }, AuthorizePath: func(context.Context, model.ScopeMember, string) error { return nil }, VerifyFacts: func(_ context.Context, m model.ScopeMember, d string) error {
			if m.GenerationID != generation || len(d) != 64 {
				return errors.New("not bound")
			}
			return nil
		}, MaxManifestBytes: 65536, MaxFactBytes: 4 * 1024 * 1024, MaxSourceBytes: 1024 * 1024, MaxFacts: 10000, MaxNodes: 100, MaxEdges: 100, MaxDepth: 8, MaxOutputBytes: 4 * 1024 * 1024}
}

func TestSelectExactTargetEvidenceAndDeterminism(t *testing.T) {
	ctx, in := fixture(t)
	selection, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err := selection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Target.SymbolID != "target" || !report.EnumerationComplete || len(report.References) != 7 {
		t.Fatalf("unexpected target or references: %+v", report)
	}
	expected := map[string]string{"r-caller": "static", "r-helper": "static", "r-cycle": "static", "r-interface": "interface", "r-candidate": "candidate", "r-unresolved": "unresolved", "r-test": "static"}
	for _, r := range report.References {
		if expected[r.Relation.RelationID] != r.Classification {
			t.Fatalf("unexpected reference: %+v", r)
		}
		delete(expected, r.Relation.RelationID)
		file, _ := in.Snapshot.File(r.Source.Path)
		if err := r.Source.ValidateCapturedBytes(file.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing references %v", expected)
	}
	groupFound := false
	for _, group := range report.Groups {
		if group.RelationKind == "CALLS_INTERFACE" && group.SourceModuleKey == "module-a" && group.TargetModuleKey == "module-b" && group.Classification == "interface" && group.SelectedCount == 1 {
			groupFound = true
		}
	}
	if !groupFound {
		t.Fatal("module and semantic group missing")
	}
	for _, node := range report.Nodes {
		if node.SymbolID == "other-same" {
			t.Fatal("same-name symbol was selected without evidence")
		}
	}
	for i := 1; i < len(report.References); i++ {
		a, b := report.References[i-1].Relation, report.References[i].Relation
		if a.Kind > b.Kind || a.Kind == b.Kind && (a.SitePath > b.SitePath || a.SitePath == b.SitePath && (a.Range.StartByte > b.Range.StartByte || a.Range.StartByte == b.Range.StartByte && a.RelationID > b.RelationID)) {
			t.Fatal("reference order changed")
		}
	}
	for i, a := range report.Sources {
		for _, b := range report.Sources[i+1:] {
			if a.Source.Path == b.Source.Path && a.Source.Range.StartByte < b.Source.Range.EndByte && b.Source.Range.StartByte < a.Source.Range.EndByte {
				t.Fatal("overlapping ranges were not merged")
			}
		}
	}
	if !reflect.DeepEqual(report.CycleWitness, []string{"r-helper", "r-cycle"}) && !reflect.DeepEqual(report.CycleWitness, []string{"r-cycle", "r-helper"}) {
		t.Fatalf("missing actual directed cycle: %v", report.CycleWitness)
	}
	if len(report.Sources) == 0 || report.Sources[0].Source.Path != "a.go" || report.Sources[0].Source.Range.StartByte != 0 {
		t.Fatalf("target is not first: %+v", report.Sources)
	}
	contents, err := selection.ReadSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range contents {
		file, _ := in.Snapshot.File(v.Source.Path)
		bytes, err := file.Range(int(v.Source.Range.StartByte), int(v.Source.Range.EndByte))
		if err != nil || string(bytes) != v.Content {
			t.Fatal("not the selected captured bytes")
		}
	}
	if !strings.Contains(contents[0].Content, "func Same() { Helper(); Dynamic() }") {
		t.Fatal("target body is missing")
	}
	for i := int64(1); i <= 10; i++ {
		rng := rand.New(rand.NewSource(i))
		shuffled := in
		shuffled.Packages = append([]model.PackageFact(nil), in.Packages...)
		shuffled.Symbols = append([]model.SymbolFact(nil), in.Symbols...)
		shuffled.Relations = append([]model.RelationFact(nil), in.Relations...)
		rng.Shuffle(len(shuffled.Symbols), func(i, j int) { shuffled.Symbols[i], shuffled.Symbols[j] = shuffled.Symbols[j], shuffled.Symbols[i] })
		rng.Shuffle(len(shuffled.Packages), func(i, j int) {
			shuffled.Packages[i], shuffled.Packages[j] = shuffled.Packages[j], shuffled.Packages[i]
		})
		rng.Shuffle(len(shuffled.Relations), func(i, j int) {
			shuffled.Relations[i], shuffled.Relations[j] = shuffled.Relations[j], shuffled.Relations[i]
		})
		next, err := Select(ctx, shuffled)
		if err != nil {
			t.Fatal(err)
		}
		got, err := next.Read(ctx)
		if err != nil || !reflect.DeepEqual(got, report) {
			t.Fatalf("order changed on permutation %d: %v", i, err)
		}
	}
	report.Scope.Members[0].GenerationID = "mutated"
	report.References[0].Relation.RelationID = "mutated"
	again, err := selection.Read(ctx)
	if err != nil || again.Scope.Members[0].GenerationID != in.Target.GenerationID || again.References[0].Relation.RelationID == "mutated" {
		t.Fatal("returned values changed selection")
	}
}

func hasGap(r Report, code string) bool {
	for _, g := range r.Gaps {
		if g.Code == code {
			return g.Count == nil
		}
	}
	return false
}

func TestTraversalBoundsNeverAssertCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*Input)
	}{{"depth", "GRAPH_DEPTH_LIMIT", func(in *Input) { in.MaxDepth = 0 }}, {"nodes", "GRAPH_NODE_LIMIT", func(in *Input) { in.MaxNodes = 1 }}, {"edges", "GRAPH_EDGE_LIMIT", func(in *Input) { in.MaxEdges = 1 }}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, in := fixture(t)
			tc.change(&in)
			s, err := Select(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.Read(ctx)
			if err != nil || r.EnumerationComplete || !hasGap(r, tc.code) {
				t.Fatalf("limit hidden: %+v %v", r, err)
			}
		})
	}
}

func TestSelectionRejectsMismatchesAndRevocation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Input)
		want   error
	}{
		{"generation", func(in *Input) { in.Target.GenerationID = "66666666-6666-4666-8666-666666666666" }, model.ErrMemberNotInScope},
		{"target", func(in *Input) { in.Target.SymbolID = "absent" }, ErrTarget},
		{"scope digest", func(in *Input) { in.Scope.VectorDigest = strings.Repeat("0", 64) }, ErrInput},
		{"duplicate symbol", func(in *Input) { in.Symbols = append(in.Symbols, in.Symbols[0]) }, ErrInput},
		{"duplicate package", func(in *Input) { in.Packages = append(in.Packages, in.Packages[0]) }, ErrInput},
		{"package snapshot", func(in *Input) { in.Packages[0].InputDigest = strings.Repeat("0", 64) }, ErrInput},
		{"duplicate relation", func(in *Input) { in.Relations = append(in.Relations, in.Relations[0]) }, ErrInput},
		{"missing endpoint", func(in *Input) { bad := "not-present"; in.Relations[0].TargetSymbolID = &bad }, ErrInput},
		{"split unicode", func(in *Input) {
			at := int64(strings.Index(stringFile(in.Snapshot, "a.go"), "😀"))
			in.Relations[0].Range = model.ByteRange{StartByte: at + 1, EndByte: at + 2}
		}, ErrInput},
		{"scope denied", func(in *Input) {
			in.AuthorizeScope = func(context.Context, model.ScopeVector, model.ScopeMember) error {
				return errors.New("sensitive detail")
			}
		}, ErrAuthority},
		{"path denied", func(in *Input) {
			in.AuthorizePath = func(context.Context, model.ScopeMember, string) error { return errors.New("sensitive detail") }
		}, ErrAuthority},
		{"facts denied", func(in *Input) {
			in.VerifyFacts = func(context.Context, model.ScopeMember, string) error { return errors.New("sensitive detail") }
		}, ErrAuthority},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, in := fixture(t)
			tc.change(&in)
			_, err := Select(ctx, in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	ctx, in := fixture(t)
	count := 0
	in.VerifyFacts = func(context.Context, model.ScopeMember, string) error {
		count++
		if count >= 4 {
			return errors.New("revoked")
		}
		return nil
	}
	if _, err := Select(ctx, in); !errors.Is(err, ErrAuthority) {
		t.Fatalf("postcheck revocation accepted: %v", err)
	}
	ctx, in = fixture(t)
	cancelled, cancel := context.WithCancel(ctx)
	in.AuthorizePath = func(context.Context, model.ScopeMember, string) error { cancel(); return nil }
	if _, err := Select(cancelled, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	ctx, in = fixture(t)
	in.AuthorizeScope = func(_ context.Context, scope model.ScopeVector, member model.ScopeMember) error {
		scope.Members[0].GenerationID = "changed"
		return nil
	}
	s, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Read(ctx)
	if err != nil || r.Scope.Members[0].GenerationID != in.Target.GenerationID {
		t.Fatal("callback changed retained authority scope")
	}
}

func stringFile(s *snapshot.Snapshot, path string) string {
	f, _ := s.File(path)
	return string(f.Bytes())
}

func TestThousandsOfReferencesAndEdgeLimit(t *testing.T) {
	ctx, in := fixture(t)
	var fan strings.Builder
	fan.WriteString("package a\nfunc Fan() {\n")
	start := fan.Len()
	for i := 0; i < 3001; i++ {
		fan.WriteString("Same()\n")
	}
	fan.WriteString("}\n")
	newFile, err := snapshot.NewFile("fan.go", []byte(fan.String()), 65536)
	if err != nil {
		t.Fatal(err)
	}
	manifest := in.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, size := newFile.ContentHash(), int64(newFile.Len())
	manifest.Files = append(manifest.Files, model.ManifestFile{Path: "fan.go", Status: "captured", ContentHash: &hash, ByteLength: &size})
	files := []snapshot.File{newFile}
	for _, entry := range manifest.Files {
		if entry.Path != "fan.go" {
			f, ok := in.Snapshot.File(entry.Path)
			if !ok {
				t.Fatal("missing fixture file")
			}
			files = append(files, f)
		}
	}
	in.Snapshot, err = snapshot.Freeze(manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	in.Scope.Members[0].SnapshotDigest = in.Snapshot.Manifest().SnapshotDigest
	for i := range in.Packages {
		in.Packages[i].InputDigest = in.Scope.Members[0].SnapshotDigest
	}
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	for i := range in.Symbols {
		if in.Symbols[i].SymbolID == "caller" {
			path := "fan.go"
			at := int64(strings.Index(fan.String(), "Fan"))
			in.Symbols[i].Path = &path
			in.Symbols[i].Name = "Fan"
			in.Symbols[i].Range = &model.ByteRange{StartByte: at, EndByte: at + 3}
		}
	}
	in.Relations = nil
	for i := 0; i < 3001; i++ {
		id := fmt.Sprintf("reference-%04d", i)
		target := "target"
		at := int64(start + i*len("Same()\n"))
		in.Relations = append(in.Relations, model.RelationFact{RelationID: id, SourceSymbolID: "caller", TargetSymbolID: &target, Kind: "REFERS_TO", EvidenceKind: "type_resolved", SitePath: "fan.go", Range: model.ByteRange{StartByte: at, EndByte: at + 4}})
	}
	in.MaxEdges = 4000
	s, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Read(ctx)
	if err != nil || len(r.References) != 3001 || !r.EnumerationComplete {
		t.Fatalf("catalog lost sites: %d %v", len(r.References), err)
	}
	if len(r.Groups) != 1 || r.Groups[0].SelectedCount != 3001 {
		t.Fatalf("group count lost references: %+v", r.Groups)
	}
	seen := map[int64]bool{}
	for _, ref := range r.References {
		if ref.Source.Path != "fan.go" || seen[ref.Source.Range.StartByte] {
			t.Fatal("duplicate or wrong source site")
		}
		seen[ref.Source.Range.StartByte] = true
		body, e := newFile.Range(int(ref.Source.Range.StartByte), int(ref.Source.Range.EndByte))
		if e != nil || string(body) != "Same" {
			t.Fatal("incorrect reference source")
		}
	}
	in.MaxEdges = 200
	s, err = Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Read(ctx)
	if err != nil || len(r.References) != 200 || r.EnumerationComplete || !hasGap(r, "GRAPH_EDGE_LIMIT") {
		t.Fatalf("limit hidden: %d %v", len(r.References), err)
	}
	if len(r.Groups) != 1 || r.Groups[0].SelectedCount != 200 {
		t.Fatalf("bounded group count is not selected count: %+v", r.Groups)
	}
}

func TestSelectionBlocksSecretInCapturedSource(t *testing.T) {
	ctx, in := fixture(t)
	manifest := in.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	files := []snapshot.File{}
	for i := range manifest.Files {
		entry := &manifest.Files[i]
		file, ok := in.Snapshot.File(entry.Path)
		if !ok {
			t.Fatal("missing source")
		}
		if entry.Path == "a.go" {
			body := append(file.Bytes(), []byte("\n// password="+strings.Repeat("x", 20)+"\n")...)
			var err error
			file, err = snapshot.NewFile(entry.Path, body, 65536)
			if err != nil {
				t.Fatal(err)
			}
			hash, size := file.ContentHash(), int64(file.Len())
			entry.ContentHash = &hash
			entry.ByteLength = &size
		}
		files = append(files, file)
	}
	var err error
	in.Snapshot, err = snapshot.Freeze(manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	in.Scope.Members[0].SnapshotDigest = in.Snapshot.Manifest().SnapshotDigest
	for i := range in.Packages {
		in.Packages[i].InputDigest = in.Scope.Members[0].SnapshotDigest
	}
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Select(ctx, in); !errors.Is(err, policy.ErrSecretBlocked) {
		t.Fatalf("secret was accepted: %v", err)
	}
}

func TestSelectionRechecksReadAuthorityAndCoverageSources(t *testing.T) {
	ctx, in := fixture(t)
	allowed := true
	in.AuthorizePath = func(context.Context, model.ScopeMember, string) error {
		if !allowed {
			return errors.New("revoked")
		}
		return nil
	}
	s, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	allowed = false
	if _, err := s.Read(ctx); !errors.Is(err, ErrAuthority) {
		t.Fatalf("report read after revoke: %v", err)
	}
	if _, err := s.ReadSources(ctx); !errors.Is(err, ErrAuthority) {
		t.Fatalf("source read after revoke: %v", err)
	}
	ctx, in = fixture(t)
	file, _ := in.Snapshot.File("a.go")
	ref := model.SourceRef{WorktreeID: in.Target.WorktreeID, ProfileID: in.Target.ProfileID, GenerationID: "99999999-9999-4999-8999-999999999999", Path: "a.go", ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: 1}}
	in.Coverage.Gaps = []model.Gap{{Code: "BAD_SOURCE", Reason: "wrong generation", SourceRefs: []model.SourceRef{ref}}}
	if _, err := Select(ctx, in); !errors.Is(err, ErrInput) {
		t.Fatalf("coverage source escaped generation: %v", err)
	}
}

func TestSelectionRejectsAmbiguousServiceAndTooSmallOutput(t *testing.T) {
	ctx, in := fixture(t)
	group := "99999999-9999-4999-8999-999999999999"
	revision := int64(1)
	first, second := in.Scope.Members[0], in.Scope.Members[0]
	serviceA, serviceB := "service-a", "service-b"
	first.ServiceID = &serviceA
	second.ServiceID = &serviceB
	in.Scope.Kind = "federated"
	in.Scope.GroupID = &group
	in.Scope.GroupRevision = &revision
	in.Scope.Members = []model.ScopeMember{first, second}
	var err error
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Select(ctx, in); !errors.Is(err, model.ErrAmbiguousMember) {
		t.Fatalf("ambiguous service was selected: %v", err)
	}
	in.ServiceID = &serviceB
	selected, err := Select(ctx, in)
	if err != nil {
		t.Fatalf("explicit service rejected: %v", err)
	}
	report, err := selected.Read(ctx)
	if err != nil || report.EnumerationComplete || !hasGap(report, "OTHER_MEMBERS_NOT_ENUMERATED") {
		t.Fatalf("other members hidden: %+v %v", report, err)
	}
	in.Scope.Members = []model.ScopeMember{first}
	in.Scope.UnavailableMembers = []model.MemberUnavailable{{ServiceID: &serviceB, Status: "index_missing", ReasonCode: "NO_INDEX"}}
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	in.ServiceID = &serviceA
	selected, err = Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	report, err = selected.Read(ctx)
	if err != nil || report.EnumerationComplete || !hasGap(report, "SCOPE_MEMBERS_UNAVAILABLE") {
		t.Fatalf("unavailable member hidden: %+v %v", report, err)
	}
	ctx, in = fixture(t)
	in.MaxOutputBytes = 1024
	if _, err := Select(ctx, in); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize output was accepted: %v", err)
	}
}
