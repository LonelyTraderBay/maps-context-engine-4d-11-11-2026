package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
	"golang.org/x/sys/windows"
	modsqlite "modernc.org/sqlite"
)

type windowsStoreMaintenance struct {
	files          windowsMaintenanceFiles
	profile        windowsProfilePreparation
	lease          *runtime.Lifecycle
	revisions      catalog.BackupRevisions
	check          func() error
	catalogControl windowsMaintenanceFiles
	catalogLease   *runtime.Lifecycle
	writer         *runtime.Writer
	job            string
}

func windowsMaintenanceID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6], id[8] = id[6]&15|64, id[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

func withWindowsMaintenanceLease[T any](ctx context.Context, files windowsMaintenanceFiles, mode ports.LeaseMode, wait time.Duration, use func(*runtime.Lifecycle) (T, error)) (out T, resultErr error) {
	lock, err := files.control.OpenRegular("lifecycle.lock")
	if err != nil {
		return out, errors.Join(ErrProfileIncomplete, err)
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	lease, err := runtime.AcquireLifecycle(ctx, files.control, files.scope, mode, wait)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	return use(lease)
}

func withWindowsStoreMaintenance[T any](ctx context.Context, installation, target, worktree, action string, wait time.Duration, use func(windowsStoreMaintenance) (T, error)) (out T, resultErr error) {
	return withWindowsStoreWriter(ctx, installation, target, worktree, action, ports.ExclusiveLease, wait, use)
}

func withWindowsStoreWriter[T any](ctx context.Context, installation, target, worktree, action string,
	worktreeMode ports.LeaseMode, wait time.Duration, use func(windowsStoreMaintenance) (T, error)) (out T, resultErr error) {
	if ctx == nil || wait <= 0 || wait > RestoreMaxWait ||
		!(target == "catalog" && worktree == "" || target == "worktree" && model.ValidUUID(worktree)) {
		return out, ErrProfileInput
	}
	return withWindowsProfileControl(ctx, installation, func(m windowsProfilePreparation, control windowsMaintenanceFiles) (out T, resultErr error) {
		session, err := openWindowsRoleSession(ctx, m, control, windowsIPCMaintenance, action+"\x00"+target+"\x00"+worktree)
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, session.Close()) }()
		mode := ports.ExclusiveLease
		if target == "worktree" {
			mode = ports.SharedLease
		}
		return withWindowsMaintenanceLease(ctx, control, mode, wait, func(catalogLease *runtime.Lifecycle) (out T, resultErr error) {
			run := func(o windowsStoreMaintenance) (T, error) {
				o.catalogControl, o.catalogLease = control, catalogLease
				return use(o)
			}
			checkCatalog := func() error {
				return catalogLease.CheckRoot(ctx, control.control, "catalog", mode)
			}
			files, lease := control, catalogLease
			var revisions catalog.BackupRevisions
			if target == "worktree" {
				if err := control.checkServe(ctx, ""); err != nil {
					return out, err
				}
				var path string
				revisions, path, err = control.windowsMaintenanceRevisions(ctx, m, catalogLease, worktree, func(context.Context) error {
					return errors.Join(checkCatalog(), session.check(ctx))
				})
				if err != nil {
					return out, err
				}
				root, err := openWindowsApprovedRegistrationRoot(path, m.TargetRoots)
				if err != nil {
					return out, err
				}
				defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
				baseCheck := checkCatalog
				checkCatalog = func() error { return errors.Join(baseCheck(), root.Verify(), control.checkServe(ctx, "")) }
				files, err = control.openWindowsPoolDirectory("worktrees/" + worktree)
				if err != nil {
					return out, err
				}
				defer func() { resultErr = errors.Join(resultErr, files.control.Close()) }()
				files.scope = worktree
				return withWindowsMaintenanceLease(ctx, files, worktreeMode, wait, func(lease *runtime.Lifecycle) (T, error) {
					return withWindowsMaintenanceWriter(ctx, m, files, lease, worktreeMode, revisions, wait, checkCatalog, session, run)
				})
			}
			return withWindowsMaintenanceWriter(ctx, m, files, lease, mode, revisions, wait, checkCatalog, session, run)
		})
	})
}

