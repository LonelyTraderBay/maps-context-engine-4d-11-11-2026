//go:build linux

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func runDefaultConfig(t *testing.T, action, input string, prefix ...string) Result {
	t.Helper()
	args := append(append([]string{}, prefix...), "config", action, "--input-json", "-")
	var out, errOut bytes.Buffer
	deps := Dependencies{CPUs: 4, OpenConfig: func(ctx context.Context, configPath string) (ConfigOwner, func() error, error) {
		return OpenDefaultReadOnlyConfig(ctx, configPath, 4)
	}}
	code := Run(context.Background(), args, io.NopCloser(strings.NewReader(input)), &out, &errOut, deps)
	if errOut.Len() != 0 {
		t.Fatalf("unexpected diagnostic: %s", errOut.String())
	}
	return Result{JSON: bytes.TrimSpace(out.Bytes()), ExitCode: code}
}

func TestDefaultConfigShowAndSetDoNotCreateState(t *testing.T) {
	t.Setenv("MCE4D_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	result := runDefaultConfig(t, "show", `{"action":"show"}`)
	if result.ExitCode != 0 {
		t.Fatalf("default show failed: %s", result.JSON)
	}
	data := decodeResult(t, result).Data.(model.RuntimeConfigData)
	if data.Config.Core.MaxParseWorkers != 4 || data.RestartRequired {
		t.Fatalf("unexpected default config: %+v", data)
	}
	result = runDefaultConfig(t, "set", `{"action":"set","config":{"schema_version":1},"expected_digest":null}`)
	if result.ExitCode != 3 || decodeResult(t, result).Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("read-only owner accepted set: %s", result.JSON)
	}
	if _, err := os.Lstat(filepath.Join(home, ".local")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config commands created state: %v", err)
	}
}

func TestDefaultConfigUsesXDGAndPreservesTargetFiles(t *testing.T) {
	t.Setenv("MCE4D_HOME", "")
	home, target := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", target)
	state := filepath.Join(target, "mce4d")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema_version":1,"core":{"max_file_bytes":12345}}`)
	file := filepath.Join(state, "runtime.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(canary, []byte("fixture canary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result := runDefaultConfig(t, "show", `{"action":"show"}`)
	if result.ExitCode != 0 || decodeResult(t, result).Data.(model.RuntimeConfigData).Config.Core.MaxFileBytes != 12345 {
		t.Fatalf("XDG config not read: %s", result.JSON)
	}
	result = runDefaultConfig(t, "set", `{"action":"set","config":{"schema_version":1},"expected_digest":null}`)
	if result.ExitCode != 3 || decodeResult(t, result).Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatalf("read-only owner accepted set: %s", result.JSON)
	}
	actual, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("config content changed")
	}
	actual, err = os.ReadFile(canary)
	if err != nil || string(actual) != "fixture canary\n" {
		t.Fatal("target canary changed")
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 1 || entries[0].Name() != "runtime.json" {
		t.Fatal("config show or set created a file")
	}
}

func TestDefaultConfigRejectsUnsafeOrUnapprovedPaths(t *testing.T) {
	t.Setenv("MCE4D_HOME", "")
	for _, kind := range []string{"symlink_root", "symlink_file", "hardlink_file", "directory_permissions", "file_permissions", "pending", "invalid_json"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("HOME", base)
			t.Setenv("XDG_STATE_HOME", base)
			state, other := filepath.Join(base, "mce4d"), filepath.Join(base, "other")
			if err := os.Mkdir(other, 0700); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink_root" {
				if err := os.Symlink(other, state); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(state, 0700); err != nil {
				t.Fatal(err)
			}
			file, outside := filepath.Join(state, "runtime.json"), filepath.Join(other, "fixture.json")
			if err := os.WriteFile(outside, []byte(`{"schema_version":1}`), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink_file":
				if err := os.Symlink(outside, file); err != nil {
					t.Fatal(err)
				}
			case "hardlink_file":
				if err := os.Link(outside, file); err != nil {
					t.Fatal(err)
				}
			case "directory_permissions":
				if err := os.Chmod(state, 0755); err != nil {
					t.Fatal(err)
				}
			case "file_permissions":
				if err := os.WriteFile(file, []byte(`{"schema_version":1}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(file, 0644); err != nil {
					t.Fatal(err)
				}
			case "pending":
				if err := os.WriteFile(filepath.Join(state, ".runtime.json.pending"), []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid_json":
				if err := os.WriteFile(file, []byte(`{"schema_version":1,"unexpected":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := runDefaultConfig(t, "show", `{"action":"show"}`)
			if result.ExitCode == 0 {
				t.Fatalf("accepted %s", kind)
			}
			response := decodeResult(t, result)
			if kind == "pending" && (response.Error == nil || response.Error.Code != "STORE_RECOVERY_REQUIRED") {
				t.Fatalf("pending update was not reported: %s", result.JSON)
			}
			actual, err := os.ReadFile(outside)
			if err != nil || string(actual) != `{"schema_version":1}` {
				t.Fatal("outside fixture changed")
			}
		})
	}
	t.Run("relative_xdg", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_STATE_HOME", "relative")
		result := runDefaultConfig(t, "show", `{"action":"show"}`)
		if result.ExitCode != 3 || decodeResult(t, result).Error.Code != "UNSUPPORTED_CAPABILITY" {
			t.Fatalf("relative XDG path accepted: %s", result.JSON)
		}
	})
	t.Run("unapproved_override", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_STATE_HOME", "")
		result := runDefaultConfig(t, "show", `{"action":"show"}`, "--runtime-config", filepath.Join(t.TempDir(), "runtime.json"))
		if result.ExitCode != 3 || decodeResult(t, result).Error.Code != "UNSUPPORTED_CAPABILITY" {
			t.Fatalf("unapproved path accepted: %s", result.JSON)
		}
	})
}

func TestDefaultConfigOwnerHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	owner, release, err := OpenDefaultReadOnlyConfig(ctx, "", 4)
	if owner != nil || release != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled owner: %v", err)
	}
	store := &defaultReadOnlyConfigStore{state: filepath.Join(t.TempDir(), "missing")}
	if _, _, err := store.Read(ctx, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	if result, err := store.Set(ctx, nil, 4); !errors.Is(err, context.Canceled) || result.State != "not_applied" {
		t.Fatalf("cancelled set: %+v, %v", result, err)
	}
	if result, err := store.Set(context.Background(), nil, 4); !errors.Is(err, fsruntime.ErrConfigUnsupported) || result.State != "not_applied" {
		t.Fatalf("read-only store accepted set: %+v, %v", result, err)
	}
}
