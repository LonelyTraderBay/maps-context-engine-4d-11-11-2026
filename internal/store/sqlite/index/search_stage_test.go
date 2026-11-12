package index

import (
	"context"
	"errors"
	"testing"
	"time"
)

func searchStageFixture(capture CaptureWrite) StageSearchDocumentsRequest {
	return StageSearchDocumentsRequest{Capture: capture, Paths: []string{"main.go"}, MaxBatchBytes: 1024,
		AuthorizeGeneration: func(context.Context, string, string, string, int64) error { return nil }}
}

func TestSearchDocumentsPublishOnlyCapturedContents(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(map[bool]string{false: "captured", true: "provider"}[provider], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if provider {
				store, stage := sealedProviderFixture(t, false)
				if err := store.StageSearchDocuments(ctx, searchStageFixture(stage.Capture)); err != nil {
					t.Fatal(err)
				}
				if err := store.PublishProvider(ctx, publishProviderFixture(stage)); err != nil {
					t.Fatal(err)
				}
				assertSearchDocument(t, store, "package main\n")
				return
			}
			store, _, stage := stageFixture(t)
			if err := store.StageFiles(ctx, stage); err != nil {
				t.Fatal(err)
			}
			if err := store.StageSearchDocuments(ctx, searchStageFixture(stage.Capture)); err != nil {
				t.Fatal(err)
			}
			if err := store.PublishCapturedOnly(ctx, publishRequest(stage)); err != nil {
				t.Fatal(err)
			}
			assertSearchDocument(t, store, "package main\n")
		})
	}
}

func assertSearchDocument(t *testing.T, store *Store, expected string) {
	t.Helper()
	var body string
	var hits int
	if err := store.writer.QueryRow(`SELECT body FROM search_documents WHERE generation_id=? AND item_id=?`, fixtureGeneration, documentID("main.go")).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != expected {
		t.Fatalf("wrong captured document %q", body)
	}
	if err := store.writer.QueryRow(`SELECT count(*) FROM search_fts JOIN search_documents d ON d.rowid=search_fts.rowid WHERE search_fts MATCH ? AND d.generation_id=?`, `"main"`, fixtureGeneration).Scan(&hits); err != nil || hits != 1 {
		t.Fatalf("FTS hits=%d err=%v", hits, err)
	}
}

func TestSearchDocumentStageRejectsInvalidOrRevokedInput(t *testing.T) {
	for _, kind := range []string{"not-staged", "wrong-path", "duplicate", "budget", "source-budget", "revoke"} {
		t.Run(kind, func(t *testing.T) {
			store, db, stage := stageFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if kind != "not-staged" {
				if err := store.StageFiles(ctx, stage); err != nil {
					t.Fatal(err)
				}
			}
			request := searchStageFixture(stage.Capture)
			switch kind {
			case "wrong-path":
				request.Paths[0] = "other.go"
			case "duplicate":
				request.Paths = []string{"main.go", "main.go"}
			case "budget":
				request.MaxBatchBytes = 2
			case "source-budget":
				request.Capture.MaxSourceBytes = 2
			case "revoke":
				calls := 0
				request.Capture.Authorize = func(context.Context, string, string, string, int64) error {
					calls++
					if calls == 2 {
						return errFixtureLease
					}
					return nil
				}
			}
			if err := store.StageSearchDocuments(ctx, request); err == nil {
				t.Fatal("invalid stage succeeded")
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM search_documents`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalid stage persisted %d: %v", count, err)
			}
		})
	}
}

func TestPublishRejectsTamperedSearchDocument(t *testing.T) {
	for _, column := range []string{"body", "name", "path", "kind", "item_id"} {
		t.Run(column, func(t *testing.T) {
			store, db, stage := stageFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := store.StageFiles(ctx, stage); err != nil {
				t.Fatal(err)
			}
			if err := store.StageSearchDocuments(ctx, searchStageFixture(stage.Capture)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE search_documents SET `+column+`=? WHERE generation_id=?`, "tampered", fixtureGeneration); err != nil {
				t.Fatal(err)
			}
			if err := store.PublishCapturedOnly(ctx, publishRequest(stage)); !errors.Is(err, ErrPublishIntegrity) {
				t.Fatalf("tampered document published: %v", err)
			}
			var active int
			if err := db.QueryRow(`SELECT count(*) FROM active_generations`).Scan(&active); err != nil || active != 0 {
				t.Fatalf("pointer changed: %d %v", active, err)
			}
		})
	}
}
