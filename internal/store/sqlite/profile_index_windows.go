package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	powershellprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/powershell-provider"
	pythonprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/python-provider"
	tsprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/ts-provider"
	modsqlite "modernc.org/sqlite"
)

type WindowsIndexScope struct {
	Binding       catalog.Binding
	Target        *paths.Root
	CommonGit     *paths.Root
	LinkedGit     string
	Check         func(context.Context) error
	ReadConfig    func(context.Context, int) ([]byte, bool, error)
	ReadIgnore    func(context.Context, int) ([]byte, bool, error)
	JobID         string
	OpenRuntime   func(context.Context, string) (*paths.Root, error)
	WithScratch   func(context.Context, WindowsProviderCapture, func(*paths.Root, string) error) error
	OpenGoScratch func(context.Context, *snapshot.Snapshot, int64) (*WindowsRenameScratch, error)
	ApprovedRoots []string
}

type WindowsIndexPublish[T any] func(*index.Store, *runtime.Writer, string) (T, error)

type WindowsIndexPlan[T any] struct {
	CacheLimit int64
	Deadline   time.Time
	// Reuse checks the authenticated checkpoint before opening a WAL mutation.
	Reuse   func(*index.Store) (T, bool, error)
	Publish WindowsIndexPublish[T]
}

// RunWindowsIndexMutation admits one captured generation to the Windows
// SQLite owner. Capture and preflight precede the durable PREPARE receipt.
func RunWindowsIndexMutation[T any](ctx context.Context, installation, worktree, profile string,
	prepare func(WindowsIndexScope) (WindowsIndexPlan[T], error)) (out T, resultErr error) {
	if !model.ValidDigest(profile) || prepare == nil {
		return out, ErrProfileInput
	}
	return withWindowsStoreWriter(ctx, installation, "worktree", worktree, "index\x00"+profile, ports.SharedLease, 5*time.Second, func(o windowsStoreMaintenance) (out T, resultErr error) {
		if err := o.files.checkServe(ctx, ""); err != nil {
			return out, err
		}
		binding, err := o.readIndexBinding(ctx, profile)
		if err != nil {
			return out, err
		}
		switch profile := binding.EffectiveProfile().(type) {
		case *model.GoProfile:
			_, raw, err := windowsRegistrationMaterial(ctx, binding.CanonicalRoot)
			if err != nil {
				return out, err
			}
			local, err := model.DecodeEffectiveProfileJSON(raw, 1<<20)
			current, ok := local.(*model.GoProfile)
			if err != nil || !ok || profile.ProviderDigest != current.ProviderDigest ||
				profile.AnalysisMode == "syntax" && profile.ToolchainDigest != current.ToolchainDigest {
				return out, errors.Join(ErrProfileDenied, err)
			}
		case *model.PowerShellProfile:
			if err := powershellprovider.VerifyProfile(profile); err != nil {
				return out, catalog.ErrUnsupportedProfile
			}
		case *model.TSProfile:
			if err := tsprovider.VerifyWindowsProfile(profile); err != nil {
				return out, catalog.ErrUnsupportedProfile
			}
		case *model.PythonProfile:
			if err := pythonprovider.VerifyWindowsProfile(profile); err != nil {
				return out, catalog.ErrUnsupportedProfile
			}
		default:
			return out, catalog.ErrUnsupportedProfile
		}
		// Typed profiles can publish syntax fallback with the executing
		// provider digest. Their Go distribution is proved by the typed
		// analysis owner before it enables a typed capability.
		configRoot, err := paths.OpenRoot(installation)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, configRoot.Close()) }()
		config := o.catalogControl
		config.control = configRoot
		readConfig := func(ctx context.Context, limit int) ([]byte, bool, error) {
			return readWindowsScopeConfig(ctx, config, limit)
		}
		readIgnore := func(ctx context.Context, limit int) ([]byte, bool, error) {
			return readWindowsScopeFile(ctx, config, windowsGlobalIgnoreName, limit)
		}
		root, err := openWindowsApprovedRegistrationRoot(binding.CanonicalRoot, o.profile.TargetRoots)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
		common, linked, err := openWindowsApprovedGitMetadata(root, o.profile.TargetRoots)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, common.Close()) }()
		check := func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return errors.Join(o.check(), binding.VerifyRoots(root, common, linked), o.files.checkServe(ctx, ""))
		}
		if err := check(ctx); err != nil {
			return out, err
		}
		scope := WindowsIndexScope{Binding: binding, Target: root, CommonGit: common, LinkedGit: linked, Check: check,
			ReadConfig: readConfig, ReadIgnore: readIgnore, JobID: o.job, OpenRuntime: o.catalogControl.openWindowsProviderRuntime,
			WithScratch: func(ctx context.Context, capture WindowsProviderCapture, use func(*paths.Root, string) error) error {
				return o.withProviderCapture(ctx, capture, check, use)
			}, ApprovedRoots: append([]string{installation}, o.profile.TargetRoots...)}
		if o.profile.LspSID != "" {
			scope.OpenGoScratch = func(ctx context.Context, captured *snapshot.Snapshot, limit int64) (*WindowsRenameScratch, error) {
				return openWindowsGoScratch(ctx, captured, limit, binding, configRoot, o.profile, o.files, o.lease, check)
			}
		}
		plan, err := prepare(scope)
		if err != nil {
			return out, err
		}
		if plan.Publish == nil || plan.Deadline.IsZero() {
			return out, ErrProfileInput
		}
		ctx, cancel := context.WithDeadline(ctx, plan.Deadline)
		defer cancel()
		// Keep the existing reserve for a 1 MiB index. Larger indexes
		// also need space for their preimage, WAL/SHM and receipt writes.
		// Leave at least half the cache budget for existing files when
		// scaling the reservation, and charge those files separately.
		const reserveFactor = 8
		imageLimit := min(windowsIndexImageMaxBytes, max(windowsCatalogSeedMaxSize, plan.CacheLimit/(2*reserveFactor)))
		cache, err := reserveWindowsCache(ctx, o.files, o.lease, o.profile.LspSID, check, plan.CacheLimit, imageLimit*reserveFactor)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, cache.Close()) }()
		out, err = runWindowsIndexWAL(ctx, o, check, imageLimit, plan.Publish, plan.Reuse)
		if err != nil {
			return out, err
		}
		return out, cache.Check(ctx)
	})
}

