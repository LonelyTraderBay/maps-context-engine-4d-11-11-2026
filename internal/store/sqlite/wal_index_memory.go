package sqlite

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var ErrWALIndexUnsafe = errors.New("anonymous WAL index identity or authority unsafe")
var ErrWALIndexInUse = errors.New("anonymous WAL index has active mappings or users")

// WALIndexPeer identifies a live process's descriptor for one anonymous
// WAL-index object. A trusted registry must bind it to the correct database
// incarnation and scope; this value alone is not authorization.
type WALIndexPeer struct {
	PID       int
	StartTime uint64
	FD        int
	Device    uint64
	Inode     uint64
	Size      int64
}

// WALIndexMemory is an experimental descriptor-scoped memory object. It is not
// a SQLite opener or a cross-process registry and must not be wired into the
// product until the VFS, registry and state-file containment are proved.
type WALIndexMemory struct {
	mu            sync.Mutex
	root          *paths.Root
	protected     []*paths.Root
	lease         *runtime.Lifecycle
	hold          *runtime.LifecycleHold
	scope         string
	authorize     func(context.Context) error
	peer          *WALIndexPeer
	authorizePeer func(context.Context, WALIndexPeer) error
	file          *os.File
	identity      WALIndexPeer
	registered    bool
	mappings      int
	users         int
	closed        bool
	closeErr      error
}

// WALIndexMapping tracks a mapping created by WALIndexMemory. Callers must
// stop using the returned bytes before Close and must serialize their use with
// Close. Mappings and descriptors created outside this API are not tracked.
type WALIndexMapping struct {
	mu     sync.Mutex
	owner  *WALIndexMemory
	data   []byte
	closed bool
}

// WALIndexUse is a cooperative attachment for a consumer that can retain an
// fd or mapping outside this API. It must be held until that consumer has
// closed every such resource; it does not track them automatically.
type WALIndexUse struct {
	mu    sync.Mutex
	owner *WALIndexMemory
}
