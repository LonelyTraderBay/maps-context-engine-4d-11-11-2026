package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	mceMCP "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/mcp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// Dependencies are owned by the caller. An absent owner leaves the
// corresponding capability unavailable; no paths or principals are inferred.
type Dependencies struct {
	CPUs                int
	OpenConfig          func(context.Context, string) (ConfigOwner, func() error, error)
	OpenCore            OpenCore
	OpenRetainedCore    OpenCore
	OpenExplorer        OpenExplorer
	ExplorerAssets      fs.FS
	OpenAdmin           OpenAdmin
	ProbeConfig         func(context.Context, string) error
	SQLiteProbe         func(context.Context) (string, bool, error)
	LookupExecutable    func(string) (string, error)
	AddRepo             func(context.Context, string, string) (RegisteredRepo, error)
	Index               RunIndex
	LocalInventory      bool
	StoreGC             RunStoreGC
	StoreBackup         RunStoreBackup
	StoreRecoveryReport RunStoreRecoveryReport
	StoreRestore        RunStoreRestore
}

const help = `mce4d (development build)

Usage:
  mce4d version --json
  mce4d doctor --json
  mce4d [--runtime-config <approved-local-file>] config show|validate|set --input-json <file-or-dash>
  mce4d query status|locate --input-json <file-or-dash>
  mce4d mcp --worktree-id <id> --profile-id <id>
  mce4d explorer --worktree-id <id> --profile-id <id> --listen 127.0.0.1:0
  mce4d profile set --worktree-id <id> --input-json <file-or-dash>
  mce4d repo add --root <approved-absolute-path>
  mce4d policy set --input-json <file-or-dash>

Config input is a RuntimeConfigRequest with an action matching the subcommand.
Validate only checks the supplied request. On Linux, show can read the default
state configuration; other paths and config set require an authorized owner.
Query and MCP require an authorized bound service.
For an installed profile, prefix query with --worktree-id <id> --profile-id <id>.
Query context|expand|validate|rename-preview are recognized but unavailable.
Other product commands are unavailable until wired.
`