func (o windowsStoreMaintenance) readIndexBinding(ctx context.Context, profile string) (binding catalog.Binding, resultErr error) {
	catalogOwner := windowsStoreMaintenance{files: o.catalogControl, profile: o.profile, lease: o.catalogLease}
	ids, err := catalogOwner.incarnations(ctx)
	if err != nil {
		return binding, err
	}
	stores, release, err := catalogOwner.openStores()
	if err != nil {
		return binding, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	raw, _, key, err := stores[0].capture(ctx, ids[0])
	clear(key[:])
	if err != nil {
		return binding, err
	}
	hold, err := o.catalogLease.Hold(ctx)
	if err != nil {
		return binding, err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	err = withWindowsSQLiteImage(ctx, raw, func(db *sql.DB) error {
		store, err := catalog.OpenAuthenticatedImage(ctx, db, o.catalogLease)
		if err != nil {
			return err
		}
		binding, err = store.ReadBinding(ctx, o.files.scope, profile, func(_ context.Context, w, p string) error {
			if w != o.files.scope || p != profile {
				return ErrProfileDenied
			}
			return o.check()
		})
		return err
	})
	if err != nil || binding.CatalogIncarnation != ids[0] {
		return catalog.Binding{}, errors.Join(ErrProfileDenied, err)
	}
	return binding, nil
}

func runWindowsIndexWAL[T any](ctx context.Context, o windowsStoreMaintenance, check func(context.Context) error, imageLimit int64,
	publish WindowsIndexPublish[T], reuse func(*index.Store) (T, bool, error)) (out T, resultErr error) {
	ids, err := o.incarnations(ctx)
	if err != nil {
		return out, err
	}
	stores, release, err := o.openStores()
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	// Prove the accompanying records head before writing the index.
	_, _, key, err := stores[1].capture(ctx, ids[1])
	clear(key[:])
	if err != nil {
		return out, err
	}
	s := stores[0]
	dataDir, err := s.data.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, dataDir.Close()) }()
	headDir, err := s.head.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, headDir.Close()) }()
	identity, err := s.identity(ids[0])
	if err != nil {
		return out, err
	}
	images := windowsIndexImages{s.head, s.data, headDir, dataDir, identity}
	var reused bool
	var checkReuse func([]byte) (bool, error)
	if reuse != nil {
		checkReuse = func(image []byte) (bool, error) {
			err := withWindowsSQLiteImage(ctx, image, func(db *sql.DB) error {
				store, err := index.OpenAuthenticatedImage(ctx, db, o.files.scope, o.lease)
				if err != nil {
					return err
				}
				observed, err := store.Identity(ctx)
				if err != nil || observed.Incarnation != ids[0] {
					return errors.Join(ErrStateChanged, err)
				}
				out, reused, err = reuse(store)
				return err
			})
			return reused, err
		}
	}
	authority, event, err := images.prepareMutation(ctx, o, ids[0], check, checkReuse, imageLimit)
	defer clear(authority.key[:])
	if err != nil || reused {
		return out, err
	}
	path, err := s.data.control.CanonicalPath()
	if err != nil {
		return out, errors.Join(ErrStoreRecoveryRequired, err)
	}
	result, sqlErr := executeWindowsIndex(ctx, o, filepath.Join(path, s.main), event.Image.PageSize, ids[0], imageLimit, publish)
	if err := images.finishMutation(ctx, o, s, ids[0], check, &authority, event, sqlErr); err != nil {
		return out, errors.Join(ErrStoreRecoveryRequired, sqlErr, err)
	}
	return result, sqlErr
}

