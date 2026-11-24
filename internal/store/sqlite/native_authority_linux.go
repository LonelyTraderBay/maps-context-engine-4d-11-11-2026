//go:build linux

package sqlite

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

var ErrNativeOutcomeUnknown = errors.New("native SQLite transaction requires reconciliation")
var errNativeOutcomeUnknown = ErrNativeOutcomeUnknown

// A trusted profile owner supplies the identity/key and a capture implemented
// in a separate process. The parent must NEVER open/close ordinary main/WAL
// descriptors while its native pools are alive (POSIX process-lock semantics).
// The capture runs under this store's short gate, after SQL commit/rollback,
// with auto-checkpoint disabled and native closes also excluded by the gate.
type nativeWALCapture struct {
	Image  walLogicalImage
	Prefix walCommitPrefixReceipt
}

type nativeWALStore struct {
	domain, journal *stateDomain
	control         *paths.Root
	lease           *runtime.Lifecycle
	scope, name     string
	identity        walAuthorityIdentity
	key             [32]byte
	authorize       func(context.Context) error
	capture         func(context.Context) (nativeWALCapture, error)
	quota           *cacheBudget // nil only for catalog/durable records or standalone fixtures
}

func (s *nativeWALStore) checkBoundary(ctx context.Context) error {
	if s == nil || s.domain == nil || s.journal == nil || s.control == nil || s.lease == nil || s.authorize == nil || s.capture == nil ||
		(s.scope == "catalog" && s.name != "catalog.sqlite" || s.scope != "catalog" && s.name != "index.sqlite" && s.name != "records.sqlite") ||
		checkStateDomains(ctx, s.domain, s.journal) != nil {
		return ErrStateUnsafe
	}
	if err := s.lease.CheckRoot(ctx, s.domain.root, s.scope, ports.SharedLease); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *nativeWALStore) check(ctx context.Context) error {
	if err := s.checkBoundary(ctx); err != nil {
		return err
	}
	return s.authorize(ctx)
}

func (s *nativeWALStore) closeGate(ctx context.Context) (*runtime.Lifecycle, error) {
	// Revocation denies new data/mutations but must not strand native handles.
	// Closing still requires the actual protected root and DB lifecycle.
	if err := s.checkBoundary(ctx); err != nil {
		return nil, err
	}
	return runtime.AcquireLifecycle(ctx, s.journal.root, s.scope, ports.ExclusiveLease, 5*time.Second)
}

// This metadata-directory lease is a short transaction/admission gate. It is
// distinct from the worktree DB lifecycle and from the long-lived job writer.
// It never grants a writer permission or upgrades a DB lifecycle lease.
func (s *nativeWALStore) gate(ctx context.Context) (*runtime.Lifecycle, error) {
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	return runtime.AcquireLifecycle(ctx, s.journal.root, s.scope, ports.ExclusiveLease, 5*time.Second)
}

func (s *nativeWALStore) openLog(ctx context.Context, initialize *walLogicalImage) (*walAuthorityLog, *walHeadAuthority, error) {
	if err := s.check(ctx); err != nil {
		return nil, nil, err
	}
	controlPath, err := s.control.CanonicalPath()
	if err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(controlPath, walHeadName), os.O_RDWR|os.O_APPEND|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	protected := append(append([]*paths.Root(nil), s.domain.protected...), s.domain.root)
	head, err := openWALHeadAuthority(ctx, s.control, protected, f, s.identity, s.key)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	journalPath, err := s.journal.root.CanonicalPath()
	if err != nil {
		return nil, nil, errors.Join(err, head.Close())
	}
	f, err = os.OpenFile(filepath.Join(journalPath, walAuthorityName), os.O_RDWR|os.O_APPEND|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, errors.Join(err, head.Close())
	}
	log, err := openAnchoredWALAuthorityLog(ctx, s.journal, f, head, s.identity, s.key, walAuthorityMaxBytes, initialize)
	if err != nil {
		return nil, nil, errors.Join(err, head.Close())
	}
	return log, head, nil
}

func (s *nativeWALStore) observed(ctx context.Context) (nativeWALCapture, error) {
	if err := s.check(ctx); err != nil {
		return nativeWALCapture{}, err
	}
	capture, err := s.capture(ctx)
	if err != nil {
		return nativeWALCapture{}, err
	}
	if !validAuthorityImage(capture.Image) || capture.Prefix != (walCommitPrefixReceipt{}) && !validAuthorityPrefix(capture.Prefix, capture.Image) {
		return nativeWALCapture{}, ErrStateUnsafe
	}
	if err := s.check(ctx); err != nil {
		return nativeWALCapture{}, err
	}
	return capture, nil
}

type nativeAdmission struct {
	store     *nativeWALStore
	gate      *runtime.Lifecycle
	log       *walAuthorityLog
	head      *walHeadAuthority
	receipt   walAuthorityReceipt
	operation [32]byte
	cacheGate *runtime.Lifecycle
}

