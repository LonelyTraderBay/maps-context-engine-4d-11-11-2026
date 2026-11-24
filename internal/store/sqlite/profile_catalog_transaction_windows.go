package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
)

// RegisterWindowsStagedRepo is an offline controller operation on the
// CREATING catalog. It authenticates the held role process objects at the
// write boundaries. Only the controller may request the operation.
func RegisterWindowsStagedRepo(ctx context.Context, installation, target string, enginePID, clientPID uint32) (catalog.Registration, error) {
	return registerWindowsStagedRepo(ctx, installation, target, func(m windowsProfilePreparation, _ windowsMaintenanceFiles, use func(func() error) error) error {
		return withVerifiedWindowsRoles(ctx, m, enginePID, clientPID, use)
	})
}

func RegisterWindowsStagedRepoIPC(ctx context.Context, installation, target string) (catalog.Registration, error) {
	return registerWindowsStagedRepo(ctx, installation, target, func(m windowsProfilePreparation, control windowsMaintenanceFiles, use func(func() error) error) error {
		return withWindowsRoleIPC(ctx, m, control, windowsIPCRegisterCatalog, target, use)
	})
}

func registerWindowsStagedRepo(ctx context.Context, installation, target string,
	withRoles func(windowsProfilePreparation, windowsMaintenanceFiles, func(func() error) error) error) (catalog.Registration, error) {
	return withWindowsProfileControl(ctx, installation, func(m windowsProfilePreparation, control windowsMaintenanceFiles) (out catalog.Registration, err error) {
		root, err := openWindowsApprovedRegistrationRoot(target, m.TargetRoots)
		if err != nil {
			return out, err
		}
		defer func() { err = errors.Join(err, root.Close()) }()
		err = withRoles(m, control, func(checkRoles func() error) (resultErr error) {
			common, linked, err := openWindowsApprovedGitMetadata(root, m.TargetRoots)
			if err != nil {
				return err
			}
			defer func() { resultErr = errors.Join(resultErr, common.Close()) }()
			policy, profile, err := windowsRegistrationMaterial(ctx, target)
			if err != nil {
				return err
			}
			out, err = runWindowsCatalogTransaction(ctx, control, m, checkRoles, func(store *catalog.Store) (catalog.Registration, error) {
				return store.Register(ctx, root, common, linked, "default", policy, profile)
			})
			return err
		})
		return out, err
	})
}

func openWindowsApprovedRegistrationRoot(target string, approved []string) (*paths.Root, error) {
	for _, name := range approved {
		if target == name {
			root, err := paths.OpenRoot(target)
			if err != nil {
				return nil, err
			}
			actual, err := root.CanonicalPath()
			if err != nil || actual != target {
				return nil, errors.Join(ErrProfileDenied, err, root.Close())
			}
			return root, nil
		}
	}
	return nil, ErrProfileDenied
}

func openWindowsApprovedGitMetadata(worktree *paths.Root, approved []string) (*paths.Root, string, error) {
	return runtime.OpenApprovedGitMetadata(worktree, func(name string) (*paths.Root, error) {
		root, err := openWindowsApprovedRegistrationRoot(name, approved)
		if err != nil {
			return nil, err
		}
		common, err := runtime.OpenLocalGitMetadata(root)
		if err := errors.Join(err, root.Close()); err != nil {
			if common != nil {
				err = errors.Join(err, common.Close())
			}
			return nil, err
		}
		return common, nil
	})
}

func windowsRegistrationMaterial(ctx context.Context, target string) (policy, profile []byte, resultErr error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	dir, err := paths.OpenRoot(filepath.Dir(executable))
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	f, err := dir.OpenRegular(filepath.Base(executable))
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	const limit int64 = 128 << 20
	info, err := f.Stat()
	if err != nil || info.Size() < 1 || info.Size() > limit {
		return nil, nil, errors.Join(ErrStateUnsafe, err)
	}
	digest, err := hashLogicalReader(ctx, f, info.Size())
	if err != nil {
		return nil, nil, err
	}
	if err := errors.Join(paths.VerifyRegularFile(f), dir.Verify()); err != nil {
		return nil, nil, err
	}
	provider := hex.EncodeToString(digest[:])
	toolchain := sha256.Sum256([]byte("MCE4D-EMBEDDED-GO-PARSER1\x00" + goruntime.Version() + "\x00" + provider))
	config := model.DefaultRuntimeConfig().Core
	policy, err = json.Marshal(model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{target}, DeniedGlobs: []string{},
		TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{},
		MaxFileBytes: config.MaxFileBytes, MaxSourceBytes: config.MaxSourceBytesPerSnapshot, MaxOutputBytes: config.MaxOutputBytes,
		SecretRulesVersion: "builtin-v1"})
	if err != nil {
		return nil, nil, err
	}
	profile, err = json.Marshal(&model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "windows", Goarch: goruntime.GOARCH,
		BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: hex.EncodeToString(toolchain[:]),
		ProviderDigest: provider, ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}})
	return policy, profile, err
}

