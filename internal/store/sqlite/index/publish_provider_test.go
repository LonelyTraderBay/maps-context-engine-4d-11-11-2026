package index

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func sealedProviderFixture(t *testing.T, complete bool) (*Store, StageProviderRequest) {
	t.Helper()
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if complete {
		stage.Batch.Packages[0].SemanticStatus = "complete_in_profile"
		target := "symbol"
		stage.Batch.Relations[0].TargetSymbolID = &target
		stage.Batch.Relations[0].Kind = "CALLS_DIRECT"
		stage.Batch.Relations[0].EvidenceKind = "type_resolved"
		stage.Batch.Relations[0].UnresolvedLabel = nil
	}
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	if complete {
		done.Status = "complete_in_supported_scope"
		done.Capabilities[0].Status = "supported_scope_complete"
		done.Gaps = []model.Gap{}
	}
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatal(err)
	}
	return store, stage
}

func publishProviderFixture(stage StageProviderRequest) PublishProviderRequest {
	return PublishProviderRequest{Capture: stage.Capture, Stream: stage.Stream, MaxFacts: 100, MaxFrameBytes: 65536,
		AuthorizeGeneration: func(context.Context, string, string, string, int64) error { return nil },
		Revalidate:          func(context.Context, *snapshot.Snapshot) error { return nil },
		VerifyProvider:      func(context.Context, *snapshot.Snapshot, model.ProviderDone) error { return nil }}
}

func TestPublishProviderCommitsFactsAndCoverage(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "complete"}[complete], func(t *testing.T) {
			store, stage := sealedProviderFixture(t, complete)
			request := publishProviderFixture(stage)
			checks := 0
			request.VerifyProvider = func(_ context.Context, captured *snapshot.Snapshot, done model.ProviderDone) error {
				checks++
				if captured != stage.Capture.Snapshot || done.InputDigest != captured.Manifest().SnapshotDigest {
					return ErrCaptureMismatch
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := store.PublishProvider(ctx, request); err != nil {
				t.Fatal(err)
			}
			var state, active, summary string
			if err := store.writer.QueryRow(`SELECT g.state,a.generation_id,c.summary_json FROM generations g JOIN active_generations a ON a.generation_id=g.generation_id JOIN capability_results c ON c.generation_id=g.generation_id WHERE g.generation_id=?`, fixtureGeneration).Scan(&state, &active, &summary); err != nil {
				t.Fatal(err)
			}
			coverage, err := model.DecodeCoverageJSON([]byte(summary), 65536)
			if err != nil {
				t.Fatal(err)
			}
			wantState, wantCoverage := "READY_PARTIAL", "partial"
			if complete {
				wantState, wantCoverage = "READY", "supported_scope_complete"
			}
			if state != wantState || active != fixtureGeneration || coverage.Status != wantCoverage || checks != 2 {
				t.Fatalf("bad publish state=%s active=%s coverage=%+v checks=%d", state, active, coverage, checks)
			}
		})
	}
}

