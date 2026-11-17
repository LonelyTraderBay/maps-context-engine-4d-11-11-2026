package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// This fixture uses the actual SQLite staging and publication APIs, with fake
// leases and authority callbacks. No target workspace or process is executed.
func publishedGraphFixture(t *testing.T, relations int) (context.Context, *Store, *sql.DB, PublishedGraphRequest, model.ScopeVector) {
	t.Helper()
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	manifest := stage.Capture.Snapshot.Manifest()
	stage.Batch.Packages[0].InputDigest = manifest.SnapshotDigest
	stage.Batch.Relations = nil
	for i := 0; i < relations; i++ {
		rel := providerBatchFixture().Relations[0]
		rel.RelationID = fmt.Sprintf("relation-%03d", i)
		stage.Batch.Relations = append(stage.Batch.Relations, rel)
	}
	if stage.Batch.Relations == nil {
		stage.Batch.Relations = []model.RelationFact{}
	}
	stage.Batch.Diagnostics = []model.DiagnosticFact{{DiagnosticID: "warning", Code: "FIXTURE", Severity: "warning", Message: "Fixture diagnostic.", ScopeDescription: "workspace"}}
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
	if err := store.ReleaseProviderStage(ctx, stage.Capture, stage.Stream, func(context.Context, string, string, string, int64) error { return nil }); err != nil {
		t.Fatal(err)
	}
	member := model.ScopeMember{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: stage.Capture.GenerationID, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: store.identity.Incarnation, RecordsIncarnation: fixtureGeneration}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: fixtureGeneration, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	r := PublishedGraphRequest{Member: member, VectorDigest: scope.VectorDigest, AuthorizeGeneration: func(context.Context, string, string, string, int64) error { return nil }, AuthorizePath: stage.Capture.Authorize, Scanner: stage.Capture.Scanner, MaxManifestBytes: 8192, MaxFileBytes: 1024, MaxSourceBytes: 1024, MaxFactBytes: 65536, MaxFacts: 100}
	return ctx, store, store.writer, r, scope
}

func TestReadPublishedGraphFeedsProjectedTraversal(t *testing.T) {
	ctx, store, db, request, scope := publishedGraphFixture(t, 37)
	var before int64
	if err := db.QueryRow(`SELECT count(*) FROM relations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	catalog, err := store.ReadPublishedGraph(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := catalog.Snapshot.File("main.go")
	if !ok || string(file.Bytes()) != "package main\n" || len(catalog.Packages) != 1 || len(catalog.Symbols) != 1 || len(catalog.Relations) != 37 || len(catalog.Diagnostics) != 1 || catalog.Coverage.Status != "partial" || len(catalog.Coverage.Gaps) != 1 {
		t.Fatal("published source, facts, diagnostic or coverage were lost")
	}
	input := contextpack.Input{Scope: scope, Target: model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{WorktreeID: request.Member.WorktreeID, ProfileID: request.Member.ProfileID, GenerationID: request.Member.GenerationID}, SymbolID: "symbol"}, Snapshot: catalog.Snapshot, Packages: catalog.Packages, Symbols: catalog.Symbols, Relations: catalog.Relations, Coverage: catalog.Coverage, Scanner: request.Scanner, AuthorizeScope: func(context.Context, model.ScopeVector, model.ScopeMember) error { return nil }, AuthorizePath: func(context.Context, model.ScopeMember, string) error { return nil }, VerifyFacts: catalog.VerifyFacts, MaxManifestBytes: 8192, MaxFactBytes: 65536, MaxSourceBytes: 1024, MaxFacts: 100, MaxNodes: 100, MaxEdges: 100, MaxDepth: 2, MaxOutputBytes: 65536}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "projection-v1", Scope: scope}
	var key [32]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, contextpack.CursorBinding) error { return nil }
	graph, err := overview.NewProjectedGraph(ctx, input, codec, binding, guard, 100, 100, 65536)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("cursor did not terminate")
		}
		page, err := graph.Page(ctx, cursor, binding.PrincipalDigest, guard, 10, 5, 8192)
		if err != nil {
			t.Fatal(err)
		}
		if page.Coverage.Status != "partial" || len(page.Gaps) == 0 {
			t.Fatal("page lost provider coverage or gaps")
		}
		for _, edge := range page.Result.Data.Edges {
			if len(edge.EvidenceRefs) != 1 || seen[edge.EvidenceRefs[0].EvidenceID] {
				t.Fatal("edge lost its unique evidence")
			}
			seen[edge.EvidenceRefs[0].EvidenceID] = true
		}
		if page.Result.NextCursor == nil {
			break
		}
		cursor = *page.Result.NextCursor
	}
	if len(seen) != 37 {
		t.Fatalf("graph lost published relations: got %d", len(seen))
	}
	var after int64
	if err := db.QueryRow(`SELECT count(*) FROM relations`).Scan(&after); err != nil || after != before {
		t.Fatal("read modified the published index")
	}
	input.Symbols = append([]model.SymbolFact(nil), input.Symbols...)
	input.Symbols[0].Name = "changed"
	if result, err := overview.NewProjectedGraph(ctx, input, codec, binding, guard, 100, 100, 65536); err == nil || result != nil {
		t.Fatal("mutated catalog bypassed published fact digest")
	}
	if _, err := db.Exec(`DELETE FROM active_generations WHERE generation_id=?`, request.Member.GenerationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE generations SET state='RETIRED' WHERE generation_id=?`, request.Member.GenerationID); err != nil {
		t.Fatal(err)
	}
	if page, err := graph.Page(ctx, "", binding.PrincipalDigest, guard, 10, 5, 8192); err == nil || len(page.Result.Data.Edges) != 0 {
		t.Fatal("retired generation leaked a graph page")
	}
}

