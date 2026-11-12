//go:build linux

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func configServiceForTest(ctx context.Context, root *paths.Root, name string, cpus int) (*RuntimeConfigService, error) {
	store, err := fsruntime.NewRuntimeConfigStore(root, name)
	if err != nil {
		return nil, err
	}
	return NewRuntimeConfigService(ctx, store, cpus)
}

func TestRuntimeConfigShowAndValidateAreReadOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	config, digest, err := ValidateRuntimeConfig(context.Background(), []byte(`{"action":"validate","config":{"schema_version":1}}`), 4)
	if err != nil || config.Core.MaxParseWorkers != 4 || len(digest) != 64 {
		t.Fatalf("standalone validation: %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("standalone validation changed state: %v %v", entries, err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	service, err := configServiceForTest(context.Background(), root, "runtime.json", 4)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := service.Show(context.Background())
	if err != nil || shown.RestartRequired || shown.Config.Core.MaxParseWorkers != 4 {
		t.Fatalf("default config: %+v %v", shown, err)
	}
	validated, err := service.Validate(context.Background(), []byte(`{"action":"validate","config":{"schema_version":1,"core":{"max_file_bytes":12345}}}`))
	if err != nil || validated.Config.Core.MaxFileBytes != 12345 || !validated.RestartRequired {
		t.Fatalf("validated config: %+v %v", validated, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("show or validate created state: %v %v", entries, err)
	}
	if _, err := service.Validate(context.Background(), []byte(`{"action":"set","config":{"schema_version":1},"expected_digest":null}`)); !errors.Is(err, ErrInvalidConfigAction) {
		t.Fatalf("set request accepted as validation: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.json"), []byte(`{"schema_version":1,"core":{"max_file_bytes":12345}}`), 0600); err != nil {
		t.Fatal(err)
	}
	shown, err = service.Show(context.Background())
	if err != nil || !shown.RestartRequired || shown.Config.Core.MaxFileBytes != 12345 || shown.ConfigDigest != validated.ConfigDigest {
		t.Fatalf("changed config: %+v %v", shown, err)
	}
	newProcess, err := configServiceForTest(context.Background(), root, "runtime.json", 4)
	if err != nil {
		t.Fatal(err)
	}
	shown, err = newProcess.Show(context.Background())
	if err != nil || shown.RestartRequired {
		t.Fatalf("startup config considered stale: %+v %v", shown, err)
	}
}

func TestRuntimeConfigRejectsLinksAndInvalidContent(t *testing.T) {
	base := t.TempDir()
	state := filepath.Join(base, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.WriteFile(outside, []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Symlink(outside, filepath.Join(state, "runtime.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := configServiceForTest(context.Background(), root, "runtime.json", 4); err == nil {
		t.Fatal("config symlink was followed")
	}
	if err := os.Remove(filepath.Join(state, "runtime.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(state, "runtime.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := configServiceForTest(context.Background(), root, "runtime.json", 4); err == nil {
		t.Fatal("config hardlink was accepted")
	}
	if err := os.Remove(filepath.Join(state, "runtime.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "runtime.json"), []byte(`{"schema_version":1,"unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := configServiceForTest(context.Background(), root, "runtime.json", 4); err == nil {
		t.Fatal("invalid config was accepted")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != `{"schema_version":1}` {
		t.Fatalf("outside fixture changed: %v", err)
	}
}

func TestRuntimeConfigHonorsCancellationAndRootIdentity(t *testing.T) {
	base := t.TempDir()
	state := filepath.Join(base, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	service, err := configServiceForTest(context.Background(), root, "runtime.json", 4)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Show(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled show accepted: %v", err)
	}
	if err := os.Rename(state, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Show(context.Background()); !errors.Is(err, paths.ErrRootChanged) {
		t.Fatalf("changed state root accepted: %v", err)
	}
}
