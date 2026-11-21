//go:build linux

package lsp

import (
	"context"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func openSessionServer(root *paths.Root) (*os.File, error) { return root.OpenRegular("gopls") }

func startSessionProcess(ctx context.Context, config SessionConfig, executable *os.File, env []string) (*managedProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.WindowsContainerSID != "" {
		return nil, ErrLSPAuthority
	}
	return startManaged(executable, nil, config.Environment.WorkspaceRoot, env)
}