func TestReadPublishedGraphRejectsLimitsAndRevocation(t *testing.T) {
	ctx, store, _, original, _ := publishedGraphFixture(t, 37)
	tests := []struct {
		name string
		edit func(*PublishedGraphRequest)
		want error
	}{
		{"facts", func(r *PublishedGraphRequest) { r.MaxFacts = 10 }, ErrCaptureLimit},
		{"fact bytes", func(r *PublishedGraphRequest) { r.MaxFactBytes = 1024 }, ErrCaptureLimit},
		{"file bytes", func(r *PublishedGraphRequest) { r.MaxFileBytes = 2 }, ErrCaptureLimit},
		{"source bytes", func(r *PublishedGraphRequest) { r.MaxSourceBytes = 2 }, ErrCaptureLimit},
		{"missing generation", func(r *PublishedGraphRequest) { r.Member.GenerationID = "99999999-9999-4999-8999-999999999999" }, ErrSnapshotUnavailable},
		{"wrong snapshot", func(r *PublishedGraphRequest) { r.Member.SnapshotDigest = strings.Repeat("f", 64) }, ErrCaptureMismatch},
		{"wrong incarnation", func(r *PublishedGraphRequest) { r.Member.IndexIncarnation = "99999999-9999-4999-8999-999999999999" }, ErrInvalidStore},
		{"generation revoked", func(r *PublishedGraphRequest) {
			calls := 0
			r.AuthorizeGeneration = func(context.Context, string, string, string, int64) error {
				calls++
				if calls >= 2 {
					return errFixtureLease
				}
				return nil
			}
		}, errFixtureLease},
		{"path revoked", func(r *PublishedGraphRequest) {
			calls := 0
			r.AuthorizePath = func(context.Context, string, string, string, int64) error {
				calls++
				if calls >= 4 {
					return errFixtureLease
				}
				return nil
			}
		}, errFixtureLease},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := original
			test.edit(&r)
			got, err := store.ReadPublishedGraph(ctx, r)
			if got != nil || !errors.Is(err, test.want) {
				t.Fatalf("unsafe read: catalog=%v error=%v want=%v", got != nil, err, test.want)
			}
		})
	}
	if got, err := store.ReadPublishedGraph(context.Background(), original); got != nil || !errors.Is(err, ErrInvalidStore) {
		t.Fatal("accepted an unbounded context")
	}
}

