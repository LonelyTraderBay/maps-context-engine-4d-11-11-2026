//go:build linux

package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestSessionLimitReleasedWhenServerMissing(t *testing.T) {
	root, err := paths.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	scratch := t.TempDir()
	workspace := filepath.Join(scratch, "input")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	limiter, _ := NewSessionLimiter(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input := workspaceEditFixture(t)
	config := SessionConfig{ServerBinRoot: root, ExpectedSHA256: strings.Repeat("a", 64), Environment: runtime.GoEnvironmentInput{GoRoot: "/approved/go", ScratchRoot: scratch, WorkspaceRoot: workspace, GOOS: "linux", GOARCH: "amd64", BuildTags: []string{}}, Documents: input.Documents, Check: func(context.Context) error { return nil }, Limiter: limiter}
	_, err = StartSession(ctx, config)
	if !errors.Is(err, ErrLSPAuthority) {
		t.Fatalf("missing server accepted: %v", err)
	}
	permit, err := limiter.acquire(ctx)
	if err != nil {
		t.Fatalf("failed startup retained quota: %v", err)
	}
	permit.release()
	cancelled, stop := context.WithTimeout(context.Background(), time.Second)
	stop()
	if _, err := StartSession(cancelled, config); !errors.Is(err, context.Canceled) {
		t.Fatalf("startup did not preserve cancellation: %v", err)
	}
}