func (s *nativeWALStore) admit(ctx context.Context, writing bool) (_ *nativeAdmission, resultErr error) {
	a := &nativeAdmission{store: s}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, a.close())
		}
	}()
	var err error
	if writing && s.quota != nil {
		a.cacheGate, err = s.quota.acquire(ctx)
		if err != nil {
			return nil, err
		}
	}
	a.gate, err = s.gate(ctx)
	if err != nil {
		return nil, err
	}
	a.log, a.head, err = s.openLog(ctx, nil)
	if err != nil {
		return nil, err
	}
	a.receipt, err = a.log.Head(ctx)
	if err != nil {
		return nil, err
	}
	observed, err := s.observed(ctx)
	if err != nil {
		return nil, err
	}
	// A legitimate checkpoint changes the physical WAL prefix, but never the
	// exact logical page image authenticated by the latest acknowledged head.
	if observed.Image != a.receipt.Image {
		return nil, ErrStateChanged
	}
	if writing {
		if s.quota != nil {
			if err := s.quota.check(ctx, a.receipt.Image, 0, 2*walAuthorityRecordSize); err != nil {
				return nil, err
			}
		}
		if _, err := rand.Read(a.operation[:]); err != nil {
			return nil, err
		}
		if err := a.log.Prepare(ctx, a.operation); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *nativeAdmission) finish(ctx context.Context, committed bool, sqlErr error) error {
	observed, err := a.store.observed(ctx)
	if err != nil {
		return errors.Join(errNativeOutcomeUnknown, sqlErr, err)
	}
	if !committed || sqlErr != nil {
		if observed.Image != a.receipt.Image {
			return errors.Join(errNativeOutcomeUnknown, sqlErr)
		}
		return errors.Join(sqlErr, a.log.Abort(ctx, a.operation, observed.Image))
	}
	if observed.Prefix == (walCommitPrefixReceipt{}) {
		err = a.log.CommitQuiesced(ctx, a.operation, observed.Image)
	} else {
		err = a.log.Commit(ctx, a.operation, observed.Image, observed.Prefix)
	}
	if err != nil {
		return errors.Join(errNativeOutcomeUnknown, err)
	}
	_, err = a.log.Head(ctx)
	return err
}

func (a *nativeAdmission) close() error {
	var err error
	if a.log != nil {
		err = errors.Join(err, a.log.Close())
	}
	if a.head != nil {
		err = errors.Join(err, a.head.Close())
	}
	if a.gate != nil {
		err = errors.Join(err, a.gate.Close())
	}
	if a.cacheGate != nil {
		err = errors.Join(err, a.cacheGate.Close())
	}
	return err
}

// recoverUnchangedPending never invokes writable SQLite or infers COMMIT
// success from a changed database. It can settle only a byte-identical logical
// last-ACK image under the gate. All other outcomes remain pending, with their
// WAL and journal preserved for explicit, operation-specific reconciliation.
func (s *nativeWALStore) recoverUnchangedPending(ctx context.Context) (resultErr error) {
	if s.quota != nil {
		cacheGate, err := s.quota.acquire(ctx)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, cacheGate.Close()) }()
	}
	gate, err := s.gate(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, gate.Close()) }()
	log, head, err := s.openLog(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, log.Close(), head.Close()) }()
	if _, err := log.Head(ctx); !errors.Is(err, errWALAuthorityPending) {
		return err
	}
	observed, err := s.observed(ctx)
	if err != nil {
		return err
	}
	if observed.Image != log.head.Image || log.pending == nil {
		return errNativeOutcomeUnknown
	}
	if s.quota != nil {
		if err := s.quota.check(ctx, observed.Image, 0, walAuthorityRecordSize); err != nil {
			return err
		}
	}
	return log.Abort(ctx, log.pending.Operation, observed.Image)
}

// ObserveCatalogReadFence authenticates a catalog head during a full native
// admission. The returned check does not authorize a SQLite read or supply a
// binding. A caller must bracket its ordinary binding read with this proof
// before using the binding to authorize a sequence of source reads.
func (o *ProtectedState) ObserveCatalogReadFence(ctx context.Context) (func(context.Context) error, error) {
	if err := o.Check(ctx); err != nil {
		return nil, err
	}
	if o.native == nil || o.native.scope != "catalog" || o.native.name != "catalog.sqlite" {
		return nil, ErrStateUnsafe
	}
	s := o.native
	a, err := s.admit(ctx, false)
	if err != nil {
		return nil, err
	}
	var identity unix.Stat_t
	err = unix.Fstat(int(a.head.file.Fd()), &identity)
	expectedSize, expectedTail, expectedPosition := a.head.size, a.head.chain, a.head.position
	protected := a.head.protected
	if err := errors.Join(err, a.close()); err != nil {
		return nil, err
	}
	if expectedSize <= 0 || identity.Size != expectedSize {
		return nil, ErrStateChanged
	}
	return func(ctx context.Context) (resultErr error) {
		if err := s.check(ctx); err != nil {
			return err
		}
		file, err := s.control.OpenRegular(walHeadName)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
		verifyIdentity := func() error {
			var current unix.Stat_t
			if err := unix.Fstat(int(file.Fd()), &current); err != nil {
				return err
			}
			if current.Dev != identity.Dev || current.Ino != identity.Ino || current.Size != expectedSize {
				return ErrStateChanged
			}
			return ctx.Err()
		}
		if err := verifyIdentity(); err != nil {
			return err
		}
		// This is a read-only observation of the same append-only head, with
		// all normal root, ownership, size, and authenticated tail checks.
		observation := walHeadAuthority{root: s.control, protected: protected, file: file,
			size: expectedSize, chain: expectedTail, position: expectedPosition}
		if err := observation.check(ctx, expectedPosition); err != nil {
			return err
		}
		return verifyIdentity()
	}, nil
}
