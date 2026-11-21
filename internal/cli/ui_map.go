package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/uimap"
)

type UIMapImportReport struct {
	BuildMapID string `json:"build_map_id"`
	BindingID  string `json:"binding_id,omitempty"`
	Status     string `json:"status"`
}

type ImportUIMap func(context.Context, string, string, string) (UIMapImportReport, error)

type UIMapWatchReport struct {
	BuildMapID   string `json:"build_map_id"`
	BindingID    string `json:"binding_id,omitempty"`
	Status       string `json:"status"`
	QueueAgeMS   int64  `json:"queue_age_ms"`
	WriterWaitMS int64  `json:"writer_wait_ms"`
	RebindMS     int64  `json:"rebind_ms"`
	Attempt      int    `json:"attempt"`
}

type WatchUIMap func(context.Context, string, string, string, string, func(UIMapWatchReport) error) error

func runUIMapWatch(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, run WatchUIMap) int {
	diagnostic := func(code int, message string) int {
		_, _ = fmt.Fprintln(stderr, "mce4d: "+message)
		return code
	}
	if len(args) != 8 || args[1] != "watch" || args[2] != "--worktree-id" || args[4] != "--profile-id" ||
		args[6] != "--directory" || !model.ValidUUID(args[3]) || !model.ValidDigest(args[5]) ||
		!filepath.IsAbs(args[7]) || filepath.Clean(args[7]) != args[7] || len(args[7]) > 4000 {
		return diagnostic(2, "INVALID_ARGUMENT")
	}
	if run == nil {
		return diagnostic(3, "UNSUPPORTED_CAPABILITY")
	}
	output := json.NewEncoder(stdout)
	err := run(ctx, selected, args[3], args[5], args[7], func(report UIMapWatchReport) error {
		return output.Encode(report)
	})
	if err == nil || errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled) {
		return 0
	}
	switch {
	case errors.Is(err, ErrBindingDenied):
		return diagnostic(3, "ACCESS_DENIED")
	case errors.Is(err, app.ErrCoreStoreRecovery), errors.Is(err, ErrIndexRecovery):
		return diagnostic(3, "STORE_RECOVERY_REQUIRED")
	case errors.Is(err, app.ErrCoreUnsupported):
		return diagnostic(3, "UNSUPPORTED_CAPABILITY")
	case errors.Is(err, uimap.ErrBuildArtifact):
		return diagnostic(2, "INVALID_ARGUMENT")
	default:
		return diagnostic(5, "UI map watch stopped; inspect the state before retrying")
	}
}

func runUIMapImport(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, run ImportUIMap) int {
	diagnostic := func(code int, message string) int {
		_, _ = fmt.Fprintln(stderr, "mce4d: "+message)
		return code
	}
	if len(args) != 6 || args[1] != "import" || args[2] != "--worktree-id" || args[4] != "--manifest" ||
		!model.ValidUUID(args[3]) || !filepath.IsAbs(args[5]) || filepath.Clean(args[5]) != args[5] || filepath.Ext(args[5]) != ".json" {
		return diagnostic(2, "INVALID_ARGUMENT")
	}
	if run == nil {
		return diagnostic(3, "UNSUPPORTED_CAPABILITY")
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	out, err := run(bounded, selected, args[3], args[5])
	switch {
	case errors.Is(err, app.ErrCoreStoreRecovery), errors.Is(err, ErrIndexRecovery):
		return diagnostic(3, "STORE_RECOVERY_REQUIRED")
	case errors.Is(err, ErrBindingDenied):
		return diagnostic(3, "ACCESS_DENIED")
	case errors.Is(err, app.ErrCoreUnsupported):
		return diagnostic(3, "UNSUPPORTED_CAPABILITY")
	case errors.Is(err, uimap.ErrBuildArtifact):
		return diagnostic(2, "INVALID_ARGUMENT")
	case bounded.Err() != nil:
		return diagnostic(4, "UI map import interrupted; inspect the state before retrying")
	case err != nil || !model.ValidDigest(out.BuildMapID) ||
		!(out.Status == "bound" && model.ValidDigest(out.BindingID) || out.Status == "imported_unbound" && out.BindingID == ""):
		return diagnostic(5, "UI map import outcome unavailable; inspect the state before retrying")
	default:
		return writeJSON(stdout, stderr, out)
	}
}
