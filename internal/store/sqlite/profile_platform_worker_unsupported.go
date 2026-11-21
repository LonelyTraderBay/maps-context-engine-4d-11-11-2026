//go:build !linux && !windows

package sqlite

import (
	"context"
	"io"
)

func runPlatformProtectedWorker(context.Context, []string, io.Writer) (bool, int) { return false, 0 }

func ProvisionProtectedWorktree(context.Context, string, string) (ProtectedWorktreeSummary, error) {
	return ProtectedWorktreeSummary{}, ErrStateUnsupported
}
