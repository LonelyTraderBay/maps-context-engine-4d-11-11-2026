package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
	modsqlite "modernc.org/sqlite"
)

// WindowsScopedPoolsCheck reports the verified identities of a staged scope.
type WindowsScopedPoolsCheck struct {
	ProtectedWorktreeSummary
	InstallationID     string `json:"installation_id"`
	Stage              string `json:"stage"`
	ProfileID          string `json:"profile_id"`
	CatalogIncarnation string `json:"catalog_incarnation"`
	WALHeadsVerified   bool   `json:"wal_heads_verified"`
}

// WindowsScopedPools owns one controller inspection session and three leased
// immutable images. Consumers authorize each call through Check. Close must
// follow the completion of all calls and borrowed-pool work.
type WindowsScopedPools struct {
	Summary   WindowsScopedPoolsCheck
	Catalog   *catalog.Store
	Index     *index.Store
	Records   *records.Store
	Target    *paths.Root
	CommonGit *paths.Root

	gate          chan struct{}
	closed        bool
	checks        []func(context.Context) error
	release       []func() error
	config        windowsMaintenanceFiles
	control       windowsMaintenanceFiles
	worktree      windowsMaintenanceFiles
	worktreeLease *runtime.Lifecycle
	binding       catalog.Binding
	preparation   windowsProfilePreparation
}

func CheckWindowsScopedPools(ctx context.Context, installation, worktree, profile string) (WindowsScopedPoolsCheck, error) {
	p, err := OpenWindowsScopedPools(ctx, installation, worktree, profile)
	if err != nil {
		return WindowsScopedPoolsCheck{}, err
	}
	if err := p.Close(); err != nil {
		return WindowsScopedPoolsCheck{}, err
	}
	return p.Summary, nil
}