func (s windowsIndexImages) prepareMutation(ctx context.Context, o windowsStoreMaintenance, incarnation string,
	check func(context.Context) error, reuse func([]byte) (bool, error), imageLimit int64) (authority windowsWALAuthority, event walAuthorityRecord, resultErr error) {
	gate, err := s.acquire(ctx, ports.ExclusiveLease)
	if err != nil {
		return authority, event, err
	}
	defer func() { resultErr = errors.Join(resultErr, gate.Close()) }()
	authority, err = s.authority(ctx)
	if errors.Is(err, ErrStoreRecoveryRequired) {
		authority, err = s.finishInterruptedIndexReceipt(ctx, incarnation, func() error {
			return s.checkMutation(ctx, o, gate, check)
		})
	}
	if err != nil {
		return authority, event, err
	}
	image, before, err := s.data.captureWindowsCheckpoint(ctx, s.dataDir, "index.sqlite")
	if err != nil || before != authority.head.Image {
		return authority, event, errors.Join(ErrStoreRecoveryRequired, err)
	}
	if int64(len(image)) > imageLimit {
		return authority, event, ErrStateLimit
	}
	if authority.pending != nil {
		if authority.pending.Image != before || authority.head.Prefix != (walCommitPrefixReceipt{}) {
			return authority, event, ErrStoreRecoveryRequired
		}
		abort := walAuthorityRecord{kind: walAuthorityAbort, walAuthorityReceipt: *authority.pending}
		if err := s.appendMutation(ctx, o, gate, check, &authority, abort); err != nil {
			return authority, event, errors.Join(ErrStoreRecoveryRequired, err)
		}
	}
	if reuse != nil {
		if unchanged, err := reuse(image); err != nil || unchanged {
			return authority, event, errors.Join(err, s.checkMutation(ctx, o, gate, check))
		}
	}
	if authority.records > walAuthorityMaxBytes/walAuthorityRecordSize-2 {
		return authority, event, ErrStateLimit
	}
	var operation [32]byte
	if _, err := rand.Read(operation[:]); err != nil {
		return authority, event, err
	}
	if err := check(ctx); err != nil {
		return authority, event, err
	}
	if err := s.publishPreimage(ctx, operation, image); err != nil {
		return authority, event, err
	}
	event = walAuthorityRecord{kind: walAuthorityPrepare, walAuthorityReceipt: walAuthorityReceipt{
		Sequence: authority.head.Sequence + 1, Operation: operation, Image: before,
	}}
	err = s.appendMutation(ctx, o, gate, check, &authority, event)
	return authority, event, err
}

