package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type StoreRecoveryReport struct {
	Target              string   `json:"target"`
	WorktreeID          string   `json:"worktree_id"`
	ReportOnly          bool     `json:"report_only"`
	JournalPresent      bool     `json:"journal_present"`
	OperationID         string   `json:"operation_id"`
	Phase               string   `json:"phase"`
	Outcome             string   `json:"outcome"`
	CompletedSteps      []string `json:"completed_steps"`
	ActualStateVerified bool     `json:"actual_state_verified"`
}
type RunStoreRecoveryReport func(context.Context, string, string) (StoreRecoveryReport, error)

func runStoreRecoveryReport(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, run RunStoreRecoveryReport) int {
	diagnostic := func(message string, code int) int { _, _ = fmt.Fprintln(stderr, "mce4d: "+message); return code }
	target, worktree := "", ""
	if len(args) > 0 && args[len(args)-1] == "--report-only" {
		args = args[:len(args)-1]
	}
	switch {
	case len(args) == 4 && args[2] == "--target" && args[3] == "catalog":
		target = "catalog"
	case len(args) == 6 && args[2] == "--target" && args[3] == "worktree" && args[4] == "--worktree-id" && model.ValidUUID(args[5]):
		target = "worktree"
		worktree = args[5]
	default:
		return diagnostic("recovery report requires an explicit target and valid scope", 2)
	}
	// Reports inspect the installed protected profile, never a supplied runtime
	// configuration. Reject an override rather than silently ignoring it.
	if selected != "" {
		return diagnostic("recovery report does not accept a runtime configuration override", 2)
	}
	if run == nil {
		return diagnostic("protected recovery reporter unavailable", 3)
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := run(bounded, target, worktree)
	if err != nil {
		return diagnostic("protected recovery metadata unavailable; no recovery performed", 3)
	}
	if !validStoreRecoveryReport(result, target, worktree) {
		return diagnostic("recovery report outcome unavailable; no recovery performed", 5)
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 32768 {
		return diagnostic("recovery report output unavailable", 5)
	}
	raw = append(raw, '\n')
	if n, err := stdout.Write(raw); err != nil || n != len(raw) {
		return diagnostic("recovery report output unavailable", 5)
	}
	return 0
}

func validStoreRecoveryReport(r StoreRecoveryReport, target, worktree string) bool {
	if r.Target != target || r.WorktreeID != worktree || !r.ReportOnly || r.ActualStateVerified || r.CompletedSteps == nil {
		return false
	}
	if !r.JournalPresent {
		return r.OperationID == "" && r.Phase == "none" && r.Outcome == "" && len(r.CompletedSteps) == 0
	}
	if !model.ValidUUID(r.OperationID) {
		return false
	}
	switch r.Phase {
	case "NEEDS_RECOVERY", "PREPARED", "REPLACING", "VERIFYING", "VERIFIED":
	default:
		return false
	}
	if r.Phase == "VERIFIED" {
		return r.Outcome == "restored" && storeStepsMatch(target, r.CompletedSteps, true, true) ||
			r.Outcome == "aborted" && len(r.CompletedSteps) == 0
	}
	return r.Outcome == "" && storeStepsMatch(target, r.CompletedSteps, r.Phase == "VERIFYING", true)
}

func storeStepsMatch(target string, observed []string, complete, indexRollback bool) bool {
	match := func(expected ...string) bool {
		if len(observed) > len(expected) || complete && len(observed) != len(expected) {
			return false
		}
		for i, step := range observed {
			if step != expected[i] {
				return false
			}
		}
		return true
	}
	switch target {
	case "catalog":
		return match("catalog_main", "catalog_journal", "catalog_head", "profile_manifest")
	case "worktree":
		return match("index_main", "records_main", "index_journal", "records_journal", "index_head", "records_head", "worktree_manifest") ||
			indexRollback && match("index_main", "index_journal", "index_head", "worktree_manifest")
	default:
		return false
	}
}
