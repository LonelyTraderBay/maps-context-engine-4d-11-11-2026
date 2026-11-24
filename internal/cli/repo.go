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
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// RegisteredRepo is an internal adapter result for the existing human-readable
// repo add command. It is not a new core/MCP JSON operation or access grant.
type RegisteredRepo struct{ WorktreeID, ProfileID string }

func localAdminError(w io.Writer, err error) int {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, runtime.ErrStoreMaintenance), errors.Is(err, runtime.ErrWorktreeBusy), errors.Is(err, app.ErrCoreLimit):
		fmt.Fprintln(w, "mce4d: administrative operation cancelled, busy or limited")
		return 4
	case errors.Is(err, ErrIndexRecovery), errors.Is(err, app.ErrCoreStoreRecovery):
		fmt.Fprintln(w, "mce4d: reconcile the protected store before retrying")
		return 3
	case errors.Is(err, ErrBindingDenied), errors.Is(err, app.ErrAdminDenied),
		errors.Is(err, app.ErrAdminConflict), errors.Is(err, app.ErrAdminUnavailable),
		errors.Is(err, app.ErrAdminUnsupported), errors.Is(err, app.ErrCoreAuthentication):
		fmt.Fprintln(w, "mce4d: administrative capability unavailable, conflicting or denied")
		return 3
	default:
		fmt.Fprintln(w, "mce4d: administrative result unavailable")
		return 5
	}
}

func writeLocalAdminJSON(stdout, stderr io.Writer, value any) int {
	raw, err := json.Marshal(value)
	if err == nil && len(raw) < 4<<20 {
		raw = append(raw, '\n')
		if n, err := stdout.Write(raw); err == nil && n == len(raw) {
			return 0
		}
	}
	return localAdminError(stderr, app.ErrCoreBackend)
}

func runRepoAdmin(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, deps Dependencies) int {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if len(args) == 2 && args[0] == "list" && args[1] == "--json" {
		if deps.ListRepos == nil {
			return localAdminError(stderr, app.ErrAdminUnsupported)
		}
		entries, err := deps.ListRepos(ctx, selected)
		if err != nil {
			return localAdminError(stderr, err)
		}
		return writeLocalAdminJSON(stdout, stderr, struct {
			Worktrees []model.LocalWorktree `json:"worktrees"`
		}{entries})
	}
	if len(args) < 4 || args[1] != "--worktree-id" || !model.ValidUUID(args[2]) {
		fmt.Fprintln(stderr, "mce4d: invalid repo command or worktree ID")
		return 2
	}
	var root string
	var purge bool
	switch args[0] {
	case "rebind":
		if len(args) != 6 || args[3] != "--root" || !filepath.IsAbs(args[4]) ||
			filepath.Clean(args[4]) != args[4] || args[5] != "--confirm" {
			fmt.Fprintln(stderr, "mce4d: repo rebind requires a canonical absolute root and --confirm")
			return 2
		}
		root = args[4]
	case "remove":
		purge = len(args) == 5 && args[4] == "--purge-index"
		if len(args) != 4 && !purge || args[3] != "--confirm" {
			fmt.Fprintln(stderr, "mce4d: repo remove requires --confirm")
			return 2
		}
	default:
		fmt.Fprintln(stderr, "mce4d: invalid repo command")
		return 2
	}
	if deps.UpdateRepo == nil {
		return localAdminError(stderr, app.ErrAdminUnsupported)
	}
	revision, err := deps.UpdateRepo(ctx, selected, args[2], root, purge)
	if err != nil {
		return localAdminError(stderr, err)
	}
	return writeLocalAdminJSON(stdout, stderr, struct {
		WorktreeID      string `json:"worktree_id"`
		BindingRevision int64  `json:"binding_revision"`
	}{args[2], revision})
}

func runCacheAdmin(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) != 4 || args[1] != "--worktree-id" || !model.ValidUUID(args[2]) {
		fmt.Fprintln(stderr, "mce4d: invalid cache command or worktree ID")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if args[0] == "inspect" && args[3] == "--json" {
		if deps.InspectCache == nil {
			return localAdminError(stderr, app.ErrAdminUnsupported)
		}
		usage, err := deps.InspectCache(ctx, selected, args[2])
		if err != nil {
			return localAdminError(stderr, err)
		}
		return writeLocalAdminJSON(stdout, stderr, usage)
	}
	if args[0] != "gc" || args[3] != "--dry-run" && args[3] != "--confirm" {
		fmt.Fprintln(stderr, "mce4d: cache gc requires --dry-run or --confirm")
		return 2
	}
	if deps.CacheProfile == nil || deps.StoreGC == nil {
		return localAdminError(stderr, app.ErrAdminUnsupported)
	}
	profile, err := deps.CacheProfile(ctx, selected, args[2])
	if err != nil {
		return localAdminError(stderr, err)
	}
	mapped := []string{"store", "gc", "--worktree-id", args[2], "--profile-id", profile}
	if args[3] == "--confirm" {
		mapped = append(mapped, "--apply", "--confirm")
	}
	return runStore(ctx, mapped, selected, stdout, stderr, deps.StoreGC, nil)
}