// OpenWindowsScopedPools is restricted to the protected profile's controller
// and authenticates both configured AppContainer role processes. The staged
// SQLite images stay read-only and each must match its current WAL head.
func OpenWindowsScopedPools(ctx context.Context, installation, worktree, profile string) (_ *WindowsScopedPools, resultErr error) {
	if ctx == nil || !validWindowsPreparationRoot(installation) || !model.ValidUUID(worktree) || !model.ValidDigest(profile) {
		return nil, ErrProfileInput
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return nil, ErrProfileInput
	}
	p := &WindowsScopedPools{gate: make(chan struct{}, 1)}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, p.Close())
		}
	}()
	root, err := paths.OpenRoot(installation)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, root.Close)
	manifest, acl, err := readWindowsProfilePreparation(ctx, root, installation)
	if err != nil {
		return nil, err
	}
	p.config = windowsMaintenanceFiles{control: root, acl: acl, profile: manifest.ProfileID, scope: "catalog"}
	p.preparation = manifest
	control, err := p.config.openWindowsPoolDirectory("control")
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, control.control.Close)
	p.control = control
	roles, err := openWindowsRoleSession(ctx, manifest, control, windowsIPCOpenPools, worktree+"\x00"+profile)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, roles.Close)
	p.checks = append(p.checks, roles.check, func(ctx context.Context) error {
		now, _, err := readWindowsProfilePreparation(ctx, root, installation)
		if err != nil || now.ProfileID != manifest.ProfileID || now.Stage != manifest.Stage ||
			now.ControllerSID != manifest.ControllerSID || now.EngineSID != manifest.EngineSID || now.ClientSID != manifest.ClientSID ||
			now.LspSID != manifest.LspSID ||
			!slices.Equal(now.TargetRoots, manifest.TargetRoots) {
			return errors.Join(ErrProfileDenied, err)
		}
		return nil
	})
	catalogLease, err := p.openReadLease(ctx, control)
	if err != nil {
		return nil, err
	}
	seedDir, err := control.directory(ctx)
	if err != nil {
		return nil, err
	}
	seed, err := control.readCatalogSeed(ctx, seedDir, manifest)
	if err := errors.Join(err, seedDir.Close()); err != nil || !seed.CatalogVerified {
		return nil, errors.Join(ErrProfileIncomplete, err)
	}
	identity, err := windowsCatalogAuthorityIdentity(manifest.ProfileID, seed.CatalogIncarnation)
	if err != nil {
		return nil, err
	}
	stage, err := control.openCatalogWALStage()
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, stage.control.Close)
	db, err := p.openCapturedPool(ctx, control, stage, windowsCatalogWALMain, identity, catalogLease)
	if err != nil {
		return nil, err
	}
	p.Catalog, err = catalog.OpenAuthenticatedImage(ctx, db, catalogLease)
	if err != nil {
		return nil, errors.Join(ErrProfileIncomplete, err)
	}
	binding, err := p.Catalog.ReadBinding(ctx, worktree, profile, func(_ context.Context, w, id string) error {
		if w != worktree || id != profile {
			return ErrProfileDenied
		}
		return roles.check(ctx)
	})
	if err != nil || binding.CatalogIncarnation != seed.CatalogIncarnation {
		return nil, errors.Join(ErrProfileDenied, err)
	}
	p.binding = binding
	p.Target, err = openWindowsApprovedRegistrationRoot(binding.CanonicalRoot, manifest.TargetRoots)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, p.Target.Close)
	p.CommonGit, err = runtime.OpenLocalGitMetadata(p.Target)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, p.CommonGit.Close)
	verifyRoots := func(context.Context) error { return binding.VerifyRoots(p.Target, p.CommonGit, "") }
	if err := verifyRoots(ctx); err != nil {
		return nil, err
	}
	p.checks = append(p.checks, verifyRoots)

	slot, err := control.openWindowsPoolDirectory("worktrees/" + worktree)
	if err != nil {
		return nil, err
	}
	slot.scope = worktree
	p.release = append(p.release, slot.control.Close)
	worktreeLease, err := p.openReadLease(ctx, slot)
	if err != nil {
		return nil, err
	}
	p.worktree, p.worktreeLease = slot, worktreeLease
	marker, err := slot.readWindowsWorktreeStage(ctx)
	if err != nil {
		return nil, err
	}
	p.checks = append(p.checks, func(ctx context.Context) error {
		now, err := slot.readWindowsWorktreeStage(ctx)
		if err != nil || now != marker {
			return errors.Join(ErrStoreRecoveryRequired, err)
		}
		return nil
	})
	indexDB, err := p.openWorktreePool(ctx, slot, "index", marker.IndexIncarnation, worktreeLease)
	if err != nil {
		return nil, err
	}
	p.Index, err = index.OpenAuthenticatedImage(ctx, indexDB, worktree, worktreeLease)
	if err != nil {
		return nil, err
	}
	recordsDB, err := p.openWorktreePool(ctx, slot, "records", marker.RecordsIncarnation, worktreeLease)
	if err != nil {
		return nil, err
	}
	p.Records, err = records.OpenAuthenticatedImage(ctx, recordsDB, worktree, worktreeLease)
	if err != nil {
		return nil, err
	}
	if err := p.Check(ctx); err != nil {
		return nil, err
	}
	p.Summary = WindowsScopedPoolsCheck{ProtectedWorktreeSummary: marker.ProtectedWorktreeSummary,
		InstallationID: manifest.ProfileID, Stage: manifest.Stage, ProfileID: profile,
		CatalogIncarnation: binding.CatalogIncarnation, WALHeadsVerified: true}
	return p, nil
}