// Run is the local command boundary. stdin must be closeable to unblock Read
// on cancellation. JSON results go to stdout; diagnostics never include input.
func Run(ctx context.Context, args []string, stdin io.ReadCloser, stdout, stderr io.Writer, deps Dependencies) int {
	if ctx == nil || stdin == nil || stdout == nil || stderr == nil || deps.CPUs < 1 {
		return 5
	}
	diagnostic := func(message string, code int) int {
		_, _ = fmt.Fprintln(stderr, "mce4d: "+message)
		return code
	}
	if ctx.Err() != nil {
		return diagnostic("operation cancelled", 4)
	}
	if len(args) == 0 || len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		text := help
		if deps.OpenRetainedCore != nil {
			text = strings.Replace(text, "mce4d query status|locate --input-json", "mce4d query status|locate|context|expand|validate|rename-preview --input-json", 1)
			text = strings.Replace(text, "Query context|expand|validate|rename-preview are recognized but unavailable.", "Query context|expand|validate|rename-preview require their authorized retained-result owner.\nRename preview additionally requires an approved provider and verified typed inputs; it never applies edits.", 1)
		}
		if deps.Index != nil {
			repoLine := "  mce4d repo add --root <approved-absolute-path>\n"
			commands := "  mce4d index --worktree-id <id> --profile-id <id> [--full]\n  mce4d watch --worktree-id <id> --profile-id <id>\n"
			if deps.LocalInventory {
				text += "Index/watch capture approved files in a supported local SHA-1 repository. Watch runs in the foreground.\n"
			} else {
				text += "Index/watch run in the foreground and require a policy-pinned Git executable.\n"
			}
			text = strings.Replace(text, repoLine, repoLine+commands, 1)
			text += "Index progress goes to stderr. Source is read-only.\n"
		}
		if deps.StoreGC != nil {
			text += "  mce4d store gc --worktree-id <id> --profile-id <id> [--report-only | --apply --confirm]\nGC defaults to a read-only report; each applied generation is a separate transaction.\n"
		}
		if deps.StoreBackup != nil {
			text += "  mce4d store backup --target catalog --output <approved-new-private-directory>\n  mce4d store backup --target worktree --worktree-id <id> --output <approved-new-private-directory>\nBackup writes fresh artifacts below the selected store's private backups directory.\n"
		}
		if deps.StoreRecoveryReport != nil {
			text += "  mce4d store recover --target catalog [--report-only]\n  mce4d store recover --target worktree --worktree-id <id> [--report-only]\nRecovery defaults to a metadata report; it does not verify or change database files.\n"
		}
		if deps.StoreRestore != nil {
			text += "  mce4d store restore --manifest <approved-local-file> --confirm --wait-ms <1..30000>\n  mce4d store recover --target catalog|worktree [--worktree-id <id>] --apply --confirm --wait-ms <1..30000>\nRestore and recovery apply require the installed profile administrator.\n"
		}
		if n, err := io.WriteString(stdout, text); err != nil || n != len(text) {
			return diagnostic("output unavailable", 5)
		}
		return 0
	}
	configPath := ""
	if len(args) > 0 && args[0] == "--runtime-config" {
		if len(args) < 3 || !filepath.IsAbs(args[1]) || filepath.Clean(args[1]) != args[1] {
			return diagnostic("invalid runtime configuration path", 2)
		}
		configPath, args = args[1], args[2:]
	}
	if len(args) > 0 && args[0] == "--worktree-id" {
		if len(args) < 5 || args[2] != "--profile-id" || args[4] != "query" {
			return diagnostic("query binding requires worktree and profile IDs", 2)
		}
		worktree, profile := args[1], args[3]
		scope := model.ScopeRequest{WorktreeID: &worktree, ProfileID: &profile}
		if mode, err := scope.SelectionMode(1); err != nil || mode != "single" {
			return diagnostic("invalid query binding", 2)
		}
		bind := func(open OpenCore) OpenCore {
			if open == nil {
				return nil
			}
			return func(ctx context.Context, config, askedWorktree, askedProfile string) (*app.CoreService, func() error, error) {
				if askedWorktree != "" && askedWorktree != worktree || askedProfile != "" && askedProfile != profile {
					return nil, nil, ErrBindingDenied
				}
				return open(ctx, config, worktree, profile)
			}
		}
		deps.OpenCore, deps.OpenRetainedCore = bind(deps.OpenCore), bind(deps.OpenRetainedCore)
		args = args[4:]
	}
	if len(args) == 2 && args[0] == "version" && args[1] == "--json" {
		return writeJSON(stdout, stderr, map[string]any{"name": "mce4d", "version": "0.0.0-dev", "schema_version": 1, "release_status": "development"})
	}
	if len(args) == 2 && args[0] == "doctor" && args[1] == "--json" {
		return writeJSON(stdout, stderr, doctor(ctx, deps, configPath))
	}
	if len(args) > 0 && args[0] == "explorer" {
		return runExplorer(ctx, args[1:], configPath, stdout, stderr, deps)
	}
	if len(args) > 0 && args[0] == "store" {
		if len(args) > 1 && (args[1] == "restore" || args[1] == "recover" && containsFlag(args[2:], "--apply")) {
			return runStoreRestore(ctx, args, configPath, stdout, stderr, deps.StoreRestore)
		}
		if len(args) > 1 && args[1] == "recover" {
			return runStoreRecoveryReport(ctx, args, configPath, stdout, stderr, deps.StoreRecoveryReport)
		}
		return runStore(ctx, args, configPath, stdout, stderr, deps.StoreGC, deps.StoreBackup)
	}
	if len(args) == 4 && args[0] == "repo" && args[1] == "add" && args[2] == "--root" {
		if !filepath.IsAbs(args[3]) || filepath.Clean(args[3]) != args[3] {
			return diagnostic("invalid repository root", 2)
		}
		if deps.AddRepo == nil {
			return diagnostic("authorized registration owner unavailable", 3)
		}
		registered, err := deps.AddRepo(ctx, configPath, args[3])
		if err != nil {
			if ctx.Err() != nil {
				return diagnostic("registration cancelled", 4)
			}
			if errors.Is(err, app.ErrCoreStoreRecovery) {
				return diagnostic("STORE_RECOVERY_REQUIRED; inspect store recovery before retrying registration", 3)
			}
			return diagnostic("registration unavailable; inspect state before retrying", 3)
		}
		scope := model.ScopeRequest{WorktreeID: &registered.WorktreeID, ProfileID: &registered.ProfileID}
		if mode, err := scope.SelectionMode(1); err != nil || mode != "single" {
			return diagnostic("registration outcome requires reconciliation", 5)
		}
		if _, err := fmt.Fprintf(stdout, "Registered worktree %s\nProfile %s\n", registered.WorktreeID, registered.ProfileID); err != nil {
			return diagnostic("registration response unavailable; inspect state before retrying", 5)
		}
		return 0
	}
	if len(args) == 5 && args[0] == "mcp" && args[1] == "--worktree-id" && args[3] == "--profile-id" {
		scope := model.ScopeRequest{WorktreeID: &args[2], ProfileID: &args[4]}
		if mode, err := scope.SelectionMode(1); err != nil || mode != "single" {
			return diagnostic("invalid MCP binding", 2)
		}
		if deps.OpenCore == nil {
			return diagnostic("authorized bound service unavailable", 3)
		}
		service, release, err := deps.OpenCore(ctx, configPath, args[2], args[4])
		if err != nil || service == nil || release == nil {
			if release != nil {
				_ = release()
			}
			if errors.Is(err, ErrBindingDenied) {
				return diagnostic("authorized bound service unavailable", 3)
			}
			if errors.Is(err, app.ErrCoreStoreRecovery) {
				return diagnostic("STORE_RECOVERY_REQUIRED; use explicit store recovery", 3)
			}
			if errors.Is(err, fsruntime.ErrStoreMaintenance) {
				return diagnostic("STORE_MAINTENANCE", 4)
			}
			if errors.Is(err, app.ErrCoreLimit) {
				return diagnostic("RESOURCE_LIMIT; bound service exceeds configured capacity", 4)
			}
			return diagnostic("bound service unavailable", 5)
		}
		server, err := mceMCP.New(service)
		if err == nil {
			err = server.Run(ctx, stdin, noCloseWriter{stdout})
		}
		err = errors.Join(err, release())
		if ctx.Err() != nil {
			return diagnostic("operation cancelled", 4)
		}
		if err != nil {
			return diagnostic("MCP session unavailable", 5)
		}
		return 0
	}
	if len(args) > 0 && (args[0] == "index" || args[0] == "watch") {
		return runIndex(ctx, args, configPath, stderr, deps.Index)
	}
	if len(args) == 4 && args[0] == "query" && args[2] == "--input-json" {
		switch args[1] {
		case "status", "locate", "context", "expand", "validate", "rename-preview":
		default:
			return diagnostic("invalid query action", 2)
		}
		raw, err := readInput(ctx, args[3], stdin, 32768)
		if err != nil {
			if ctx.Err() != nil {
				return diagnostic("operation cancelled", 4)
			}
			return diagnostic("input is unavailable or exceeds its limit", 2)
		}
		result, err := executeQuery(ctx, args[1], raw, deps.CPUs, configPath, deps.OpenCore, deps.OpenRetainedCore)
		if err != nil {
			return diagnostic("query unavailable", 2)
		}
		output := append(result.JSON, '\n')
		if n, err := stdout.Write(output); err != nil || n != len(output) {
			return diagnostic("output unavailable", 5)
		}
		return result.ExitCode
	}
	kind, worktree, inputName := "", "", ""
	if len(args) == 6 && args[0] == "profile" && args[1] == "set" && args[2] == "--worktree-id" && args[4] == "--input-json" {
		kind, worktree, inputName = "profile", args[3], args[5]
	} else if len(args) == 4 && args[0] == "policy" && args[1] == "set" && args[2] == "--input-json" {
		kind, inputName = "policy", args[3]
	}
	if kind != "" {
		raw, err := readInput(ctx, inputName, stdin, 32768)
		if err != nil {
			if ctx.Err() != nil {
				return diagnostic("operation cancelled", 4)
			}
			return diagnostic("input is unavailable or exceeds its limit", 2)
		}
		result, err := ExecuteAdmin(ctx, kind, worktree, raw, deps.CPUs, configPath, deps.OpenAdmin)
		if err != nil {
			return diagnostic("administrative response unavailable", 5)
		}
		output := append(result.JSON, '\n')
		if n, err := stdout.Write(output); err != nil || n != len(output) {
			return diagnostic("output unavailable", 5)
		}
		return result.ExitCode
	}
	if len(args) != 4 || args[0] != "config" || args[2] != "--input-json" || args[3] == "" {
		return diagnostic("invalid or unavailable command; use --help", 2)
	}
	action := args[1]
	if action != "show" && action != "validate" && action != "set" {
		return diagnostic("invalid configuration action", 2)
	}
	raw, err := readInput(ctx, args[3], stdin, configRequestLimit)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return diagnostic("operation cancelled", 4)
		}
		return diagnostic("input is unavailable or exceeds its limit", 2)
	}
	var open OpenConfig
	if deps.OpenConfig != nil {
		open = func(ctx context.Context) (ConfigOwner, func() error, error) { return deps.OpenConfig(ctx, configPath) }
	}
	result, err := ExecuteConfig(ctx, action, raw, deps.CPUs, open)
	if err != nil {
		return diagnostic("configuration response unavailable", 5)
	}
	output := append(result.JSON, '\n')
	if n, err := stdout.Write(output); err != nil || n != len(output) {
		return diagnostic("output unavailable", 5)
	}
	return result.ExitCode
}

