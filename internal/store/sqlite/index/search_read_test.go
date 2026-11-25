package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func searchFixture(t *testing.T, extra ...model.SymbolFact) (*Store, CaptureWrite, search.Request) {
	t.Helper()
	store, _, begin := beginFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inputs := []struct{ path, source string }{
		{"a/a.go", "package a\n// é\nfunc Same() {}\n"},
		{"b/b.go", "package b\nfunc Same() {}\n"},
		{"schema.sql", "SELECT feature FROM customers;\n"},
		{"ui.ts", "const feature = 'hello';\n"},
	}
	manifest := begin.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	manifest.Files = nil
	var files []snapshot.File
	var classifications []FileClassification
	var paths []string
	for _, input := range inputs {
		file, err := snapshot.NewFile(input.path, []byte(input.source), 1024)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: input.path, Status: "captured", ContentHash: &hash, ByteLength: &length})
		files = append(files, file)
		classifications = append(classifications, FileClassification{Path: input.path, Kind: "source"})
		paths = append(paths, input.path)
	}
	var err error
	begin.Snapshot, err = snapshot.Freeze(manifest, files, 8192, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	capture := CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, PolicyEpoch: 1, Snapshot: begin.Snapshot, Writer: begin.Writer, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 4096, Authorize: authorize}
	if err := store.StageFiles(ctx, StageFilesRequest{Capture: capture, Files: classifications}); err != nil {
		t.Fatal(err)
	}
	if err := store.StageSearchDocuments(ctx, StageSearchDocumentsRequest{Capture: capture, AuthorizeGeneration: authorize, Paths: paths, MaxBatchBytes: 4096}); err != nil {
		t.Fatal(err)
	}
	stream, err := store.BeginProviderStage(ctx, capture, authorize)
	if err != nil {
		t.Fatal(err)
	}
	batch := providerBatchFixture()
	batch.Packages, batch.Symbols, batch.Relations = []model.PackageFact{}, []model.SymbolFact{}, []model.RelationFact{}
	for i := 0; i < 2; i++ {
		key, filename := fmt.Sprintf("pkg%d", i), inputs[i].path
		start := int64(strings.Index(inputs[i].source, "Same"))
		batch.Packages = append(batch.Packages, model.PackageFact{PackageKey: key, ImportPath: fmt.Sprintf("example.invalid/%d", i), ModuleKey: "module", Variant: "production", InputDigest: strings.Repeat("a", 64), SemanticStatus: "partial"})
		batch.Symbols = append(batch.Symbols, model.SymbolFact{SymbolID: fmt.Sprintf("symbol%d", i), Path: &filename, PackageKey: &key, Kind: "function", Name: "Same", QualifiedName: fmt.Sprintf("pkg%d.Same", i), ScopeKey: key, Range: &model.ByteRange{StartByte: start, EndByte: start + 4}, Signature: "func Same()"})
	}
	filename, key := "b/b.go", "pkg1"
	batch.Symbols = append(batch.Symbols, model.SymbolFact{SymbolID: "file-b", Path: &filename, PackageKey: &key, Kind: "file", Name: "b.go", QualifiedName: "pkg1.b.go", ScopeKey: key, Range: &model.ByteRange{StartByte: 0, EndByte: int64(len(inputs[1].source))}})
	batch.Symbols = append(batch.Symbols, extra...)
	stage := StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: 65536}
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishProvider(ctx, publishProviderFixture(stage)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseProviderStage(ctx, capture, stream, authorize); err != nil {
		t.Fatal(err)
	}
	request := search.Request{ProfileID: capture.Snapshot.Manifest().ProfileID, Limit: 10, MaxOutputBytes: 65536, MaxManifestBytes: 8192, MaxFileBytes: 4096, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: scanner}
	return store, capture, request
}