// Check revalidates the role session, profile and Git binding, both leases,
// recovery state, and all current head chains without entering a SQL pool.
func (p *WindowsScopedPools) Check(ctx context.Context) error {
	if ctx == nil {
		return ErrProfileInput
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrProfileInput
	}
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if p.closed {
		return ErrProfileDenied
	}
	for _, check := range p.checks {
		if err := check(ctx); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// ReadConfig reads the optional controller-owned startup configuration.
func (p *WindowsScopedPools) ReadConfig(ctx context.Context, maxBytes int) ([]byte, bool, error) {
	return readWindowsScopeConfig(ctx, p.config, maxBytes)
}

func (p *WindowsScopedPools) ReadIgnore(ctx context.Context, maxBytes int) ([]byte, bool, error) {
	return readWindowsScopeFile(ctx, p.config, windowsGlobalIgnoreName, maxBytes)
}

// CursorKey reads the protected installation key. Older staged profiles
// without a key still use a process-local key in the inspection owner.
func (p *WindowsScopedPools) CursorKey(ctx context.Context) (_ [32]byte, resultErr error) {
	if err := p.Check(ctx); err != nil {
		return [32]byte{}, err
	}
	dir, err := p.control.directory(ctx)
	if err != nil {
		return [32]byte{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	raw, present, err := p.control.readEntryLimit(ctx, dir, windowsCursorKeyName, 32)
	defer clear(raw)
	if err != nil {
		return [32]byte{}, err
	}
	if !present {
		return [32]byte{}, os.ErrNotExist
	}
	if len(raw) != 32 || [32]byte(raw) == ([32]byte{}) {
		return [32]byte{}, ErrProfileIncomplete
	}
	return [32]byte(raw), p.Check(ctx)
}

func readWindowsScopeConfig(ctx context.Context, config windowsMaintenanceFiles, maxBytes int) ([]byte, bool, error) {
	return readWindowsScopeFile(ctx, config, "runtime.json", maxBytes)
}

func readWindowsScopeFile(ctx context.Context, config windowsMaintenanceFiles, name string, maxBytes int) ([]byte, bool, error) {
	dir, err := config.directory(ctx)
	if err != nil {
		return nil, false, err
	}
	data, present, err := config.readEntryLimit(ctx, dir, name, int64(maxBytes))
	return data, present, errors.Join(err, dir.Close())
}

func (p *WindowsScopedPools) Close() error {
	p.gate <- struct{}{}
	defer func() { <-p.gate }()
	if p.closed {
		return nil
	}
	p.closed = true
	var err error
	for i := len(p.release) - 1; i >= 0; i-- {
		err = errors.Join(err, p.release[i]())
	}
	p.release, p.checks = nil, nil
	return err
}

func (p *WindowsScopedPools) openReadLease(ctx context.Context, files windowsMaintenanceFiles) (*runtime.Lifecycle, error) {
	// Pin the provisioned lock so a read never creates a missing lock.
	lock, err := files.control.OpenRegular("lifecycle.lock")
	if err != nil {
		return nil, errors.Join(ErrProfileIncomplete, err)
	}
	p.release = append(p.release, lock.Close)
	lease, err := runtime.AcquireLifecycle(ctx, files.control, files.scope, ports.SharedLease, 5*time.Second)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, lease.Close)
	if err := files.checkServe(ctx, ""); err != nil {
		return nil, err
	}
	p.checks = append(p.checks, func(ctx context.Context) error {
		return errors.Join(lease.CheckRoot(ctx, files.control, files.scope, ports.SharedLease), files.checkServe(ctx, ""))
	})
	return lease, nil
}

func (j windowsMaintenanceFiles) readWindowsWorktreeStage(ctx context.Context) (marker windowsWorktreeStage, resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return marker, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	raw, present, err := j.readEntry(ctx, dir, "worktree.json")
	if err != nil || !present || model.ValidateWireJSON(raw, maintenanceJournalMaxBytes) != nil {
		return marker, errors.Join(ErrProfileIncomplete, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil || marker.FormatVersion != 1 || marker.ProfileID != j.profile ||
		marker.Stage != "CREATING" || marker.WorktreeID != j.scope || !marker.Verified ||
		!model.ValidUUID(marker.IndexIncarnation) || !model.ValidUUID(marker.RecordsIncarnation) ||
		marker.IndexIncarnation == marker.RecordsIncarnation {
		return windowsWorktreeStage{}, errors.Join(ErrProfileIncomplete, err)
	}
	return marker, nil
}

func (p *WindowsScopedPools) openWorktreePool(ctx context.Context, slot windowsMaintenanceFiles, kind, incarnation string,
	lease *runtime.Lifecycle) (*sql.DB, error) {
	data, err := slot.openWindowsPoolDirectory(kind + "-wal")
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, data.control.Close)
	head, err := slot.openWindowsPoolDirectory(kind + "-control")
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, head.control.Close)
	identity, err := slot.windowsWorktreeAuthorityIdentity(kind, incarnation)
	if err != nil {
		return nil, err
	}
	var db *sql.DB
	if kind == "index" {
		db, err = p.openLiveIndexReader(ctx, head, data, identity, lease)
	} else {
		db, err = p.openCapturedPool(ctx, head, data, kind+".sqlite", identity, lease)
	}
	if err != nil {
		return nil, err
	}
	if err := slot.verifyWorktreePool(ctx, db, kind, incarnation); err != nil {
		return nil, err
	}
	return db, nil
}

func (p *WindowsScopedPools) openLiveIndexReader(ctx context.Context, head, data windowsMaintenanceFiles,
	identity walAuthorityIdentity, lease *runtime.Lifecycle) (*sql.DB, error) {
	headDir, err := head.directory(ctx)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, headDir.Close)
	dataDir, err := data.directory(ctx)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, dataDir.Close)
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, hold.Close)
	images := windowsIndexImages{head, data, headDir, dataDir, identity}
	db := sql.OpenDB(&windowsIndexReadConnector{driver: &modsqlite.Driver{}, images: images})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	p.release = append(p.release, db.Close)
	p.checks = append(p.checks, func(ctx context.Context) error {
		_, err := images.capture(ctx)
		return err
	})
	return db, nil
}