func TestReadPublishedGraphRejectsCorruptRange(t *testing.T) {
	ctx, store, db, request, _ := publishedGraphFixture(t, 2)
	// Corrupt only this isolated fixture after publication, bypassing its normal
	// frozen-row trigger so the read-side integrity check is exercised.
	var trigger string
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='trigger' AND name='relations_update_frozen'`).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER relations_update_frozen`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE relations SET end_byte=999 WHERE relation_id='relation-000'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(trigger); err != nil {
		t.Fatal(err)
	}
	if result, err := store.ReadPublishedGraph(ctx, request); result != nil || !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("corrupt source range accepted: %v", err)
	}
}

func TestReadPublishedGraphChecksUnescapedFactText(t *testing.T) {
	ctx, store, db, request, _ := publishedGraphFixture(t, 1)
	var trigger string
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='trigger' AND name='relations_update_frozen'`).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TRIGGER relations_update_frozen`); err != nil {
		t.Fatal(err)
	}
	// The quoted fixture value is intentionally synthetic. JSON escaping can
	// hide it from a scanner that sees only the serialized representation.
	if _, err := db.Exec(`UPDATE relations SET reason=? WHERE relation_id='relation-000'`, `api_key = "abcdefghijklmnopqrstuvwx"`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(trigger); err != nil {
		t.Fatal(err)
	}
	if result, err := store.ReadPublishedGraph(ctx, request); result != nil || !errors.Is(err, policy.ErrSecretBlocked) {
		t.Fatalf("unescaped sensitive fact was accepted: %v", err)
	}
}

func TestReadPublishedGraphVerifierRechecksAuthority(t *testing.T) {
	ctx, store, _, request, _ := publishedGraphFixture(t, 1)
	allowed := true
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error {
		if !allowed {
			return errFixtureLease
		}
		return nil
	}
	result, err := store.ReadPublishedGraph(ctx, request)
	if err != nil || result.VerifyFacts(ctx, request.Member, result.digest) != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}
	wrong := request.Member
	wrong.RecordEpoch++
	if err := result.VerifyFacts(ctx, wrong, result.digest); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("wrong member accepted: %v", err)
	}
	if err := result.VerifyFacts(ctx, request.Member, strings.Repeat("f", 64)); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("wrong catalog digest accepted: %v", err)
	}
	if err := result.VerifyCatalog(ctx, request.Member); err != nil {
		t.Fatalf("unchanged catalog rejected: %v", err)
	}
	original := result.Symbols[0].Name
	result.Symbols[0].Name = "changed after reading"
	if err := result.VerifyCatalog(ctx, request.Member); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("changed materialization accepted: %v", err)
	}
	result.Symbols[0].Name = original
	allowed = false
	if err := result.VerifyFacts(ctx, request.Member, result.digest); !errors.Is(err, errFixtureLease) {
		t.Fatalf("revoked generation accepted: %v", err)
	}
}

func TestReadPublishedGraphCapturedOnlyReportsUnknown(t *testing.T) {
	store, _, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishCapturedOnly(ctx, publishRequest(stage)); err != nil {
		t.Fatal(err)
	}
	manifest := stage.Capture.Snapshot.Manifest()
	member := model.ScopeMember{WorktreeID: manifest.WorktreeID, ProfileID: manifest.ProfileID, GenerationID: stage.Capture.GenerationID, SnapshotDigest: manifest.SnapshotDigest, PolicyDigest: manifest.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: store.identity.Incarnation, RecordsIncarnation: fixtureGeneration}
	r := PublishedGraphRequest{Member: member, VectorDigest: strings.Repeat("a", 64), AuthorizeGeneration: func(context.Context, string, string, string, int64) error { return nil }, AuthorizePath: stage.Capture.Authorize, Scanner: stage.Capture.Scanner, MaxManifestBytes: 8192, MaxFileBytes: 1024, MaxSourceBytes: 1024, MaxFactBytes: 65536, MaxFacts: 10}
	result, err := store.ReadPublishedGraph(ctx, r)
	if err != nil || result.Coverage.Status != "unknown" || len(result.Coverage.Gaps) != 1 || result.Coverage.Gaps[0].Code != "PROVIDER_NOT_RUN" || len(result.Diagnostics) != 1 || len(result.Relations) != 0 {
		t.Fatalf("captured-only coverage was misrepresented: %v", err)
	}
}