func TestSearchSymbolsAmbiguityPositionAndPrefix(t *testing.T) {
	store, capture, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request.Mode, request.Query = "exact", "Same"
	result, err := store.Search(ctx, request)
	if err != nil || !result.Ambiguous || result.Truncated || len(result.Symbols) != 2 || result.Symbols[0].Source.Path != "a/a.go" || result.Symbols[1].Source.Path != "b/b.go" || result.Coverage != "unknown" || result.Freshness != "not_checked" {
		t.Fatalf("ambiguous lookup: %+v %v", result, err)
	}
	request.Mode, request.Query = "prefix", "Sam"
	result, err = store.Search(ctx, request)
	if err != nil || len(result.Symbols) != 2 {
		t.Fatalf("prefix: %+v %v", result, err)
	}
	request.Mode, request.Query, request.Qualified = "exact", "pkg1.Same", true
	result, err = store.Search(ctx, request)
	if err != nil || result.Ambiguous || len(result.Symbols) != 1 || result.Symbols[0].Source.Path != "b/b.go" {
		t.Fatalf("qualified: %+v %v", result, err)
	}
	file, _ := capture.Snapshot.File("b/b.go")
	request.Mode, request.Query, request.Qualified = "position", "", false
	request.Position = &model.PositionSelector{Kind: "source_position", Path: "b/b.go", ContentHash: file.ContentHash(), ByteOffset: 15}
	result, err = store.Search(ctx, request)
	if err != nil || result.Ambiguous || len(result.Symbols) != 1 || result.Symbols[0].Symbol.SymbolID != "symbol1" {
		t.Fatalf("position: %+v %v", result, err)
	}
	request.Position.ByteOffset = 0
	result, err = store.Search(ctx, request)
	if err != nil || result.Ambiguous || len(result.Symbols) != 0 {
		t.Fatalf("non-symbol position selected the file: %+v %v", result, err)
	}
	request.Position.ByteOffset = 19
	result, err = store.Search(ctx, request)
	if err != nil || result.Ambiguous || len(result.Symbols) != 0 {
		t.Fatalf("position at exclusive end matched symbol: %+v %v", result, err)
	}
	request.Position.ByteOffset = 15
	request.Position.ContentHash = strings.Repeat("a", 64)
	result, err = store.Search(ctx, request)
	if !errors.Is(err, ErrCaptureMismatch) || result.GenerationID != "" {
		t.Fatalf("wrong position hash accepted: %+v %v", result, err)
	}
	file, _ = capture.Snapshot.File("a/a.go")
	request.Position.Path, request.Position.ContentHash = "a/a.go", file.ContentHash()
	request.Position.ByteOffset = int64(strings.Index(string(file.Bytes()), "é") + 1)
	if _, err := store.Search(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("position split UTF-8: %v", err)
	}
}

func TestSearchSymbolPathDisambiguatesIdenticalNames(t *testing.T) {
	store, _, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request.Mode, request.Query, request.Path = "exact", "Same", "b/b.go"
	result, err := store.Search(ctx, request)
	if err != nil || result.Ambiguous || len(result.Symbols) != 1 ||
		result.Symbols[0].Source == nil || result.Symbols[0].Source.Path != request.Path {
		t.Fatalf("file-scoped search: %+v %v", result, err)
	}
}

func TestSearchPositionRetainsOverlappingRealSymbols(t *testing.T) {
	filename, key := "b/b.go", "pkg1"
	extra := model.SymbolFact{SymbolID: "symbol1-alternate", Path: &filename, PackageKey: &key, Kind: "function", Name: "Same", QualifiedName: "pkg1.Same", ScopeKey: key, Range: &model.ByteRange{StartByte: 15, EndByte: 19}, Signature: "func Same()"}
	store, capture, request := searchFixture(t, extra)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file, _ := capture.Snapshot.File(filename)
	request.Mode = "position"
	request.Position = &model.PositionSelector{Kind: "source_position", Path: filename, ContentHash: file.ContentHash(), ByteOffset: 15}
	result, err := store.Search(ctx, request)
	if err != nil || !result.Ambiguous || len(result.Symbols) != 2 {
		t.Fatalf("overlapping real symbols were silently resolved: %+v %v", result, err)
	}
	request.Limit = 1
	result, err = store.Search(ctx, request)
	if err != nil || !result.Ambiguous || !result.Truncated || len(result.Symbols) != 1 {
		t.Fatalf("truncated overlap lost ambiguity: %+v %v", result, err)
	}
}

