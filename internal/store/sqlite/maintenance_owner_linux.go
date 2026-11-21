//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

// Single-operation offline owner. Normal native pools must have closed before
// this exclusive lease can be acquired. It opens no source SQL during startup;
// main/WAL byte observations are forbidden while its raw pool is alive.
// Construction is private: a command still needs its scoped authorizer and an
// approved backup/staging destination before any file can be written.
type quiescentOwner struct {
	profile             *protectedProfile
	domain              *stateDomain
	control             *paths.Root
	manifest            protectedWorktreeManifest
	digest              [32]byte
	catalogLease, lease *runtime.Lifecycle
	writer              *runtime.Writer
	operation, scope    string
	stores              []*nativeWALStore
	pool                *MaintenanceOwner
	authorize           func(context.Context) error
	closed              bool
}

func openQuiescentOwner(ctx context.Context, root, scope string, wait time.Duration, authorize func(context.Context) error) (_ *quiescentOwner, resultErr error) {
	return openQuiescentOperation(ctx, root, scope, wait, authorize, "", false)
}

// Recovery after a partial exchange needs the leases but cannot open SQL or
// authenticate a mix of old and new authority files.
func openQuiescentOperation(ctx context.Context, root, scope string, wait time.Duration, authorize func(context.Context) error, recovery string, leaseOnly bool) (_ *quiescentOwner, resultErr error) {
	if ctx == nil || authorize == nil || wait <= 0 || wait > time.Hour {
		return nil, ErrProfileInput
	}
	if scope != "catalog" {
		if _, err := profileUUIDBytes(scope); err != nil {
			return nil, err
		}
	}
	p, err := openProtectedProfile(ctx, root, "READY")
	if err != nil {
		return nil, err
	}
	o := &quiescentOwner{profile: p, scope: scope, authorize: authorize}
	if scope == "catalog" {
		p.maintenanceOperation = recovery
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, o.Close())
		}
	}()
	_, digest, err := trustedProfileExecutable(ctx)
	if err != nil || digest != p.manifest.BootstrapExecutableSHA256 {
		return nil, ErrProfileDenied
	}
	o.operation, err = profileUUID()
	if err != nil {
		return nil, err
	}
	if err := authorize(ctx); err != nil {
		return nil, err
	}
	if scope == "catalog" {
		o.domain = p.domain
	} else {
		// Acquire catalog first, retain its lifecycle while the scoped worktree
		// gate is held, but never hold a catalog SQL transaction while waiting.
		o.catalogLease, err = runtime.AcquireLifecycle(ctx, p.state, "catalog", ports.SharedLease, wait)
		if err != nil {
			return nil, err
		}
		o.control, err = p.root.OpenDirectory("control/worktrees/" + scope)
		if err != nil {
			return nil, err
		}
		o.manifest, o.digest, err = readWorktreeManifestForMaintenance(ctx, o.control, p.manifest.ProfileID, scope, recovery)
		if err != nil || o.manifest.Stage != "READY" {
			return nil, errors.Join(ErrProfileIncomplete, err)
		}
		o.domain, err = p.domain.Child(ctx, "worktrees/"+scope)
		if err != nil {
			return nil, err
		}
	}
	o.lease, err = runtime.AcquireLifecycle(ctx, o.domain.root, scope, ports.ExclusiveLease, wait)
	if err != nil {
		return nil, err
	}
	if scope == "catalog" {
		o.writer, err = runtime.AcquireCatalogMutation(ctx, o.lease, o.operation, wait)
	} else {
		o.writer, err = runtime.AcquireWriter(ctx, o.lease, o.operation, wait)
	}
	if err != nil {
		return nil, err
	}
	if leaseOnly {
		journalRoot := o.control
		if scope == "catalog" {
			journalRoot = p.maintenanceControl
		}
		journal, present, err := readMaintenanceJournal(ctx, journalRoot, p.manifest.ProfileID, scope)
		if err != nil || !present || journal.OperationID != recovery || journal.Phase == "VERIFIED" {
			return nil, errors.Join(ErrStoreRecoveryRequired, err)
		}
		return o, ctx.Err()
	}
	if scope == "catalog" {
		identity, err := p.identity(p.manifest.CatalogIncarnation)
		if err != nil {
			return nil, err
		}
		store := &nativeWALStore{domain: p.domain, journal: p.journalDomain, control: p.control, lease: o.lease, scope: scope, name: "catalog.sqlite", identity: identity, key: p.key, authorize: o.check}
		store.capture = func(ctx context.Context) (nativeWALCapture, error) { return o.capture(ctx, store) }
		o.stores = append(o.stores, store)
	} else {
		for _, item := range []struct{ kind, incarnation string }{{"index", o.manifest.IndexIncarnation}, {"records", o.manifest.RecordsIncarnation}} {
			journal, err := o.domain.Child(ctx, ".maintenance/"+item.kind)
			if err != nil {
				return nil, err
			}
			control, err := o.control.OpenDirectory(item.kind)
			if err != nil {
				return nil, errors.Join(err, journal.root.Close())
			}
			identity, key, err := worktreeStoreIdentity(p.manifest, scope, item.kind, item.incarnation, p.key)
			if err != nil {
				return nil, errors.Join(err, journal.root.Close(), control.Close())
			}
			store := &nativeWALStore{domain: o.domain, journal: journal, control: control, lease: o.lease, scope: scope, name: item.kind + ".sqlite", identity: identity, key: key, authorize: o.check}
			store.capture = func(ctx context.Context) (nativeWALCapture, error) { return o.capture(ctx, store) }
			o.stores = append(o.stores, store)
		}
	}
	if err := o.check(ctx); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *quiescentOwner) check(ctx context.Context) error {
	if o == nil || o.closed || o.profile == nil || o.lease == nil || o.writer == nil {
		return ErrProfileDenied
	}
	if err := o.profile.Check(ctx); err != nil {
		return err
	}
	if err := o.domain.Check(ctx); err != nil {
		return err
	}
	if err := o.lease.CheckRoot(ctx, o.domain.root, o.scope, ports.ExclusiveLease); err != nil {
		return err
	}
	if o.scope == "catalog" {
		if err := o.writer.CheckCatalogMutation(ctx, o.operation); err != nil {
			return err
		}
	} else {
		if err := o.catalogLease.Check(ctx, "catalog", ports.SharedLease); err != nil {
			return err
		}
		if err := o.writer.CheckWriter(ctx, o.scope, o.operation); err != nil {
			return err
		}
		m, digest, err := readWorktreeManifest(ctx, o.control, o.profile.manifest.ProfileID, o.scope)
		if err != nil || digest != o.digest || m.Stage != "READY" {
			return ErrProfileDenied
		}
	}
	return o.authorize(ctx)
}