func withWindowsMaintenanceWriter[T any](ctx context.Context, m windowsProfilePreparation, files windowsMaintenanceFiles, lease *runtime.Lifecycle,
	mode ports.LeaseMode, revisions catalog.BackupRevisions, wait time.Duration, checkCatalog func() error, session *windowsRoleSession,
	use func(windowsStoreMaintenance) (T, error)) (out T, resultErr error) {
	job, err := windowsMaintenanceID()
	if err != nil {
		return out, err
	}
	acquire := runtime.AcquireWriter
	if files.scope == "catalog" {
		acquire = runtime.AcquireCatalogMutation
	}
	writer, err := acquire(ctx, lease, job, wait)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	owner := windowsStoreMaintenance{files: files, profile: m, lease: lease, revisions: revisions, writer: writer, job: job, check: func() error {
		return errors.Join(checkCatalog(), session.check(ctx), lease.CheckRoot(ctx, files.control, files.scope, mode),
			writer.CheckWriterRoot(ctx, files.control, files.scope, job))
	}}
	if err := owner.check(); err != nil {
		return out, err
	}
	return use(owner)
}

type windowsMaintenanceStore struct {
	kind, main string
	head, data windowsMaintenanceFiles
}

const windowsIndexImageMaxBytes = 4 << 20

func windowsStoreImageLimit(name string) int64 {
	if name == "index.sqlite" {
		return windowsIndexImageMaxBytes
	}
	return windowsCatalogSeedMaxSize
}

func (o windowsStoreMaintenance) openStores() ([]windowsMaintenanceStore, func() error, error) {
	var stores []windowsMaintenanceStore
	var roots []func() error
	release := func() error {
		var err error
		for i := len(roots) - 1; i >= 0; i-- {
			err = errors.Join(err, roots[i]())
		}
		return err
	}
	open := func(name string) (windowsMaintenanceFiles, error) {
		dir, err := o.files.openWindowsPoolDirectory(name)
		if err == nil {
			roots = append(roots, dir.control.Close)
		}
		return dir, err
	}
	if o.files.scope == "catalog" {
		data, err := open(windowsCatalogWALStage)
		if err != nil {
			return nil, nil, err
		}
		return []windowsMaintenanceStore{{"catalog", windowsCatalogWALMain, o.files, data}}, release, nil
	}
	for _, kind := range []string{"index", "records"} {
		data, err := open(kind + "-wal")
		if err != nil {
			return nil, nil, errors.Join(err, release())
		}
		head, err := open(kind + "-control")
		if err != nil {
			return nil, nil, errors.Join(err, release())
		}
		stores = append(stores, windowsMaintenanceStore{kind, kind + ".sqlite", head, data})
	}
	return stores, release, nil
}

func (o windowsStoreMaintenance) incarnations(ctx context.Context) ([]string, error) {
	if o.files.scope != "catalog" {
		marker, err := o.files.readWindowsWorktreeStage(ctx)
		return []string{marker.IndexIncarnation, marker.RecordsIncarnation}, err
	}
	dir, err := o.files.directory(ctx)
	if err != nil {
		return nil, err
	}
	seed, err := o.files.readCatalogSeed(ctx, dir, o.profile)
	if err := errors.Join(err, dir.Close()); err != nil || !seed.CatalogVerified {
		return nil, errors.Join(ErrProfileIncomplete, err)
	}
	return []string{seed.CatalogIncarnation}, nil
}

func (s windowsMaintenanceStore) identity(incarnation string) (walAuthorityIdentity, error) {
	if s.kind == "catalog" {
		return windowsCatalogAuthorityIdentity(s.head.profile, incarnation)
	}
	return s.head.windowsWorktreeAuthorityIdentity(s.kind, incarnation)
}