func TestSearchTextIsLexicalAndBounded(t *testing.T) {
	store, _, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request.Mode, request.Query = "text", "feature"
	result, err := store.Search(ctx, request)
	if err != nil || len(result.Text) != 2 || result.Text[0].Path != "schema.sql" || result.Text[1].Path != "ui.ts" || result.Text[0].EvidenceKind != "text_match" || len(result.Symbols) != 0 || result.Ambiguous {
		t.Fatalf("lexical search: %+v %v", result, err)
	}
	request.Limit = 1
	result, err = store.Search(ctx, request)
	if err != nil || len(result.Text) != 1 || !result.Truncated || result.Text[0].Path != "schema.sql" {
		t.Fatalf("bounded lexical search: %+v %v", result, err)
	}
	request.Limit, request.Query = 10, `missing" OR feature`
	result, err = store.Search(ctx, request)
	if err != nil || len(result.Text) != 0 {
		t.Fatalf("FTS operator injection: %+v %v", result, err)
	}
	request.Mode, request.Query = "exact", "Same' OR 1=1 --"
	result, err = store.Search(ctx, request)
	if err != nil || len(result.Symbols) != 0 {
		t.Fatalf("SQL injection: %+v %v", result, err)
	}
}

func TestSearchRejectsRevocationAndInvalidRequests(t *testing.T) {
	store, _, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request.Mode, request.Query = "exact", "Same"
	calls := 0
	request.AuthorizePath = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 3 {
			return errFixtureLease
		}
		return nil
	}
	if result, err := store.Search(ctx, request); !errors.Is(err, errFixtureLease) || result.GenerationID != "" || len(result.Symbols) != 0 {
		t.Fatalf("revocation leaked: %+v %v", result, err)
	}
	request.AuthorizePath = func(context.Context, string, string, string, int64) error { return nil }
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error { return errFixtureLease }
	if result, err := store.Search(ctx, request); !errors.Is(err, errFixtureLease) || result.GenerationID != "" {
		t.Fatalf("generation revoked: %+v %v", result, err)
	}
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error { return nil }
	checks := 0
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error {
		checks++
		if checks == 2 {
			return errFixtureLease
		}
		return nil
	}
	request.Query = "no-such-symbol"
	if result, err := store.Search(ctx, request); !errors.Is(err, errFixtureLease) || result.GenerationID != "" {
		t.Fatalf("empty result ignored revocation: %+v %v", result, err)
	}
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error { return nil }
	request.Query = "api_key=abcdefghijklmnopqrstuvwxyz"
	if result, err := store.Search(ctx, request); !errors.Is(err, policy.ErrSecretBlocked) || result.GenerationID != "" {
		t.Fatalf("secret query accepted: %+v %v", result, err)
	}
	for _, q := range []string{"", "\x00", string([]byte{0xff}), strings.Repeat("x", 513), strings.Repeat("x", 2049)} {
		request.Query = q
		if _, err := store.Search(ctx, request); !errors.Is(err, ErrInvalidSearch) {
			t.Fatalf("bad query accepted: %q %v", q, err)
		}
	}
	request.Query = "Same"
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if result, err := store.Search(cancelled, request); !errors.Is(err, context.Canceled) || result.GenerationID != "" {
		t.Fatalf("cancelled search leaked: %+v %v", result, err)
	}
	if _, err := store.Search(context.Background(), request); !errors.Is(err, ErrInvalidSearch) {
		t.Fatalf("missing deadline accepted: %v", err)
	}
	request.Limit = 0
	if _, err := store.Search(ctx, request); !errors.Is(err, ErrInvalidSearch) {
		t.Fatalf("zero limit accepted: %v", err)
	}
}

func TestSearchPlansUseIndexes(t *testing.T) {
	store, _, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tc := range []struct {
		mode, query, want string
		qualified         bool
	}{
		{"exact", "Same", "symbols_name", false},
		{"prefix", "Sam", "symbols_name", false},
		{"exact", "pkg0.Same", "symbols_qualified", true},
		{"prefix", "pkg0", "symbols_qualified", true},
	} {
		request.Mode, request.Query, request.Qualified = tc.mode, tc.query, tc.qualified
		q, args := symbolSearchQuery(request, fixtureGeneration)
		if plan := explainPlan(t, ctx, store.writer, q, args...); !strings.Contains(plan, tc.want) {
			t.Errorf("%s %s plan %q missing %s", tc.mode, tc.query, plan, tc.want)
		}
	}
	plan := explainPlan(t, ctx, store.writer, textSearchSQL, `body : "feature"`, fixtureGeneration, 11)
	if !strings.Contains(strings.ToUpper(plan), "VIRTUAL TABLE INDEX") {
		t.Errorf("FTS plan lacks inverted index: %s", plan)
	}
	request.Mode, request.Query = "position", ""
	request.Position = &model.PositionSelector{Kind: "source_position", Path: "b/b.go", ContentHash: strings.Repeat("a", 64), ByteOffset: 15}
	q, args := symbolSearchQuery(request, fixtureGeneration)
	if plan := explainPlan(t, ctx, store.writer, q, args...); !strings.Contains(plan, "symbols_range") {
		t.Errorf("position plan lacks range index: %s", plan)
	}
}

