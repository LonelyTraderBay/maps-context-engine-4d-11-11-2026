package index

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func TestValidateStoredProviderFactsRejectsCorruptionAndLimits(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	manifest := stage.Capture.Snapshot.Manifest()
	if err := validateStoredProviderFacts(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 10); err != nil {
		t.Fatal(err)
	}
	if err := validateStoredProviderFacts(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 2); !errors.Is(err, ErrCaptureLimit) {
		t.Fatalf("fact limit ignored: %v", err)
	}
	if _, err := store.writer.Exec(`UPDATE symbols SET signature='api_key = abcdefghijklmnopqrstuvwxyz' WHERE generation_id=?`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if err := validateStoredProviderFacts(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 10); !errors.Is(err, policy.ErrSecretBlocked) {
		t.Fatalf("secret in stored fact accepted: %v", err)
	}
}

func TestValidateStoredProviderFactsChecksLaterPages(t *testing.T) {
	store, _, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 130; i++ {
		id := fmt.Sprintf("package-%03d", i)
		if _, err := store.writer.Exec(`INSERT INTO packages(generation_id,package_key,import_path,module_key,variant,input_digest,semantic_status) VALUES(?,?,?,'module','production',?,'partial')`, fixtureGeneration, id, "example.invalid/"+id, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
	manifest := stage.Capture.Snapshot.Manifest()
	if err := validateStoredProviderFacts(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 200); err != nil {
		t.Fatal(err)
	}
	if _, err := store.writer.Exec(`UPDATE packages SET input_digest=? WHERE generation_id=? AND package_key='package-129'`, strings.Repeat("z", 64), fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if err := validateStoredProviderFacts(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, 200); !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("later invalid fact accepted: %v", err)
	}
}

func TestValidateStoredProviderFactsPagesCapturedAndExternalSymbols(t *testing.T) {
	store, stage := providerStageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stage.MaxFrameBytes = 1 << 20
	captured := stage.Batch.Symbols[0]
	for i := 0; i < 130; i++ {
		item := captured
		item.SymbolID = fmt.Sprintf("symbol-%03d", i)
		stage.Batch.Symbols = append(stage.Batch.Symbols, item)
	}
	external := captured
	external.SymbolID = "external-symbol"
	external.Kind, external.Path, external.Range = "external", nil, nil
	stage.Batch.Symbols = append(stage.Batch.Symbols, external)
	if _, err := store.StageProviderBatch(ctx, stage); err != nil {
		t.Fatal(err)
	}
	manifest := stage.Capture.Snapshot.Manifest()
	const factCount = 134 // one package, 132 symbols and one relation
	if err := validateStoredProviderFacts(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, factCount); err != nil {
		t.Fatal(err)
	}
	if err := validateStoredProviderFacts(ctx, store.writer, fixtureWorktree, stage.Capture, manifest, factCount-1); !errors.Is(err, ErrCaptureLimit) {
		t.Fatalf("fact accounting changed across symbol groups: %v", err)
	}
}
