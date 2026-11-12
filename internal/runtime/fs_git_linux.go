//go:build linux

package runtime

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// GitInventory fails closed until Git metadata, including gitdir, index and
// linked-worktree identities, can be proven inside the authorized scope.
// A fixed argv or an anchored worktree alone does not provide that proof.
func GitInventory(ctx context.Context, root *paths.Root, gitBinary string, maxBytes int) ([]byte, error) {
	if ctx == nil || !filepath.IsAbs(gitBinary) || maxBytes <= 0 || maxBytes > 64*1024*1024 {
		return nil, errors.New("invalid git inventory arguments")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("git inventory requires a deadline")
	}
	if err := root.Verify(); err != nil {
		return nil, err
	}
	return nil, ErrGitMetadataUnverified
}