func (s windowsMaintenanceStore) capture(ctx context.Context, incarnation string) (_ []byte, _ walLogicalImage, _ [32]byte, resultErr error) {
	dataDir, err := s.data.directory(ctx)
	if err != nil {
		return nil, walLogicalImage{}, [32]byte{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, dataDir.Close()) }()
	headDir, err := s.head.directory(ctx)
	if err != nil {
		return nil, walLogicalImage{}, [32]byte{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, headDir.Close()) }()
	identity, err := s.identity(incarnation)
	if err != nil {
		return nil, walLogicalImage{}, [32]byte{}, err
	}
	current, err := readWindowsWALAuthority(ctx, s.head, headDir, s.data, dataDir, identity)
	defer clear(current.key[:])
	if err != nil || current.pending != nil {
		return nil, walLogicalImage{}, [32]byte{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	image, proof, err := s.data.captureWindowsCheckpoint(ctx, dataDir, s.main)
	if err != nil || proof != current.head.Image {
		return nil, walLogicalImage{}, [32]byte{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return image, proof, current.key, nil
}

func (j windowsMaintenanceFiles) windowsMaintenanceRevisions(ctx context.Context, m windowsProfilePreparation, lease *runtime.Lifecycle,
	worktree string, check func(context.Context) error) (revisions catalog.BackupRevisions, path string, resultErr error) {
	o := windowsStoreMaintenance{files: j, profile: m, lease: lease}
	ids, err := o.incarnations(ctx)
	if err != nil {
		return revisions, "", err
	}
	stores, closeStores, err := o.openStores()
	if err != nil {
		return revisions, "", err
	}
	defer func() { resultErr = errors.Join(resultErr, closeStores()) }()
	image, _, key, err := stores[0].capture(ctx, ids[0])
	clear(key[:])
	if err != nil {
		return revisions, "", err
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return revisions, "", err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	err = withWindowsSQLiteImage(ctx, image, func(db *sql.DB) error {
		store, err := catalog.OpenAuthenticatedImage(ctx, db, lease)
		if err != nil {
			return err
		}
		revisions, err = store.ReadBackupRevisions(ctx, worktree, check)
		if err != nil {
			return err
		}
		if revisions.CatalogIncarnation != ids[0] {
			return ErrStoreRecoveryRequired
		}
		path, err = store.ReadWorktreeRoot(ctx, worktree, check)
		return err
	})
	return revisions, path, err
}

// verifyImage uses the same SQL schema, scope and integrity checks as serving.
func (s windowsMaintenanceStore) verifyImage(ctx context.Context, db *sql.DB, lease *runtime.Lifecycle, expected string) error {
	switch s.kind {
	case "catalog":
		id, err := catalog.VerifyReadOnlyImage(ctx, db)
		if err != nil || id != expected {
			return errors.Join(ErrStateChanged, err)
		}
	case "index":
		if _, err := index.OpenAuthenticatedImage(ctx, db, s.head.scope, lease); err != nil {
			return err
		}
		return s.head.verifyWorktreePool(ctx, db, s.kind, expected)
	case "records":
		if _, err := records.OpenAuthenticatedImage(ctx, db, s.head.scope, lease); err != nil {
			return err
		}
		return s.head.verifyWorktreePool(ctx, db, s.kind, expected)
	default:
		return ErrProfileInput
	}
	return ctx.Err()
}

func (j windowsMaintenanceFiles) requiredBytes(ctx context.Context, name string, limit int64) (_ []byte, resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	data, found, err := j.readEntryLimit(ctx, dir, name, limit)
	if err != nil || !found {
		return nil, errors.Join(ErrProfileIncomplete, err)
	}
	return data, nil
}

func windowsSQLiteRW(name string) string {
	query := url.Values{"mode": {"rw"}, "_pragma": {"foreign_keys(1)", "trusted_schema(0)", "synchronous(FULL)"}}
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(name), RawQuery: query.Encode()}).String()
}

func openWindowsMaintenanceSQLite(ctx context.Context, lease *runtime.Lifecycle, name, scope string) (*sql.DB, func() error, error) {
	db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: windowsSQLiteRW(name)})
	db.SetMaxOpenConns(1)
	owner, err := OpenMaintenanceOwner(ctx, lease, db, scope)
	if err != nil {
		return nil, nil, errors.Join(err, db.Close())
	}
	return db, owner.Close, nil
}

func windowsOperationLeaf(leaf string) bool {
	return len(leaf) > 0 && len(leaf) <= 64 && filepath.IsLocal(leaf) && filepath.Base(leaf) == leaf &&
		model.ValidateRelativePath(leaf, false) == nil && !strings.ContainsAny(leaf, "<>:\"|?*") &&
		!strings.HasSuffix(leaf, ".") && !strings.HasSuffix(leaf, " ")
}

func windowsStageBackupCopy(ctx context.Context, lease *runtime.Lifecycle, image []byte, destination windowsMaintenanceFiles, filename string, limit int64) (out backupImage, resultErr error) {
	if int64(len(image)) > limit || limit > windowsStoreImageLimit(filename) {
		return out, ErrStateLimit
	}
	if err := destination.publishStagedWorktreeFile(ctx, filename, nil); err != nil {
		return out, err
	}
	path, err := destination.control.CanonicalPath()
	if err != nil {
		return out, err
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	err = withWindowsSQLiteImage(ctx, image, func(source *sql.DB) (resultErr error) {
		conn, err := source.Conn(ctx)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
		out, err = copyPinnedSQLiteBackup(ctx, conn, windowsSQLiteRW(filepath.Join(path, filename)), limit)
		return err
	})
	if err != nil {
		return out, err
	}
	return out, destination.requireAbsent(filename+"-wal", filename+"-shm", filename+"-journal")
}

func (j windowsMaintenanceFiles) flushArtifact(ctx context.Context, name string) (resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	desc, err := j.acl.descriptor()
	if err != nil {
		return err
	}
	file, err := openWindowsControlEntry(dir, name, desc, false, windows.FILE_OPEN)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	return errors.Join(j.acl.check(file), paths.VerifyRegularFile(file), file.Sync(), ctx.Err())
}
