//go:build !linux

package sqlite

import (
	"context"
	"encoding/json"
	"io"
	"strings"
)

func HardenProtectedProfileWorker() error { return ErrStateUnsupported }
func ProvisionProtectedProfile(context.Context, ProtectedProfileOptions) (ProtectedProfileSummary, error) {
	return ProtectedProfileSummary{}, ErrStateUnsupported
}
func ProtectedProfileWorker(context.Context, string, bool) (ProtectedProfileSummary, error) {
	return ProtectedProfileSummary{}, ErrStateUnsupported
}
func EncodeProfileSummary(w io.Writer, s ProtectedProfileSummary) error {
	return json.NewEncoder(w).Encode(s)
}

func RecoverProtectedWorktreePending(context.Context, string, string, func(context.Context) error) error {
	return ErrStateUnsupported
}

func RecoverProtectedCatalogPending(context.Context, string) error { return ErrStateUnsupported }

func RunProtectedWorker(ctx context.Context, args []string, output io.Writer) (bool, int) {
	if handled, code := runPlatformProtectedWorker(ctx, args, output); handled {
		return true, code
	}
	if len(args) > 0 && (args[0] == "__bootstrap-profile-v1" || args[0] == "__check-profile-v1" || args[0] == "__capture-catalog-v1" || strings.HasPrefix(args[0], "__bootstrap-worktree-v1:") || strings.HasPrefix(args[0], "__capture-store-v1:")) {
		return true, 3
	}
	return false, 0
}