func TestPublishProviderRejectsInvalidFactsAndCycles(t *testing.T) {
	for _, kind := range []string{"range", "module-range", "cycle", "metadata", "provider-proof"} {
		t.Run(kind, func(t *testing.T) {
			store, stage := sealedProviderFixture(t, false)
			request := publishProviderFixture(stage)
			switch kind {
			case "range":
				if _, err := store.writer.Exec(`UPDATE symbols SET end_byte=999 WHERE generation_id=?`, fixtureGeneration); err != nil {
					t.Fatal(err)
				}
			case "module-range":
				if _, err := store.writer.Exec(`INSERT INTO modules(generation_id,module_id,label,kind,plane,source_path,start_byte,end_byte) VALUES(?,'m','module','module','observed_static','main.go',0,999)`, fixtureGeneration); err != nil {
					t.Fatal(err)
				}
			case "metadata":
				if _, err := store.writer.Exec(`UPDATE packages SET input_digest=? WHERE generation_id=?`, strings.Repeat("z", 64), fixtureGeneration); err != nil {
					t.Fatal(err)
				}
			case "cycle":
				for _, id := range []string{"a", "b"} {
					if _, err := store.writer.Exec(`INSERT INTO modules(generation_id,module_id,label,kind,plane) VALUES(?,?,?,'module','observed_static')`, fixtureGeneration, id, id); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := store.writer.Exec(`UPDATE modules SET parent_module_id=CASE module_id WHEN 'a' THEN 'b' ELSE 'a' END WHERE generation_id=?`, fixtureGeneration); err != nil {
					t.Fatal(err)
				}
			case "provider-proof":
				request.VerifyProvider = func(context.Context, *snapshot.Snapshot, model.ProviderDone) error { return errFixtureLease }
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := store.PublishProvider(ctx, request)
			if kind == "provider-proof" && !errors.Is(err, errFixtureLease) || kind != "provider-proof" && err == nil {
				t.Fatalf("unsafe publish accepted: %v", err)
			}
			var active int
			if err := store.writer.QueryRow(`SELECT count(*) FROM active_generations`).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if active != 0 {
				t.Fatal("invalid publish changed active generation")
			}
		})
	}
}

func TestPublishProviderRequiresDiagnosticForParseError(t *testing.T) {
	store, stage := sealedProviderFixture(t, false)
	if _, err := store.writer.Exec(`UPDATE files SET status='parse_error',reason_code='PARSE_ERROR' WHERE generation_id=?`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := publishProviderFixture(stage)
	if err := store.PublishProvider(ctx, request); !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("parse error without diagnostic accepted: %v", err)
	}
	if _, err := store.writer.Exec(`INSERT INTO diagnostics(generation_id,diagnostic_id,path,package_key,code,severity,sanitized_message,scope_json) VALUES(?,'parse','main.go',NULL,'PARSE_ERROR','error','Parse failed.',?)`, fixtureGeneration, `{"scope_description":"workspace"}`); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishProvider(ctx, request); err != nil {
		t.Fatalf("documented partial parse error rejected: %v", err)
	}
}

func TestPublishProviderDoesNotHideUnresolvedRelationsInCompleteCoverage(t *testing.T) {
	store, stage := providerStageFixture(t)
	stage.Batch.Packages[0].SemanticStatus = "complete_in_profile"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	done := providerDoneFixture(stage)
	done.Sequence = 2
	done.Status = "complete_in_supported_scope"
	done.Capabilities[0].Status = "supported_scope_complete"
	done.Gaps = []model.Gap{}
	if err := store.SealProviderStage(ctx, sealRequest(stage, done)); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishProvider(ctx, publishProviderFixture(stage)); !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("unresolved relation was hidden: %v", err)
	}
}

func TestPublishProviderRollsBackPointerWhenPolicyChangesBeforeCommit(t *testing.T) {
	store, stage := sealedProviderFixture(t, false)
	old := insertPreviousActive(t, store.writer, StageFilesRequest{Capture: stage.Capture})
	request := publishProviderFixture(stage)
	checks := 0
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error {
		checks++
		if checks == 3 {
			return errFixtureLease
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.PublishProvider(ctx, request); !errors.Is(err, errFixtureLease) {
		t.Fatalf("policy revocation accepted: %v", err)
	}
	var active, state string
	var coverage int
	if err := store.writer.QueryRow(`SELECT generation_id FROM active_generations WHERE profile_id=?`, stage.Capture.Snapshot.Manifest().ProfileID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := store.writer.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := store.writer.QueryRow(`SELECT count(*) FROM capability_results WHERE generation_id=?`, fixtureGeneration).Scan(&coverage); err != nil {
		t.Fatal(err)
	}
	if active != old || state != "BUILDING" || coverage != 0 {
		t.Fatalf("partial commit active=%s state=%s coverage=%d", active, state, coverage)
	}
}