func (s windowsIndexImages) finishMutation(ctx context.Context, o windowsStoreMaintenance, store windowsMaintenanceStore, incarnation string,
	check func(context.Context) error, authority *windowsWALAuthority, event walAuthorityRecord, sqlErr error) (resultErr error) {
	gate, err := s.acquire(ctx, ports.ExclusiveLease)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, gate.Close()) }()
	after, err := s.data.closedWorktreeImage(ctx, s.dataDir, store.main, store.kind, incarnation)
	if err != nil {
		return err
	}
	if sqlErr != nil {
		if after != event.Image {
			return ErrStoreRecoveryRequired
		}
		event.kind, event.Prefix = walAuthorityAbort, authority.head.Prefix
	} else {
		event.kind, event.Image = walAuthorityCommitQuiesced, after
	}
	if err := s.appendMutation(ctx, o, gate, check, authority, event); err != nil {
		return err
	}
	_, latest, key, err := store.capture(ctx, incarnation)
	clear(key[:])
	if err := errors.Join(err, check(ctx)); err != nil || latest != after {
		return errors.Join(ErrStoreRecoveryRequired, err)
	}
	return nil
}

func (s windowsIndexImages) appendMutation(ctx context.Context, o windowsStoreMaintenance, gate *runtime.Lifecycle,
	check func(context.Context) error, authority *windowsWALAuthority, event walAuthorityRecord) error {
	if err := s.checkMutation(ctx, o, gate, check); err != nil {
		return err
	}
	return authority.appendFiles(ctx, o.files, s.head, s.headDir, s.data, s.dataDir, event)
}

func (s windowsIndexImages) checkMutation(ctx context.Context, o windowsStoreMaintenance, gate *runtime.Lifecycle, check func(context.Context) error) error {
	return errors.Join(check(ctx), o.writer.CheckWriterRoot(ctx, o.files.control, o.files.scope, o.job),
		gate.CheckRoot(ctx, s.head.control, s.head.scope, ports.ExclusiveLease))
}

func executeWindowsIndex[T any](ctx context.Context, o windowsStoreMaintenance, name string, pageSize uint32, incarnation string, imageLimit int64,
	publish WindowsIndexPublish[T]) (out T, resultErr error) {
	hold, err := o.lease.Hold(ctx)
	if err != nil {
		return out, err
	}
	db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: windowsSQLiteRW(name)})
	db.SetMaxOpenConns(1)
	defer func() { resultErr = errors.Join(resultErr, db.Close(), hold.Close()) }()
	wanted := int(imageLimit / int64(pageSize))
	var pages int
	if err := db.QueryRowContext(ctx, "PRAGMA max_page_count="+strconv.Itoa(wanted)).Scan(&pages); err != nil || pages != wanted {
		return out, errors.Join(ErrStateLimit, err)
	}
	store, err := index.Open(ctx, db, db, o.files.scope, o.lease)
	if err != nil {
		return out, err
	}
	identity, err := store.Identity(ctx)
	if err != nil || identity.Incarnation != incarnation {
		return out, errors.Join(ErrStateChanged, err)
	}
	return publish(store, o.writer, o.job)
}
