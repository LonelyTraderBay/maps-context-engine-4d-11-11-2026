//go:build linux

package sqlite

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// ProtectedProfileRootFromEnvironment selects a path, never a principal or
// access grant. OpenProtectedState must authenticate the root-owned manifest,
// executable and separate OS engine identity before any mutable capability.
func ProtectedProfileRootFromEnvironment() (string, error) {
	state := os.Getenv("MCE4D_HOME")
	if state == "" {
		base := os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "state")
		}
		state = filepath.Join(base, "mce4d")
	}
	if !filepath.IsAbs(state) || filepath.Clean(state) != state {
		return "", ErrProfileInput
	}
	root := state
	for i := 0; i < 4; i++ {
		root = filepath.Dir(root)
	}
	if !validProfilePath(root) || profileStatePath(root) != state {
		return "", ErrStateUnsupported
	}
	return root, nil
}

// ProtectedState owns the installed state boundary. Borrowed handles remain
// valid only until Close; callers may not close them independently. This is
// OS installation authority, not authorization of a requested source scope.
type ProtectedState struct {
	profile *protectedProfile
	lease   *runtime.Lifecycle
	catalog *CatalogOwner
	native  *nativeWALStore
	closed  atomic.Bool
	closeMu sync.Mutex
}

func OpenProtectedState(ctx context.Context, root string) (_ *ProtectedState, resultErr error) {
	if HardenProtectedProfileWorker() != nil {
		return nil, ErrProfileDenied
	}
	p, err := openProtectedProfile(ctx, root, "READY")
	if err != nil {
		return nil, err
	}
	o := &ProtectedState{profile: p}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, o.Close())
		}
	}()
	_, digest, err := trustedProfileExecutable(ctx)
	if err != nil || digest != p.manifest.BootstrapExecutableSHA256 {
		return nil, ErrProfileDenied
	}
	if err := p.Check(ctx); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *ProtectedState) Check(ctx context.Context) error {
	if o == nil || o.closed.Load() || o.profile == nil {
		return ErrProfileDenied
	}
	return o.profile.Check(ctx)
}

// An early metadata fence, before catalog/config pools are opened for a bound
// command. Absence of an installed slot grants nothing; ordinary catalog and
// worktree authorization must still run. The actual store rechecks the gate
// throughout its lifetime after admission.
func (o *ProtectedState) CheckWorktreeMaintenance(ctx context.Context, worktree string) (resultErr error) {
	if err := o.Check(ctx); err != nil {
		return err
	}
	if _, err := profileUUIDBytes(worktree); err != nil {
		return err
	}
	control, err := o.profile.root.OpenDirectory("control/worktrees/" + worktree)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	return checkMaintenanceGate(ctx, control, o.profile.manifest.ProfileID, worktree, "")
}
func (o *ProtectedState) InstallationID() string { return o.profile.manifest.ProfileID }
func (o *ProtectedState) ExecutableDigest() string {
	return o.profile.manifest.BootstrapExecutableSHA256
}
func (o *ProtectedState) StateRoot() *paths.Root { return o.profile.state }
func (o *ProtectedState) TargetRoots() []*paths.Root {
	return append([]*paths.Root(nil), o.profile.targets...)
}
func (o *ProtectedState) TargetApproved(name string) bool {
	for _, root := range o.profile.targets {
		path, err := root.CanonicalPath()
		if err == nil && path == name {
			return true
		}
	}
	return false
}

func (o *ProtectedState) CursorKey(ctx context.Context) (out [32]byte, resultErr error) {
	if err := o.Check(ctx); err != nil {
		return out, err
	}
	f, err := o.profile.state.OpenRegular("cursor.key")
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	if stateOwner(f, false) != nil {
		return out, ErrProfileDenied
	}
	raw, err := io.ReadAll(io.LimitReader(f, 33))
	defer clear(raw)
	if err != nil || len(raw) != 32 {
		return out, ErrProfileDenied
	}
	copy(out[:], raw)
	return out, o.Check(ctx)
}

// OpenCatalog borrows a single native pool. It neither initializes nor adopts
// state, and all data transactions use the same protected receipt fence.
func (o *ProtectedState) OpenCatalog(ctx context.Context) (*catalog.Store, error) {
	if err := o.Check(ctx); err != nil {
		return nil, err
	}
	if o.catalog != nil {
		return o.catalog.Store()
	}
	if err := o.prepareCatalogAuthority(ctx); err != nil {
		return nil, err
	}
	db := o.native.pool(false, 1)
	var err error
	o.catalog, err = OpenCatalogOwner(ctx, o.lease, db)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return o.catalog.Store()
}

func (o *ProtectedState) prepareCatalogAuthority(ctx context.Context) error {
	if err := o.Check(ctx); err != nil {
		return err
	}
	if o.native != nil {
		return nil
	}
	lease, err := runtime.AcquireLifecycle(ctx, o.profile.state, "catalog", ports.SharedLease, 5*time.Second)
	if err != nil {
		return err
	}
	o.lease = lease
	p := o.profile
	identity, err := p.identity(p.manifest.CatalogIncarnation)
	if err != nil {
		return err
	}
	store := &nativeWALStore{domain: p.domain, journal: p.journalDomain, control: p.control, lease: lease, scope: "catalog", name: "catalog.sqlite", identity: identity, key: p.key, authorize: o.Check,
		capture: func(ctx context.Context) (nativeWALCapture, error) { return p.captureWorker(ctx, "", "catalog.sqlite") }}
	o.native = store
	return nil
}

// RecoverProtectedCatalogPending is an explicit library operation under the
// installed OS authority. It opens no SQLite connection and can settle only an
// unchanged last-ACK image; changed/unproven outcomes remain pending.
func RecoverProtectedCatalogPending(ctx context.Context, root string) (resultErr error) {
	state, err := OpenProtectedState(ctx, root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	if err := state.prepareCatalogAuthority(ctx); err != nil {
		return err
	}
	return state.native.recoverUnchangedPending(ctx)
}

func (o *ProtectedState) CatalogLease() *runtime.Lifecycle { return o.lease }

func (o *ProtectedState) Close() error {
	if o == nil {
		return ErrOwnerClosed
	}
	o.closeMu.Lock()
	defer o.closeMu.Unlock()
	if o.closed.Load() {
		return ErrOwnerClosed
	}
	if o.catalog != nil {
		if err := o.catalog.Close(); err != nil {
			return err
		}
		o.catalog = nil
	}
	if o.lease != nil {
		if err := o.lease.Close(); err != nil {
			return err
		}
		o.lease = nil
	}
	o.closed.Store(true)
	if o.native != nil {
		clear(o.native.key[:])
	}
	return o.profile.Close()
}
