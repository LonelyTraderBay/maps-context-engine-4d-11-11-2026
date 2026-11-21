package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type OpenExplorer func(context.Context, string, string, string) (explorer.Options, func() error, error)

func runExplorer(ctx context.Context, args []string, selected string, stdout, stderr io.Writer, deps Dependencies) int {
	diagnostic := func(message string, code int) int {
		_, _ = fmt.Fprintln(stderr, "mce4d: "+message)
		return code
	}
	if (len(args) != 4 && len(args) != 6) || args[0] != "--worktree-id" || !model.ValidUUID(args[1]) ||
		args[2] != "--profile-id" || !model.ValidDigest(args[3]) ||
		len(args) == 6 && (args[4] != "--listen" || args[5] != explorer.ListenAddr) {
		return diagnostic("Explorer requires one explicit worktree/profile and IPv4 loopback with an OS-assigned port", 2)
	}
	if deps.ExplorerAssets == nil {
		return diagnostic(explorer.ErrAssetsUnavailable.Error(), 3)
	}
	if deps.OpenExplorer == nil {
		return diagnostic("UNSUPPORTED_CAPABILITY: authorized Explorer index owner unavailable", 3)
	}
	options, release, err := deps.OpenExplorer(ctx, selected, args[1], args[3])
	if err != nil {
		if errors.Is(err, app.ErrCoreStoreRecovery) {
			return diagnostic("STORE_RECOVERY_REQUIRED", 3)
		}
		return diagnostic("Explorer could not open the authorized index binding", 3)
	}
	if release == nil {
		return diagnostic("Explorer binding owner did not supply a release function", 5)
	}
	options.Assets = deps.ExplorerAssets
	server, code, err := explorer.Listen(ctx, options)
	if err != nil {
		if release() != nil {
			return diagnostic("Explorer binding release failed", 5)
		}
		if errors.Is(err, explorer.ErrAssetsUnavailable) {
			return diagnostic(explorer.ErrAssetsUnavailable.Error(), 3)
		}
		return diagnostic("Explorer could not start on loopback", 3)
	}
	if _, err := fmt.Fprintf(stdout, "Explorer %s/\nBootstrap code: %s\n", server.Origin(), code); err != nil {
		_ = errors.Join(server.Close(), release())
		return diagnostic("Explorer console output unavailable", 5)
	}
	if err := errors.Join(server.Serve(ctx), release()); err != nil {
		return diagnostic("Explorer stopped before clean shutdown", 5)
	}
	return 0
}
