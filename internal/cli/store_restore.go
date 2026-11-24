package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type StoreRestoreReport struct {
	OperationID    string   `json:"operation_id"`
	Target         string   `json:"target"`
	WorktreeID     string   `json:"worktree_id"`
	Outcome        string   `json:"outcome"`
	CompletedSteps []string `json:"completed_steps"`
}

type RunStoreRestore func(context.Context, string, string, string, time.Duration) (StoreRestoreReport, error)
type RunStoreRebuildIndex func(context.Context, string, time.Duration) (StoreRestoreReport, error)

func containsFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func runStoreRestore(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, run RunStoreRestore, rebuildIndex RunStoreRebuildIndex) int {
	diagnostic := func(message string, code int) int { _, _ = fmt.Fprintln(stderr, "mce4d: "+message); return code }
	bad := func() int {
		return diagnostic("store replacement requires an explicit selection, --confirm and --wait-ms", 2)
	}
	if selected != "" || len(args) < 7 || args[len(args)-3] != "--confirm" || args[len(args)-2] != "--wait-ms" {
		return bad()
	}
	rawWait := args[len(args)-1]
	ms, err := strconv.Atoi(rawWait)
	if err != nil || ms < 1 || ms > 30000 || strconv.Itoa(ms) != rawWait {
		return bad()
	}
	manifest, target, worktree := "", "", ""
	rebuilding := args[1] == "rebuild-index"
	switch {
	case rebuilding && len(args) == 7 && args[2] == "--worktree-id" && model.ValidUUID(args[3]):
		target, worktree = "worktree", args[3]
		if rebuildIndex == nil {
			return diagnostic("authorized index rebuild unavailable", 3)
		}
		run = func(ctx context.Context, _, _, worktree string, wait time.Duration) (StoreRestoreReport, error) {
			return rebuildIndex(ctx, worktree, wait)
		}
	case args[1] == "restore" && len(args) == 7 && args[2] == "--manifest":
		manifest = args[3]
		if !filepath.IsAbs(manifest) || filepath.Clean(manifest) != manifest || filepath.Base(manifest) != "backup.json" {
			return bad()
		}
	case args[1] == "recover" && len(args) == 8 && args[2] == "--target" && args[3] == "catalog" && args[4] == "--apply":
		target = "catalog"
	case args[1] == "recover" && len(args) == 10 && args[2] == "--target" && args[3] == "worktree" && args[4] == "--worktree-id" && model.ValidUUID(args[5]) && args[6] == "--apply":
		target, worktree = "worktree", args[5]
	default:
		return bad()
	}
	if run == nil {
		return diagnostic("authorized offline store owner unavailable", 3)
	}
	bounded, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	result, err := run(bounded, manifest, target, worktree, time.Duration(ms)*time.Millisecond)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrCoreStoreRecovery):
			return diagnostic("STORE_RECOVERY_REQUIRED; inspect the scoped journal", 3)
		case errors.Is(err, runtime.ErrStoreMaintenance), errors.Is(err, runtime.ErrWorktreeBusy), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			return diagnostic("STORE_MAINTENANCE; operation busy or interrupted", 4)
		default:
			return diagnostic("restore or recovery unavailable; inspect the scoped journal before retrying", 3)
		}
	}
	if !model.ValidUUID(result.OperationID) || target != "" && (result.Target != target || result.WorktreeID != worktree) {
		return diagnostic("restore outcome requires inspection", 5)
	}
	if result.Target == "worktree" && !model.ValidUUID(result.WorktreeID) || result.Target == "catalog" && result.WorktreeID != "" {
		return diagnostic("restore outcome requires inspection", 5)
	}
	if result.Outcome != "restored" || rebuilding && len(result.CompletedSteps) != 4 ||
		!storeStepsMatch(result.Target, result.CompletedSteps, true, target == "worktree") {
		return diagnostic("restore outcome requires inspection", 5)
	}
	return writeJSON(stdout, stderr, result)
}
