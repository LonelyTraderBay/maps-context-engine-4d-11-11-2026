package telemetry

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

const DefaultLogFiles = 5
const DefaultLogFileBytes int64 = 10 << 20

// FileSink operates only beneath an authorized MCE4D worktree state root.
// Authorization is rechecked for every write; the callback must be bounded.
// The caller keeps root and lease open until the collector has stopped.
type FileSink struct {
	root      *paths.Root
	lease     *runtime.Lifecycle
	worktree  string
	authorize func(context.Context) error
	files     int
	bytes     int64
}

// Zero values select defaults. Configurations outside the tested bounded
// capability are rejected, never silently clamped.
func NewFileSink(root *paths.Root, lease *runtime.Lifecycle, worktree string, authorize func(context.Context) error, files int, bytes int64) (*FileSink, error) {
	if root == nil || lease == nil || !uuid(worktree) || authorize == nil || files < 0 || bytes < 0 {
		return nil, ErrInvalid
	}
	if files == 0 {
		files = DefaultLogFiles
	}
	if bytes == 0 {
		bytes = DefaultLogFileBytes
	}
	if files > 128 || bytes < 512 || bytes > 1<<30 {
		return nil, ErrUnsupported
	}
	return &FileSink{root: root, lease: lease, worktree: worktree, authorize: authorize, files: files, bytes: bytes}, nil
}
