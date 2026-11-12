package index

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func providerBatchFixture() model.ProviderBatch {
	path, packageKey, label := "main.go", "pkg", "unresolved"
	return model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: fixtureJob, Sequence: 1,
		Packages:    []model.PackageFact{{PackageKey: packageKey, ImportPath: "example.invalid/pkg", ModuleKey: "module", Variant: "production", InputDigest: strings.Repeat("a", 64), SemanticStatus: "partial"}},
		Symbols:     []model.SymbolFact{{SymbolID: "symbol", Path: &path, PackageKey: &packageKey, Kind: "function", Name: "main", QualifiedName: "pkg.main", ScopeKey: "scope", Range: &model.ByteRange{StartByte: 0, EndByte: 7}, Signature: "func main()", Exported: false}},
		Relations:   []model.RelationFact{{RelationID: "relation", SourceSymbolID: "symbol", Kind: "UNRESOLVED_CALL", EvidenceKind: "unresolved", SitePath: path, Range: model.ByteRange{StartByte: 0, EndByte: 7}, Reason: "unresolved call", UnresolvedLabel: &label}},
		Diagnostics: []model.DiagnosticFact{}}
}

func providerStageFixture(t *testing.T) (*Store, StageProviderRequest) {
	t.Helper()
	store, _, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, stage); err != nil {
		t.Fatal(err)
	}
	stream, err := store.BeginProviderStage(ctx, stage.Capture, func(context.Context, string, string, string, int64) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return store, StageProviderRequest{Capture: stage.Capture, Stream: stream, Batch: providerBatchFixture(), MaxFrameBytes: 65536}
}

func TestStageProviderBatchBindsFactsAndSharedSequence(t *testing.T) {
	store, request := providerStageFixture(t)
	copyOfStream := request.Stream
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	next, err := store.StageProviderBatch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var packages, symbols, relations int
	if err := store.writer.QueryRow(`SELECT (SELECT count(*) FROM packages),(SELECT count(*) FROM symbols),(SELECT count(*) FROM relations)`).Scan(&packages, &symbols, &relations); err != nil {
		t.Fatal(err)
	}
	if packages != 1 || symbols != 1 || relations != 1 {
		t.Fatalf("missing facts %d %d %d", packages, symbols, relations)
	}
	request.Stream = copyOfStream
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("copied stream accepted repeated sequence: %v", err)
	}
	request.Stream = next
	request.Batch.Sequence = 2
	request.Batch.Packages = []model.PackageFact{}
	request.Batch.Symbols = []model.SymbolFact{}
	request.Batch.Relations = []model.RelationFact{}
	if _, err := store.StageProviderBatch(ctx, request); err != nil {
		t.Fatalf("next sequence rejected: %v", err)
	}
}

func TestStageProviderBatchRejectsBadRangeWithoutAdvancing(t *testing.T) {
	store, request := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request.Batch.Symbols[0].Range.EndByte = 999
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("invalid range accepted: %v", err)
	}
	var count int
	if err := store.writer.QueryRow(`SELECT count(*) FROM packages`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("invalid batch left facts")
	}
	request.Batch.Symbols[0].Range.EndByte = 7
	if _, err := store.StageProviderBatch(ctx, request); err != nil {
		t.Fatalf("valid retry failed: %v", err)
	}
}

