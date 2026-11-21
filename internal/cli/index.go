package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type IndexProgress struct {
	GenerationID, SnapshotDigest string
	Files                        int
	Unchanged, Typed             bool
}
type RunIndex func(context.Context, string, string, string, bool, bool, func(IndexProgress) error) error

var ErrIndexRecovery = errors.New("index operation outcome requires recovery")

func runIndex(ctx context.Context, args []string, config string, stderr io.Writer, run RunIndex) int {
	diagnostic := func(message string, code int) int { _, _ = fmt.Fprintln(stderr, "mce4d: "+message); return code }
	watch := args[0] == "watch"
	full := len(args) == 6 && args[5] == "--full" && !watch
	if len(args) != 5 && !full || len(args) < 5 || args[1] != "--worktree-id" || args[3] != "--profile-id" {
		return diagnostic("index/watch require an explicit worktree and profile", 2)
	}
	scope := model.ScopeRequest{WorktreeID: &args[2], ProfileID: &args[4]}
	if mode, err := scope.SelectionMode(1); err != nil || mode != "single" {
		return diagnostic("invalid index binding", 2)
	}
	if run == nil {
		return diagnostic("authorized index owner unavailable", 3)
	}
	progress := func(value IndexProgress) error {
		bound := model.ScopeRequest{WorktreeID: &value.GenerationID, ProfileID: &value.SnapshotDigest}
		if mode, err := bound.SelectionMode(1); err != nil || mode != "single" || value.Files < 0 || value.Files > 100000 {
			return errors.New("invalid index outcome")
		}
		action := "indexed"
		if value.Unchanged {
			action = "unchanged"
		}
		coverage := "syntax partial"
		if value.Typed {
			coverage = "typed partial"
		}
		_, err := fmt.Fprintf(stderr, "mce4d: %s %d files; generation %s; %s\n", action, value.Files, value.GenerationID, coverage)
		return err
	}
	err := run(ctx, config, args[2], args[4], full, watch, progress)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrIndexRecovery):
		return diagnostic("index outcome requires reconciliation before retrying", 3)
	case errors.Is(err, app.ErrCoreStoreRecovery):
		return diagnostic("STORE_RECOVERY_REQUIRED; use explicit store recovery", 3)
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		return diagnostic("index/watch cancelled", 4)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, runtime.ErrWorktreeBusy), errors.Is(err, runtime.ErrStoreMaintenance), errors.Is(err, app.ErrCoreLimit):
		return diagnostic("index/watch deadline, lock or capacity limit reached", 4)
	case errors.Is(err, app.ErrCoreUnsupported):
		return diagnostic("index capability or approved Git executable unavailable", 3)
	case errors.Is(err, ErrBindingDenied), errors.Is(err, app.ErrCoreAuthentication):
		return diagnostic("index scope is not authorized", 3)
	default:
		return diagnostic("index/watch unavailable; inspect state before retrying", 5)
	}
}