func (j windowsMaintenanceFiles) openWindowsPoolDirectory(name string) (windowsMaintenanceFiles, error) {
	child, err := j.control.OpenDirectory(name)
	return windowsMaintenanceFiles{control: child, acl: j.acl, profile: j.profile, scope: j.scope}, err
}

func (p *WindowsScopedPools) openCapturedPool(ctx context.Context, head, data windowsMaintenanceFiles, name string,
	identity walAuthorityIdentity, lease *runtime.Lifecycle) (*sql.DB, error) {
	headDir, err := head.directory(ctx)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, headDir.Close)
	dataDir, err := data.directory(ctx)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, dataDir.Close)
	observe := func(ctx context.Context) ([]byte, [32]byte, error) {
		authority, err := readWindowsWALAuthority(ctx, head, headDir, data, dataDir, identity)
		defer clear(authority.key[:])
		if err != nil || authority.pending != nil {
			return nil, [32]byte{}, errors.Join(ErrStoreRecoveryRequired, err)
		}
		image, logical, err := data.captureWindowsCheckpoint(ctx, dataDir, name)
		if err != nil || authority.head.Image != logical {
			return nil, [32]byte{}, errors.Join(ErrStoreRecoveryRequired, err)
		}
		return image, authority.headChain, nil
	}
	image, current, err := observe(ctx)
	if err != nil {
		return nil, err
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, hold.Close)
	db, release, err := openWindowsSQLiteImage(ctx, image)
	if err != nil {
		return nil, err
	}
	p.release = append(p.release, release)
	p.checks = append(p.checks, func(ctx context.Context) error {
		_, latest, err := observe(ctx)
		if err != nil || latest != current {
			return errors.Join(ErrStoreRecoveryRequired, err)
		}
		return nil
	})
	return db, nil
}

func (j windowsMaintenanceFiles) captureWindowsCheckpoint(ctx context.Context, dir *os.File, name string) ([]byte, walLogicalImage, error) {
	if err := j.requireAbsent(name+"-wal", name+"-shm", name+"-journal"); err != nil {
		return nil, walLogicalImage{}, err
	}
	data, found, err := j.readEntryLimit(ctx, dir, name, windowsCatalogSeedMaxSize)
	if err != nil || !found || len(data) < 100 {
		return nil, walLogicalImage{}, errors.Join(ErrProfileIncomplete, err)
	}
	logical, err := digestCheckpointedMain(ctx, bytes.NewReader(data), int64(len(data)), binary.BigEndian.Uint32(data[28:32]), windowsCatalogSeedMaxSize)
	if err != nil {
		return nil, walLogicalImage{}, err
	}
	return data, logical, j.requireAbsent(name+"-wal", name+"-shm", name+"-journal")
}