func (o *quiescentOwner) capture(ctx context.Context, store *nativeWALStore) (nativeWALCapture, error) {
	if !o.owns(store) {
		return nativeWALCapture{}, ErrStateUnsafe
	}
	if o.pool != nil {
		return nativeWALCapture{}, runtime.ErrLifecycleInUse
	}
	if err := o.check(ctx); err != nil {
		return nativeWALCapture{}, err
	}
	return captureNativeWALImage(ctx, o.domain, o.lease, o.scope, store.name)
}

func (o *quiescentOwner) authenticatedImage(ctx context.Context, store *nativeWALStore) (walLogicalImage, error) {
	if !o.owns(store) {
		return walLogicalImage{}, ErrStateUnsafe
	}
	if o.pool != nil {
		return walLogicalImage{}, runtime.ErrLifecycleInUse
	}
	a, err := store.admit(ctx, false)
	if err != nil {
		return walLogicalImage{}, err
	}
	image := a.receipt.Image
	if err := a.close(); err != nil {
		return walLogicalImage{}, err
	}
	return image, nil
}

// No write/recovery/migration is permitted through this source pool. mode=ro
// is independent of query_only; schema validators may reset connection PRAGMAs.
func (o *quiescentOwner) openReadPool(ctx context.Context, store *nativeWALStore) error {
	if !o.owns(store) {
		return ErrStateUnsafe
	}
	if o.pool != nil {
		return runtime.ErrLifecycleInUse
	}
	if err := o.check(ctx); err != nil {
		return err
	}
	root, err := o.domain.root.CanonicalPath()
	if err != nil {
		return err
	}
	u, err := url.Parse(profileNativeDSN(filepath.Join(root, store.name)))
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	o.pool, err = OpenMaintenanceOwner(ctx, o.lease, db, o.scope)
	if err != nil {
		return errors.Join(err, db.Close())
	}
	if _, err := o.sourceMetadata(ctx, store); err != nil {
		return errors.Join(err, o.closeReadPool())
	}
	return nil
}

