package sqlite

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var (
	ErrStateUnsafe      = errors.New("sqlite state path or authority unsafe")
	ErrStateChanged     = errors.New("sqlite state identity changed")
	ErrStateLimit       = errors.New("sqlite state file size limit exceeded")
	ErrStateUnsupported = errors.New("secure sqlite state observation unsupported")
)

type observedStateFile struct {
	name string
	file *os.File // Linux identity-only handle; nil means the sidecar was absent
}

// StateObservation pins identities of an existing database and its sidecars.
// It is not a database opener and does not prove that a separate sql.Open is
// atomic or no-follow. The trusted caller owns all roots and callbacks.
type StateObservation struct {
	mu        sync.Mutex
	root      *paths.Root
	protected []*paths.Root
	lease     *runtime.Lifecycle
	hold      *runtime.LifecycleHold
	scope     string
	maxBytes  int64
	authorize func(context.Context) error
	files     []observedStateFile
	closed    bool
}