type noCloseWriter struct{ io.Writer }

func (noCloseWriter) Close() error { return nil }

func writeJSON(stdout, stderr io.Writer, value any) int {
	raw, err := json.Marshal(value)
	if err == nil {
		output := append(raw, '\n')
		var n int
		n, err = stdout.Write(output)
		if err == nil && n != len(output) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mce4d: output unavailable")
		return 5
	}
	return 0
}

// Doctor reports observations without opening a workspace, starting an index,
// running a provider or changing configuration. A supplied configuration
// probe may read its authorized file; unchecked capabilities remain explicit.
func doctor(ctx context.Context, deps Dependencies, configPath string) any {
	checks := map[string]string{"filesystem": "not_checked", "configuration": "not_checked", "sqlite_fts": "not_checked", "sqlite_version": "not_checked", "git": "not_checked", "gopls": "not_checked", "go": "not_checked", "context": contextStatus(ctx)}
	if deps.ProbeConfig != nil {
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := deps.ProbeConfig(bounded, configPath)
		if err == nil {
			err = bounded.Err()
		}
		cancel()
		switch {
		case err == nil:
			checks["configuration"] = "available_read_only"
		case errors.Is(err, context.Canceled):
			checks["configuration"] = "cancelled"
		case errors.Is(err, context.DeadlineExceeded):
			checks["configuration"] = "timed_out"
		case errors.Is(err, fsruntime.ErrConfigPending):
			checks["configuration"] = "recovery_required"
		default:
			checks["configuration"] = "unavailable_or_unverified"
		}
	}
	if deps.LookupExecutable != nil {
		for _, name := range []string{"git", "gopls", "go"} {
			path, err := deps.LookupExecutable(name)
			if err == nil && path != "" {
				checks[name] = "found_unverified"
			} else {
				checks[name] = "unavailable_or_unverified"
			}
		}
	}
	if deps.SQLiteProbe != nil {
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		version, fts, err := deps.SQLiteProbe(bounded)
		cancel()
		if err == nil && validVersion(version) {
			checks["sqlite_version"] = version
			if fts {
				checks["sqlite_fts"] = "available_in_memory"
			} else {
				checks["sqlite_fts"] = "unavailable_in_memory"
			}
		} else {
			checks["sqlite_fts"] = "probe_failed"
		}
	}
	return map[string]any{
		"schema_version": 1,
		"platform":       map[string]string{"os": runtime.GOOS, "architecture": runtime.GOARCH, "go_runtime": runtime.Version()},
		"checks":         checks,
	}
}

func validVersion(value string) bool {
	if len(value) < 1 || len(value) > 32 || !strings.Contains(value, ".") {
		return false
	}
	for _, r := range value {
		if r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func contextStatus(ctx context.Context) string {
	if ctx.Err() != nil {
		return "cancelled"
	}
	return "available"
}