func stageNextSearchGeneration(t *testing.T, store *Store, old CaptureWrite) StageFilesRequest {
	t.Helper()
	const nextGeneration = "77777777-7777-4777-8777-777777777777"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file, err := snapshot.NewFile("ui.ts", []byte("const buildingonly = 'new';\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest := old.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, length := file.ContentHash(), int64(file.Len())
	manifest.Files = []model.ManifestFile{{Path: "ui.ts", Status: "captured", ContentHash: &hash, ByteLength: &length}}
	frozen, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	begin := BeginGenerationRequest{GenerationID: nextGeneration, JobID: old.JobID, PolicyEpoch: old.PolicyEpoch, Snapshot: frozen, Writer: old.Writer, Authorize: authorize, MaxManifestBytes: old.MaxManifestBytes}
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	capture := old
	capture.GenerationID, capture.Snapshot = nextGeneration, frozen
	stage := StageFilesRequest{Capture: capture, Files: []FileClassification{{Path: "ui.ts", Kind: "source"}}}
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	if err := store.StageSearchDocuments(ctx, StageSearchDocumentsRequest{Capture: capture, AuthorizeGeneration: authorize, Paths: []string{"ui.ts"}, MaxBatchBytes: 1024}); err != nil {
		t.Fatal(err)
	}
	return stage
}

func TestSearchSnapshotIsolationAcrossPublication(t *testing.T) {
	store, old, request := searchFixture(t)
	next := stageNextSearchGeneration(t, store, old)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request.Mode, request.Query = "text", "buildingonly"
	if result, err := store.Search(ctx, request); err != nil || len(result.Text) != 0 || result.GenerationID != old.GenerationID {
		t.Fatalf("building generation leaked: %+v %v", result, err)
	}
	request.GenerationID = next.Capture.GenerationID
	if result, err := store.Search(ctx, request); !errors.Is(err, ErrSnapshotUnavailable) || result.GenerationID != "" {
		t.Fatalf("building generation selected: %+v %v", result, err)
	}
	request.GenerationID, request.Query = "", "feature"
	var dbPath string
	if err := store.writer.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&dbPath); err != nil {
		t.Fatal(err)
	}
	readers, err := sql.Open("sqlite", fixtureSQLiteURI(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	readers.SetMaxOpenConns(4)
	defer readers.Close()
	reader, err := Open(ctx, readers, store.writer, fixtureWorktree, store.lease)
	if err != nil {
		t.Fatal(err)
	}
	entered, proceed := make(chan struct{}), make(chan struct{})
	calls := 0
	request.AuthorizePath = func(ctx context.Context, _, _, _ string, _ int64) error {
		calls++
		if calls == 1 {
			close(entered)
			select {
			case <-proceed:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	type response struct {
		value search.Result
		err   error
	}
	completed := make(chan response, 1)
	go func() { value, err := reader.Search(ctx, request); completed <- response{value, err} }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := store.PublishCapturedOnly(ctx, publishRequest(next)); err != nil {
		close(proceed)
		t.Fatal(err)
	}
	close(proceed)
	oldResult := <-completed
	if oldResult.err != nil || oldResult.value.GenerationID != old.GenerationID || len(oldResult.value.Text) != 2 {
		t.Fatalf("mixed generation: %+v %v", oldResult.value, oldResult.err)
	}
	request.AuthorizePath = func(context.Context, string, string, string, int64) error { return nil }
	request.Query = "buildingonly"
	newResult, err := reader.Search(ctx, request)
	if err != nil || newResult.GenerationID != next.Capture.GenerationID || len(newResult.Text) != 1 || newResult.Text[0].Path != "ui.ts" {
		t.Fatalf("new active generation: %+v %v", newResult, err)
	}
	request.GenerationID = old.GenerationID
	oldResult.value, err = reader.Search(ctx, request)
	if err != nil || oldResult.value.GenerationID != old.GenerationID || len(oldResult.value.Text) != 0 {
		t.Fatalf("cross-generation FTS leak: %+v %v", oldResult.value, err)
	}
}

func TestSearchDoesNotReturnOversizedResults(t *testing.T) {
	store, stage := sealedProviderFixture(t, false)
	scanner, err := policy.NewScanner(10000)
	if err != nil {
		t.Fatal(err)
	}
	stage.Capture.Scanner = scanner
	if _, err := store.writer.Exec(`UPDATE symbols SET signature=? WHERE generation_id=?`, strings.Repeat("x", 3500), fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.PublishProvider(ctx, publishProviderFixture(stage)); err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	request := search.Request{ProfileID: stage.Capture.Snapshot.Manifest().ProfileID, Mode: "exact", Query: "main", Limit: 10, MaxOutputBytes: 4096, MaxManifestBytes: 8192, MaxFileBytes: 4096, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: scanner}
	if result, err := store.Search(ctx, request); !errors.Is(err, ErrCaptureLimit) || result.GenerationID != "" || len(result.Symbols) != 0 {
		t.Fatalf("oversized result leaked: %+v %v", result, err)
	}
}

func TestPrefixUpperUnicodeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		prefix, upper string
		ok            bool
	}{
		{"abc", "abd", true}, {"a\ud7ff", "a\ue000", true}, {"a\U0010ffff", "b", true}, {"\U0010ffff", "", false},
	} {
		upper, ok := prefixUpper(tc.prefix)
		if upper != tc.upper || ok != tc.ok {
			t.Errorf("prefix %q: %q %v", tc.prefix, upper, ok)
		}
	}
}

func TestSearchSupportsLongUnicodePortablePath(t *testing.T) {
	store, _, begin := beginFixture(t)
	filename := strings.Repeat("é", 2200) + ".ts"
	file, err := snapshot.NewFile(filename, []byte("const uniqueword = 1;\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest := begin.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, length := file.ContentHash(), int64(file.Len())
	manifest.Files = []model.ManifestFile{{Path: filename, Status: "captured", ContentHash: &hash, ByteLength: &length}}
	begin.MaxManifestBytes = 32 * 1024
	begin.Snapshot, err = snapshot.Freeze(manifest, []snapshot.File{file}, begin.MaxManifestBytes, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(16 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	capture := CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, PolicyEpoch: 1, Snapshot: begin.Snapshot, Writer: begin.Writer, Scanner: scanner, MaxManifestBytes: begin.MaxManifestBytes, MaxSourceBytes: 1024, Authorize: authorize}
	stage := StageFilesRequest{Capture: capture, Files: []FileClassification{{Path: filename, Kind: "source"}}}
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	searchStage := StageSearchDocumentsRequest{Capture: capture, AuthorizeGeneration: authorize, Paths: []string{filename}, MaxBatchBytes: 1024}
	if err := store.StageSearchDocuments(ctx, searchStage); !errors.Is(err, ErrCaptureLimit) {
		t.Fatalf("document metadata escaped batch budget: %v", err)
	}
	searchStage.MaxBatchBytes = 32 * 1024
	if err := store.StageSearchDocuments(ctx, searchStage); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishCapturedOnly(ctx, publishRequest(stage)); err != nil {
		t.Fatal(err)
	}
	request := search.Request{ProfileID: begin.Snapshot.Manifest().ProfileID, Mode: "text", Query: "uniqueword", Limit: 1, MaxOutputBytes: 65536, MaxManifestBytes: begin.MaxManifestBytes, MaxFileBytes: 1024, AuthorizeGeneration: authorize, AuthorizePath: authorize, Scanner: scanner}
	result, err := store.Search(ctx, request)
	if err != nil || len(result.Text) != 1 || result.Text[0].Path != filename {
		t.Fatalf("Unicode path search: %v, hits=%d", err, len(result.Text))
	}
}

func explainPlan(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, " | ")
}