func runWindowsCatalogTransaction[T any](ctx context.Context, j windowsMaintenanceFiles, m windowsProfilePreparation, checkRoles func() error, run func(*catalog.Store) (T, error)) (out T, resultErr error) {
	dir, err := j.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	lease, err := runtime.AcquireLifecycle(ctx, j.control, "catalog", ports.ExclusiveLease, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	if err := j.checkServe(ctx, ""); err != nil {
		return out, err
	}
	seed, err := j.readCatalogSeed(ctx, dir, m)
	if err != nil || !seed.CatalogVerified {
		return out, errors.Join(ErrProfileIncomplete, err)
	}
	staged, err := j.openCatalogWALStage()
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, staged.control.Close()) }()
	stageDir, err := staged.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, stageDir.Close()) }()
	identity, err := windowsCatalogAuthorityIdentity(m.ProfileID, seed.CatalogIncarnation)
	if err != nil {
		return out, err
	}
	authority, err := readWindowsWALAuthority(ctx, j, dir, staged, stageDir, identity)
	if err != nil {
		return out, err
	}
	defer clear(authority.key[:])
	if authority.pending != nil {
		return out, ErrStoreRecoveryRequired
	}
	if authority.records > walAuthorityMaxBytes/walAuthorityRecordSize-2 {
		return out, ErrStateLimit
	}
	before, err := staged.checkpointedCatalogWAL(ctx, stageDir, seed.CatalogIncarnation)
	if err != nil || before != authority.head.Image {
		return out, errors.Join(ErrStoreRecoveryRequired, err)
	}
	base, err := staged.control.CanonicalPath()
	if err != nil {
		return out, err
	}
	var operation [32]byte
	if _, err := rand.Read(operation[:]); err != nil {
		return out, err
	}
	operation[6], operation[8] = operation[6]&15|64, operation[8]&63|128
	opID := fmt.Sprintf("%x-%x-%x-%x-%x", operation[:4], operation[4:6], operation[6:8], operation[8:10], operation[10:16])
	writer, err := runtime.AcquireCatalogMutation(ctx, lease, opID, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	check := func(ctx context.Context) error {
		return errors.Join(ctx.Err(), checkRoles(), lease.CheckRoot(ctx, j.control, "catalog", ports.ExclusiveLease),
			staged.acl.check(stageDir), staged.control.Verify(), j.checkServe(ctx, ""))
	}
	if err := check(ctx); err != nil {
		return out, err
	}
	event := walAuthorityRecord{kind: walAuthorityPrepare, walAuthorityReceipt: walAuthorityReceipt{
		Sequence: authority.head.Sequence + 1, Operation: operation, Image: before,
	}}
	if err := authority.append(ctx, lease, j, j, dir, staged, stageDir, event); err != nil {
		return out, err
	}
	result, sqlErr := executeWindowsCatalogMutation(ctx, filepath.Join(base, windowsCatalogWALMain), lease, writer, opID,
		before.PageSize, check, run)
	if err := check(ctx); err != nil {
		return out, errors.Join(ErrStoreRecoveryRequired, sqlErr, err)
	}
	after, err := staged.checkpointedCatalogWAL(ctx, stageDir, seed.CatalogIncarnation)
	if err != nil {
		return out, errors.Join(ErrStoreRecoveryRequired, sqlErr, err)
	}
	if sqlErr != nil {
		if after != before {
			return out, errors.Join(ErrStoreRecoveryRequired, sqlErr)
		}
		event.kind, event.Image, event.Prefix = walAuthorityAbort, before, authority.head.Prefix
	} else {
		event.kind, event.Image = walAuthorityCommitQuiesced, after
	}
	if err := authority.append(ctx, lease, j, j, dir, staged, stageDir, event); err != nil {
		return out, errors.Join(ErrStoreRecoveryRequired, sqlErr, err)
	}
	if err := errors.Join(check(ctx), j.checkCatalogWAL(ctx, dir, seed.CatalogIncarnation)); err != nil {
		return out, errors.Join(ErrStoreRecoveryRequired, sqlErr, err)
	}
	return result, sqlErr
}

func executeWindowsCatalogMutation[T any](ctx context.Context, name string, lease *runtime.Lifecycle, writer *runtime.Writer, operation string,
	pageSize uint32, check func(context.Context) error, run func(*catalog.Store) (T, error)) (out T, resultErr error) {
	values := url.Values{"mode": {"rw"}, "_pragma": {"synchronous(FULL)", "busy_timeout(5000)", "foreign_keys(1)",
		"temp_store(MEMORY)", "wal_autocheckpoint(0)"}}
	uri := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(name), RawQuery: values.Encode()}
	db := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: uri.String()})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	owner, err := OpenMaintenanceOwner(ctx, lease, db, "catalog")
	if err != nil {
		return out, errors.Join(err, db.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	wantPages := windowsCatalogSeedMaxSize / int(pageSize)
	var actual int
	if err := db.QueryRowContext(ctx, "PRAGMA max_page_count="+strconv.Itoa(wantPages)).Scan(&actual); err != nil || actual != wantPages {
		return out, errors.Join(ErrStateLimit, err)
	}
	store, err := catalog.OpenExistingLeased(ctx, db, lease)
	if err != nil {
		return out, err
	}
	store, err = store.WithAuthorizedMutation(ctx, writer, operation, check)
	if err != nil {
		return out, err
	}
	return run(store)
}
