//go:build linux

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestConfigCLIUsesRealCASAndPreservesProcessConfig(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	canary := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(canary, []byte("do not edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	store, err := fsruntime.NewRuntimeConfigStore(root, "runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	service, err := app.NewRuntimeConfigService(context.Background(), store, 4)
	if err != nil {
		t.Fatal(err)
	}
	open := func(context.Context) (ConfigOwner, func() error, error) {
		return service, func() error { return nil }, nil
	}
	valid := []byte(`{"action":"validate","config":{"schema_version":1}}`)
	if result, err := ExecuteConfig(context.Background(), "validate", valid, 4, open); err != nil || result.ExitCode != 0 {
		t.Fatal("validation failed")
	}
	if files, err := os.ReadDir(state); err != nil || len(files) != 0 {
		t.Fatal("validation created a state file")
	}
	var wg sync.WaitGroup
	results := make(chan Result, 2)
	errs := make(chan error, 2)
	for _, limit := range []int{12345, 23456} {
		wg.Add(1)
		go func(limit int) {
			defer wg.Done()
			raw := []byte(fmt.Sprintf(`{"action":"set","config":{"schema_version":1,"core":{"max_file_bytes":%d}},"expected_digest":null}`, limit))
			result, err := ExecuteConfig(context.Background(), "set", raw, 4, open)
			results <- result
			errs <- err
		}(limit)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	success, conflict := 0, 0
	for result := range results {
		response := decodeResult(t, result)
		if result.ExitCode == 0 && response.Error == nil {
			success++
		} else if result.ExitCode == 3 && response.Error != nil && response.Error.Code == "CONFIG_CONFLICT" {
			conflict++
		} else {
			t.Fatalf("unexpected CAS result: %d %+v", result.ExitCode, response.Error)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("CAS did not select exactly one writer")
	}
	result, err := ExecuteConfig(context.Background(), "show", []byte(`{"action":"show"}`), 4, open)
	if err != nil || result.ExitCode != 0 || !decodeResult(t, result).Data.(model.RuntimeConfigData).RestartRequired {
		t.Fatal("running process silently changed its startup config")
	}
	if content, err := os.ReadFile(canary); err != nil || string(content) != "do not edit\n" {
		t.Fatal("target canary changed")
	}
}
