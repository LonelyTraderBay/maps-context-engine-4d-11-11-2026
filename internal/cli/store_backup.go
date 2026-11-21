package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type StoreBackupReport struct {
	OperationID    string `json:"operation_id"`
	Target         string `json:"target"`
	WorktreeID     string `json:"worktree_id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	FileCount      int    `json:"file_count"`
	TotalBytes     int64  `json:"total_bytes"`
}
type RunStoreBackup func(context.Context, string, string, string, string) (StoreBackupReport, error)

var ErrStoreSchemaTooNew = errors.New("store schema is newer than this build")

func runStoreBackup(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, run RunStoreBackup) int {
	diagnostic := func(message string, code int) int { _, _ = fmt.Fprintln(stderr, "mce4d: "+message); return code }
	if len(args) < 6 || args[2] != "--target" {
		return diagnostic("backup requires explicit target and output", 2)
	}
	target, worktree, output := args[3], "", ""
	switch {
	case len(args) == 6 && target == "catalog" && args[4] == "--output":
		output = args[5]
	case len(args) == 8 && target == "worktree" && args[4] == "--worktree-id" && args[6] == "--output":
		worktree, output = args[5], args[7]
		if !model.ValidUUID(worktree) {
			return diagnostic("invalid backup worktree", 2)
		}
	default:
		return diagnostic("invalid backup target or flags", 2)
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return diagnostic("backup requires an approved absolute output directory", 2)
	}
	if run == nil {
		return diagnostic("authorized backup owner unavailable", 3)
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := run(bounded, selected, target, worktree, output)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, runtime.ErrStoreMaintenance), errors.Is(err, runtime.ErrWorktreeBusy), errors.Is(err, app.ErrCoreLimit):
			return diagnostic("backup cancelled or limited; retain and inspect chosen output before retrying", 4)
		case errors.Is(err, ErrStoreSchemaTooNew):
			return diagnostic("source schema is newer than this build; no migration performed", 3)
		case errors.Is(err, app.ErrCoreStoreRecovery):
			return diagnostic("STORE_RECOVERY_REQUIRED; use explicit store recovery", 3)
		case errors.Is(err, os.ErrExist):
			return diagnostic("backup output already exists; existing artifacts retained", 3)
		case errors.Is(err, ErrBindingDenied), errors.Is(err, app.ErrCoreAuthentication):
			return diagnostic("backup scope is not authorized", 3)
		default:
			return diagnostic("backup unavailable; retain and inspect chosen output and source state", 5)
		}
	}
	expectedFiles := 2
	if target == "worktree" {
		expectedFiles = 3
	}
	if !model.ValidUUID(result.OperationID) || !model.ValidDigest(result.ManifestSHA256) || result.Target != target || result.WorktreeID != worktree || result.FileCount != expectedFiles || result.TotalBytes < 1 || result.TotalBytes > (8<<30)+(8<<20) {
		return diagnostic("backup outcome requires inspection", 5)
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw)+1 > int(defaultOutputLimit) {
		return diagnostic("backup output unavailable; inspect chosen directory before retrying", 5)
	}
	raw = append(raw, '\n')
	if n, err := stdout.Write(raw); err != nil || n != len(raw) {
		return diagnostic("backup output unavailable; inspect chosen directory before retrying", 5)
	}
	return 0
}
