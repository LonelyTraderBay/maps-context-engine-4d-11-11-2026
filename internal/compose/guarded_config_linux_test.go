//go:build linux

package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestGuardedConfigSeparatesTargetAndRechecksBeforeCommit(t *testing.T) {
	base := t.TempDir()
	state, target := filepath.Join(base, "state"), filepath.Join(base, "target")
	for _, dir := range []string{state, target} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	canary := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(canary, []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stateRoot, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer stateRoot.Close()
	targetRoot, err := paths.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	defer targetRoot.Close()
	if _, err := NewGuardedConfigService(context.Background(), targetRoot, "runtime.json", []*paths.Root{targetRoot}, 4, func(context.Context) error { return nil }); !errors.Is(err, app.ErrAdminDenied) {
		t.Fatalf("overlapping target accepted: %v", err)
	}
	checks, failAt := 0, 0
	authorize := func(context.Context) error {
		checks++
		if failAt != 0 && checks >= failAt {
			return errors.New("private denial")
		}
		return nil
	}
	service, err := NewGuardedConfigService(context.Background(), stateRoot, "runtime.json", []*paths.Root{targetRoot}, 4, authorize)
	if err != nil {
		t.Fatal(err)
	}
	open := func(context.Context) (cli.ConfigOwner, func() error, error) {
		return service, func() error { return nil }, nil
	}
	raw := []byte(`{"action":"set","config":{"schema_version":1},"expected_digest":null}`)
	registry, _ := contracts.LoadRegistry()
	checks, failAt = 0, 3 // precheck, under-lock check, then pre-rename denial
	result, err := cli.ExecuteConfig(context.Background(), "set", raw, 4, open)
	if err != nil || result.ExitCode != 3 {
		t.Fatalf("revoked config: %v", err)
	}
	response, err := registry.DecodeResponse(contracts.CLI, "CLI config", result.JSON, 131072, 4)
	if err != nil || response.Error.Code != "ACCESS_DENIED" {
		t.Fatal("pre-commit revocation was not reported")
	}
	if _, err := os.Lstat(filepath.Join(state, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("revoked configuration was committed")
	}
	if _, err := os.Lstat(filepath.Join(state, ".runtime.json.pending")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("test-owned pending file was not cleaned")
	}
	checks, failAt = 0, 0
	result, err = cli.ExecuteConfig(context.Background(), "set", raw, 4, open)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("authorized config: %v", err)
	}
	if content, err := os.ReadFile(canary); err != nil || string(content) != "keep\n" {
		t.Fatal("target changed")
	}
}

func TestGuardedConfigReportsPostCommitRevocation(t *testing.T) {
	base := t.TempDir()
	state, target := filepath.Join(base, "state"), filepath.Join(base, "target")
	for _, dir := range []string{state, target} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	stateRoot, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer stateRoot.Close()
	targetRoot, err := paths.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	defer targetRoot.Close()
	checks, failAt := 0, 0
	service, err := NewGuardedConfigService(context.Background(), stateRoot, "runtime.json", []*paths.Root{targetRoot}, 4, func(context.Context) error {
		checks++
		if failAt != 0 && checks >= failAt {
			return errors.New("private denial")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checks, failAt = 0, 4 // denial after the successful rename
	raw := []byte(`{"action":"set","config":{"schema_version":1},"expected_digest":null}`)
	result, err := cli.ExecuteConfig(context.Background(), "set", raw, 4, func(context.Context) (cli.ConfigOwner, func() error, error) {
		return service, func() error { return nil }, nil
	})
	if err != nil || result.ExitCode != 3 {
		t.Fatal("post-commit revocation did not require reconciliation")
	}
	registry, _ := contracts.LoadRegistry()
	response, err := registry.DecodeResponse(contracts.CLI, "CLI config", result.JSON, 131072, 4)
	if err != nil || response.Error.Code != "STORE_RECOVERY_REQUIRED" {
		t.Fatal("applied outcome was hidden")
	}
	if _, err := os.Stat(filepath.Join(state, "runtime.json")); err != nil {
		t.Fatal("expected committed fixture config is missing")
	}
}
