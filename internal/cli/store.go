package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// Local maintenance output; not a new HTTP or MCP operation.
type StoreGCGeneration struct {
	GenerationID  string           `json:"generation_id"`
	Applied       bool             `json:"applied"`
	AlreadyAbsent bool             `json:"already_absent"`
	Rows          map[string]int64 `json:"rows"`
	BlobBytes     int64            `json:"blob_bytes"`
}
type StoreGCReport struct {
	WorktreeID      string              `json:"worktree_id"`
	ProfileID       string              `json:"profile_id"`
	KeepGenerations int                 `json:"keep_generations"`
	DryRun          bool                `json:"dry_run"`
	Generations     []StoreGCGeneration `json:"generations"`
	More            bool                `json:"more"`
}
type RunStoreGC func(context.Context, string, string, string, bool) (StoreGCReport, error)

func runStore(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, run RunStoreGC, backup RunStoreBackup) int {
	diagnostic := func(message string, code int) int { _, _ = fmt.Fprintln(stderr, "mce4d: "+message); return code }
	if len(args) > 1 && args[1] == "backup" {
		return runStoreBackup(ctx, args, selected, stdout, stderr, backup)
	}
	if len(args) < 2 || args[1] != "gc" {
		return diagnostic("store capability unavailable", 3)
	}
	apply := len(args) == 8 && args[6] == "--apply" && args[7] == "--confirm"
	reportOnly := len(args) == 6 || len(args) == 7 && args[6] == "--report-only"
	if !apply && !reportOnly || len(args) < 6 || args[2] != "--worktree-id" || args[4] != "--profile-id" {
		return diagnostic("store gc requires explicit scope; apply also requires --confirm", 2)
	}
	scope := model.ScopeRequest{WorktreeID: &args[3], ProfileID: &args[5]}
	if mode, err := scope.SelectionMode(1); err != nil || mode != "single" {
		return diagnostic("invalid maintenance binding", 2)
	}
	if run == nil {
		return diagnostic("authorized GC owner unavailable", 3)
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := run(bounded, selected, args[3], args[5], apply)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, runtime.ErrWorktreeBusy), errors.Is(err, runtime.ErrStoreMaintenance), errors.Is(err, app.ErrCoreLimit):
			return diagnostic("GC cancelled or limited; inspect current plan before retrying", 4)
		case errors.Is(err, ErrIndexRecovery):
			return diagnostic("GC outcome requires reconciliation before retrying", 3)
		case errors.Is(err, app.ErrCoreStoreRecovery):
			return diagnostic("STORE_RECOVERY_REQUIRED; use explicit store recovery", 3)
		case errors.Is(err, ErrBindingDenied), errors.Is(err, app.ErrCoreAuthentication):
			return diagnostic("GC scope is not authorized", 3)
		default:
			return diagnostic("GC unavailable; inspect current plan and state before retrying", 5)
		}
	}
	if !validGCReport(result, args[3], args[5], apply) {
		return diagnostic("GC outcome requires reconciliation", 5)
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw)+1 > int(defaultOutputLimit) {
		return diagnostic("GC output unavailable; inspect current plan before retrying", 5)
	}
	raw = append(raw, '\n')
	if n, err := stdout.Write(raw); err != nil || n != len(raw) {
		return diagnostic("GC output unavailable; inspect current plan before retrying", 5)
	}
	return 0
}

func validGCReport(report StoreGCReport, worktree, profile string, apply bool) bool {
	if report.WorktreeID != worktree || report.ProfileID != profile || report.DryRun == apply || report.KeepGenerations < 2 || report.KeepGenerations > 128 || report.Generations == nil || len(report.Generations) > 16 {
		return false
	}
	seen := map[string]bool{}
	allowed := map[string]bool{"relations": true, "module_members": true, "search_documents": true, "diagnostics": true, "capability_results": true, "symbols": true, "modules": true, "files": true, "packages": true, "generations": true, "content_blobs": true}
	for _, generation := range report.Generations {
		id := model.ScopeRequest{WorktreeID: &generation.GenerationID, ProfileID: &profile}
		if mode, err := id.SelectionMode(1); err != nil || mode != "single" || seen[generation.GenerationID] || generation.Applied != (apply && !generation.AlreadyAbsent) || generation.Rows == nil || len(generation.Rows) > len(allowed) || generation.BlobBytes < 0 || generation.BlobBytes > 9007199254740991 {
			return false
		}
		seen[generation.GenerationID] = true
		if generation.AlreadyAbsent && (len(generation.Rows) != 0 || generation.BlobBytes != 0) || !generation.AlreadyAbsent && len(generation.Rows) != len(allowed) {
			return false
		}
		for key, count := range generation.Rows {
			if !allowed[key] || count < 0 || count > 9007199254740991 {
				return false
			}
		}
	}
	return true
}
