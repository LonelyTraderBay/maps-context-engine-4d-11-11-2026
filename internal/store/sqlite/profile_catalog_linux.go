//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// ReadProtectedCatalog runs one authorized operation against an existing READY
// profile under an exclusive DB lifecycle. This compatibility API closes every
// native runtime pool before using ordinary checkpointed-main descriptors.
// It neither initializes nor repairs a store.
//
// The caller supplies an OS-authenticated authorizer, never a request boolean.
// The callback receives an unbound store and cannot mutate through its public
// methods. Do not publish results or retain the borrowed store in the callback;
// its pool closes before return. On any error the returned value is zero.
func ReadProtectedCatalog[T any](ctx context.Context, rootPath string, authorize func(context.Context) error, read func(*catalog.Store) (T, error)) (T, error) {
	return runProtectedCatalog(ctx, rootPath, false, authorize, read)
}

// MutateProtectedCatalog records PREPARE before invoking one catalog operation
// and persists an image receipt before returning its result. A failed callback
// is aborted only if the full acknowledged image is unchanged; otherwise the
// profile stays pending and requires reconciliation. Never retry an unknown
// outcome blindly. Authorize also runs inside each SQL write before commit.
func MutateProtectedCatalog[T any](ctx context.Context, rootPath string, authorize func(context.Context) error, mutate func(*catalog.Store) (T, error)) (T, error) {
	return runProtectedCatalog(ctx, rootPath, true, authorize, mutate)
}

func runProtectedCatalog[T any](ctx context.Context, rootPath string, writing bool, authorize func(context.Context) error, use func(*catalog.Store) (T, error)) (out T, resultErr error) {
	var zero T
	if ctx == nil || authorize == nil || use == nil {
		return zero, ErrProfileInput
	}
	outcomeError := func(err error) error {
		if writing {
			return errors.Join(catalog.ErrCatalogCommitUnknown, err)
		}
		return err
	}
	// Includes cleanup failures: callers never receive data paired with a
	// failed head, pool, writer or lifecycle finalization.
	defer func() {
		if resultErr != nil {
			out = zero
		}
	}()
	p, err := openProtectedProfile(ctx, rootPath, "READY")
	if err != nil {
		return zero, err
	}
	defer func() { resultErr = errors.Join(resultErr, p.Close()) }()
	_, executableDigest, err := trustedProfileExecutable(ctx)
	if err != nil || executableDigest != p.manifest.BootstrapExecutableSHA256 {
		return zero, ErrProfileDenied
	}
	check := func(ctx context.Context) error {
		if err := p.Check(ctx); err != nil {
			return err
		}
		return authorize(ctx)
	}
	if err := check(ctx); err != nil {
		return zero, err
	}
	operationID, err := profileUUID()
	if err != nil {
		return zero, err
	}
	lease, err := runtime.AcquireLifecycle(ctx, p.state, "catalog", ports.ExclusiveLease, 5*time.Second)
	if err != nil {
		return zero, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	mutation, err := runtime.AcquireCatalogMutation(ctx, lease, operationID, 5*time.Second)
	if err != nil {
		return zero, err
	}
	defer func() { resultErr = errors.Join(resultErr, mutation.Close()) }()
	log, head, err := p.openLog(ctx, p.manifest.CatalogIncarnation, nil)
	if err != nil {
		return zero, err
	}
	defer func() { resultErr = errors.Join(resultErr, log.Close(), head.Close()) }()
	receipt, err := log.Head(ctx)
	if err != nil {
		return zero, err
	}
	before, err := quiescedProfileCatalogImage(ctx, p, lease)
	if err != nil {
		return zero, err
	}
	if before != receipt.Image {
		return zero, ErrStateChanged
	}
	if err := check(ctx); err != nil {
		return zero, err
	}
	operation := sha256.Sum256([]byte("MCE4D-CATALOG-OP1\x00" + p.manifest.ProfileID + "\x00" + operationID))
	if writing {
		if err := log.Prepare(ctx, operation); err != nil {
			return zero, err
		}
	}
	// mode=rw requires the observed main to exist. The top-level privilege
	// boundary prevents a target principal from moving/aliasing it between
	// observation and SQLite open. No ordinary main descriptor remains open.
	db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(profileStatePath(rootPath), "catalog.sqlite")))
	if err != nil {
		return zero, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	owner, openErr := OpenCatalogOwner(ctx, lease, db)
	poolClosed := false
	closePool := func() error {
		var err error
		if owner != nil {
			err = owner.Close()
		} else {
			err = db.Close()
		}
		poolClosed = err == nil
		return err
	}
	defer func() {
		if !poolClosed {
			resultErr = errors.Join(resultErr, closePool())
		}
	}()
	var useErr error
	if openErr == nil {
		store, err := owner.Store()
		if err == nil && writing {
			store, err = store.WithAuthorizedMutation(ctx, mutation, operationID, check)
		}
		if err == nil {
			out, useErr = use(store)
		} else {
			useErr = err
		}
	} else {
		useErr = openErr
	}
	if closeErr := closePool(); closeErr != nil {
		return zero, outcomeError(errors.Join(useErr, closeErr))
	}
	// Every native handle in this operation is now closed. catalog.lock still
	// excludes all other protected catalog pools, including in this process.
	after, err := quiescedProfileCatalogImage(ctx, p, lease)
	if err != nil {
		return zero, outcomeError(errors.Join(useErr, err))
	}
	if err := check(ctx); err != nil {
		return zero, outcomeError(errors.Join(useErr, err))
	}
	if !writing {
		if after != before {
			return zero, ErrStateChanged
		}
		_, headErr := log.Head(ctx)
		return out, errors.Join(useErr, headErr)
	}
	if useErr != nil {
		if after != before {
			return zero, errors.Join(catalog.ErrCatalogCommitUnknown, useErr)
		}
		return zero, errors.Join(useErr, log.Abort(ctx, operation, after))
	}
	if err := log.CommitQuiesced(ctx, operation, after); err != nil {
		return zero, errors.Join(catalog.ErrCatalogCommitUnknown, err)
	}
	_, err = log.Head(ctx)
	return out, err
}

// Requires catalog.lock and no live native pool in this process or another
// protected owner. Unexpected WAL/journal data is a recovery condition, never
// permission to invoke writable SQLite recovery before authenticating bytes.
func quiescedProfileCatalogImage(ctx context.Context, p *protectedProfile, lease *runtime.Lifecycle) (image walLogicalImage, resultErr error) {
	observation, err := p.domain.Observe(ctx, lease, "catalog", "catalog.sqlite", walLogicalImageMaxBytes)
	if err != nil {
		return walLogicalImage{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, observation.Close())
		if resultErr != nil {
			image = walLogicalImage{}
		}
	}()
	for _, index := range []int{1, 3} { // WAL and rollback journal; SHM is not data.
		if file := observation.files[index].file; file != nil {
			info, err := file.Stat()
			if err != nil || info.Size() != 0 {
				return walLogicalImage{}, errWALAuthorityRecovery
			}
		}
	}
	image, err = profileMainImage(ctx, p.state, walLogicalImageMaxBytes)
	if err != nil {
		return walLogicalImage{}, err
	}
	if err := observation.Recheck(ctx); err != nil {
		return walLogicalImage{}, err
	}
	return image, nil
}