type backupSourceMetadata struct {
	Kind, WorktreeID, Incarnation   string
	SchemaVersion, MinReaderVersion int
	RecordEpoch                     int64
}

// Authenticate the exact recognized schema and identity before a backup API
// can copy bytes. This never initializes, migrates or down-migrates a source.
func (o *quiescentOwner) sourceMetadata(ctx context.Context, store *nativeWALStore) (out backupSourceMetadata, resultErr error) {
	if !o.owns(store) || o.pool == nil {
		return out, ErrStateUnsafe
	}
	if err := o.check(ctx); err != nil {
		return out, err
	}
	db := o.pool.db
	expectedVersion := 1
	var expectedIncarnation string
	switch store.name {
	case "catalog.sqlite":
		if _, err := catalog.OpenExistingLeased(ctx, db, o.lease); err != nil {
			return out, err
		}
		out.Kind = "catalog"
		expectedIncarnation = o.profile.manifest.CatalogIncarnation
		if err := db.QueryRowContext(ctx, `SELECT incarnation FROM catalog_identity WHERE singleton=1`).Scan(&out.Incarnation); err != nil {
			return out, err
		}
	case "index.sqlite":
		s, err := index.Open(ctx, db, db, o.scope, o.lease)
		if err != nil {
			return out, err
		}
		identity, err := s.Identity(ctx)
		if err != nil {
			return out, err
		}
		out.Kind, out.WorktreeID, out.Incarnation = "index", identity.WorktreeID, identity.Incarnation
		expectedVersion = 2
		expectedIncarnation = o.manifest.IndexIncarnation
	case "records.sqlite":
		if _, err := records.Open(ctx, db, o.scope, o.lease); err != nil {
			return out, err
		}
		out.Kind = "records"
		expectedIncarnation = o.manifest.RecordsIncarnation
		if err := db.QueryRowContext(ctx, `SELECT worktree_id,incarnation,record_epoch FROM records_meta WHERE singleton=1`).Scan(&out.WorktreeID, &out.Incarnation, &out.RecordEpoch); err != nil {
			return out, err
		}
	default:
		return out, ErrStateUnsafe
	}
	if err := db.QueryRowContext(ctx, `SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1`).Scan(&out.SchemaVersion, &out.MinReaderVersion); err != nil {
		return out, err
	}
	if out.SchemaVersion != expectedVersion || out.MinReaderVersion != expectedVersion || out.Incarnation != expectedIncarnation || out.RecordEpoch < 0 || out.RecordEpoch > 9007199254740991 || out.Kind != "catalog" && out.WorktreeID != o.scope {
		return backupSourceMetadata{}, ErrStateUnsafe
	}
	if err := o.check(ctx); err != nil {
		return backupSourceMetadata{}, err
	}
	return out, nil
}

func (o *quiescentOwner) owns(store *nativeWALStore) bool {
	if o == nil || o.closed || store == nil {
		return false
	}
	for _, current := range o.stores {
		if current == store {
			return true
		}
	}
	return false
}

func (o *quiescentOwner) closeReadPool() error {
	if o.pool == nil {
		return nil
	}
	if err := o.pool.Close(); err != nil {
		return err
	}
	o.pool = nil
	return nil
}

func (o *quiescentOwner) Close() error {
	if o == nil || o.closed {
		return ErrOwnerClosed
	}
	if err := o.closeReadPool(); err != nil {
		return err
	}
	if o.writer != nil {
		if err := o.writer.Close(); err != nil {
			return err
		}
		o.writer = nil
	}
	if o.lease != nil {
		if err := o.lease.Close(); err != nil {
			return err
		}
		o.lease = nil
	}
	if o.catalogLease != nil {
		if err := o.catalogLease.Close(); err != nil {
			return err
		}
		o.catalogLease = nil
	}
	var resultErr error
	for _, store := range o.stores {
		clear(store.key[:])
		if o.scope != "catalog" {
			resultErr = errors.Join(resultErr, store.journal.root.Close(), store.control.Close())
		}
	}
	if o.scope != "catalog" {
		if o.domain != nil {
			resultErr = errors.Join(resultErr, o.domain.root.Close())
		}
		if o.control != nil {
			resultErr = errors.Join(resultErr, o.control.Close())
		}
	}
	if o.profile != nil {
		resultErr = errors.Join(resultErr, o.profile.Close())
	}
	o.closed = true
	return resultErr
}
