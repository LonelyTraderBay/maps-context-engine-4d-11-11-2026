//go:build linux

package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/telemetry"
)

func TestTelemetryCaptureFixtureRedactionAndFailureIsolation(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	stateDir, sourceDir := t.TempDir(), t.TempDir()
	if os.Chmod(stateDir, 0700) != nil || os.Chmod(sourceDir, 0700) != nil {
		t.Fatal("chmod fixture")
	}
	name := "private-token-bearing-filename.go"
	content := []byte("package fixture\n// private source text\n")
	if err := os.WriteFile(filepath.Join(sourceDir, name), content, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := paths.OpenRoot(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	source, err := paths.OpenRoot(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lease, err := runtime.AcquireLifecycle(ctx, state, id, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	allowed := &atomic.Bool{}
	allowed.Store(true)
	sink, err := telemetry.NewFileSink(state, lease, id, func(context.Context) error {
		if !allowed.Load() {
			return errors.New("private token")
		}
		return nil
	}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := telemetry.NewCollector(sink, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	file, err := inventory.CaptureFile(ctx, source, scanner, name, 4096, func(context.Context, string) error { return nil })
	if err != nil || !bytes.Equal(file.Bytes(), content) {
		t.Fatalf("capture failed: %v", err)
	}
	metrics := telemetry.NewMetrics()
	metrics.Observe(telemetry.InventoryFiles, 1)
	metrics.Observe(telemetry.BytesHashed, int64(len(content)))
	if !collector.Submit(telemetry.Event{Level: "info", WorktreeID: id, Phase: "capture", Outcome: "ok", Counts: map[telemetry.Metric]int64{telemetry.InventoryFiles: 1, telemetry.BytesHashed: int64(len(content))}}) {
		t.Fatal("submission failed")
	}
	if err = collector.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if collector.Stats().Written != 1 || metrics.Snapshot().Measurements[telemetry.BytesHashed].Total != int64(len(content)) {
		t.Fatal("capture observation missing")
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	var logged []byte
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".jsonl") {
			raw, err := os.ReadFile(filepath.Join(stateDir, "logs", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			logged = append(logged, raw...)
		}
	}
	for _, forbidden := range [][]byte{[]byte(name), content, []byte(sourceDir), []byte("private"), []byte("token")} {
		if bytes.Contains(logged, forbidden) {
			t.Fatal("private value leaked into telemetry")
		}
	}

	// Logging failure is observed separately; a successful source capture stays successful.
	allowed.Store(false)
	failed, err := telemetry.NewCollector(sink, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	file, err = inventory.CaptureFile(ctx, source, scanner, name, 4096, func(context.Context, string) error { return nil })
	if err != nil || !bytes.Equal(file.Bytes(), content) {
		t.Fatal("logging affected capture")
	}
	failed.Submit(telemetry.Event{Level: "info", WorktreeID: id, Phase: "capture", Outcome: "ok"})
	if err = failed.Close(ctx); err != nil {
		t.Fatal(err)
	}
	message := failed.TakeDiagnostic()
	if failed.Stats().SinkFailures != 1 || message == "" || strings.Contains(message, "private") || strings.Contains(message, "token") {
		t.Fatal("unsanitized or missing degradation")
	}
}