func TestStageProviderBatchRollsBackOnRevocationAndSecret(t *testing.T) {
	for _, kind := range []string{"revocation", "secret"} {
		t.Run(kind, func(t *testing.T) {
			store, request := providerStageFixture(t)
			request.Batch.Relations = []model.RelationFact{}
			if kind == "revocation" {
				calls := 0
				request.Capture.Authorize = func(context.Context, string, string, string, int64) error {
					calls++
					if calls == 2 {
						return errFixtureLease
					}
					return nil
				}
			} else {
				request.Batch.Symbols[0].Signature = "api_key = abcdefghijklmnopqrstuvwxyz"
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := store.StageProviderBatch(ctx, request)
			if kind == "revocation" && !errors.Is(err, errFixtureLease) || kind == "secret" && !errors.Is(err, policy.ErrSecretBlocked) {
				t.Fatalf("unsafe facts accepted: %v", err)
			}
			var count int
			if err := store.writer.QueryRow(`SELECT count(*) FROM packages`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("rejected batch left package")
			}
		})
	}
}

func TestStageProviderBatchRejectsOversizedInputAndBadEndpoint(t *testing.T) {
	store, request := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request.Batch.Symbols[0].Signature = strings.Repeat("x", 65536)
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrCaptureLimit) {
		t.Fatalf("oversized frame accepted: %v", err)
	}
	request.Batch = providerBatchFixture()
	request.Batch.Relations[0].SourceSymbolID = "missing-symbol"
	if _, err := store.StageProviderBatch(ctx, request); err == nil {
		t.Fatal("dangling endpoint accepted")
	}
	var packages, symbols int
	if err := store.writer.QueryRow(`SELECT (SELECT count(*) FROM packages),(SELECT count(*) FROM symbols)`).Scan(&packages, &symbols); err != nil {
		t.Fatal(err)
	}
	if packages != 0 || symbols != 0 {
		t.Fatal("foreign key failure left partial facts")
	}
}

func TestStageProviderBatchRejectsMisleadingRelationEvidence(t *testing.T) {
	store, request := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	target := "symbol"
	request.Batch.Relations[0].TargetSymbolID = &target
	request.Batch.Relations[0].Kind = "MAY_DISPATCH_TO"
	request.Batch.Relations[0].EvidenceKind = "syntax_exact"
	request.Batch.Relations[0].UnresolvedLabel = nil
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("candidate relation with exact evidence accepted: %v", err)
	}
}

func TestStageProviderBatchBindsStreamToGeneration(t *testing.T) {
	store, request := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request.Capture.GenerationID = "99999999-9999-4999-8999-999999999999"
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("stream accepted another generation: %v", err)
	}
	request.Capture.GenerationID = fixtureGeneration
	if _, err := store.StageProviderBatch(ctx, request); err != nil {
		t.Fatalf("valid stream was advanced by rejected request: %v", err)
	}
}

func TestStageProviderBatchQuarantinesStreamAfterFinalizationFailure(t *testing.T) {
	store, request := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request.Capture.Writer = failingCloseWriter{}
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrAfterCommit) {
		t.Fatalf("committed finalization error hidden: %v", err)
	}
	request.Capture.Writer = &writerProbe{}
	request.Batch.Sequence = 2
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrCommitUnknown) {
		t.Fatalf("uncertain stream reused: %v", err)
	}
}

func TestProviderStageRegistryRejectsDuplicateUntilTerminal(t *testing.T) {
	store, request := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	if _, err := store.BeginProviderStage(ctx, request.Capture, authorize); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("duplicate stream accepted: %v", err)
	}
	if err := store.ReleaseProviderStage(ctx, request.Capture, request.Stream, authorize); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("building stream released: %v", err)
	}
	if _, err := store.writer.Exec(`UPDATE generations SET state='FAILED' WHERE generation_id=?`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseProviderStage(ctx, request.Capture, request.Stream, authorize); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StageProviderBatch(ctx, request); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("released stream accepted: %v", err)
	}
	store.streamsMu.Lock()
	remaining := len(store.streams)
	store.streamsMu.Unlock()
	if remaining != 0 {
		t.Fatal("terminal registry entry was not released")
	}
	if _, err := store.BeginProviderStage(ctx, request.Capture, authorize); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("terminal job accepted new stream: %v", err)
	}
}

func TestProviderStageReservationSpansStoreHandles(t *testing.T) {
	store, request := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	other, err := Open(ctx, store.readers, store.writer, store.identity.WorktreeID, store.lease)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, string, string, string, int64) error { return nil }
	if _, err := other.BeginProviderStage(ctx, request.Capture, authorize); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("another store started a duplicate stream: %v", err)
	}
	if err := other.PublishCapturedOnly(ctx, publishRequest(StageFilesRequest{Capture: request.Capture})); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("another store bypassed the active stream: %v", err)
	}
	if err := store.ReleaseProviderStage(ctx, request.Capture, request.Stream, authorize); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("released building stream: %v", err)
	}
}
