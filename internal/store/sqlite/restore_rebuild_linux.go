//go:build linux

package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func (o *quiescentOwner) prepareIndexRebuild(ctx context.Context, planRoot *paths.Root, operation string) (old, next []MaintenanceStore, resultErr error) {
	if len(o.stores) != 2 || o.stores[0].name != "index.sqlite" || o.stores[1].name != "records.sqlite" {
		return nil, nil, ErrProfileInput
	}
	current, records := o.stores[0], o.stores[1]
	recordsBefore, err := o.authenticatedImage(ctx, records)
	if err != nil {
		return nil, nil, err
	}
	image, err := o.authenticatedImage(ctx, current)
	if err != nil {
		return nil, nil, err
	}
	oldHash := hex.EncodeToString(image.SHA256[:])
	staging := "backups/.restore-" + operation
	destination, err := o.domain.Child(ctx, staging+"/db")
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, destination.root.Close()) }()
	identity, err := o.initializeRebuiltIndex(ctx, destination)
	if err != nil {
		return nil, nil, err
	}
	image, err = checkpointedRestoreImage(ctx, destination, current.name)
	if err != nil {
		return nil, nil, err
	}
	if err := o.bootstrapRestoreAuthority(ctx, current, planRoot, staging, "index", identity.Incarnation, image); err != nil {
		return nil, nil, err
	}
	if err := o.checkpointRestoreOriginal(ctx, current, oldHash); err != nil {
		return nil, nil, err
	}
	recordsAfter, err := o.authenticatedImage(ctx, records)
	if err != nil || recordsAfter != recordsBefore {
		return nil, nil, errors.Join(ErrStateChanged, err)
	}
	return []MaintenanceStore{{"index", o.manifest.IndexIncarnation, oldHash}},
		[]MaintenanceStore{{"index", identity.Incarnation, hex.EncodeToString(image.SHA256[:])}}, o.check(ctx)
}

func (o *quiescentOwner) initializeRebuiltIndex(ctx context.Context, destination *stateDomain) (_ index.Identity, resultErr error) {
	if err := backupCapacity(ctx, destination.root, 16<<20); err != nil {
		return index.Identity{}, err
	}
	const name = "index.sqlite"
	base, err := destination.root.CanonicalPath()
	if err != nil {
		return index.Identity{}, err
	}
	profile := o.profile.manifest
	if err := profileFile(destination.root, name, nil, profile.EngineUID, profile.EngineGID, 0600, false); err != nil {
		return index.Identity{}, err
	}
	db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(base, name)))
	if err != nil {
		return index.Identity{}, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	pool, err := OpenMaintenanceOwner(ctx, o.lease, db, o.scope)
	if err != nil {
		return index.Identity{}, errors.Join(err, db.Close())
	}
	defer func() { resultErr = errors.Join(resultErr, pool.Close()) }()
	identity, err := pool.InitializeIndex(ctx, o.writer, o.operation)
	if err != nil {
		return index.Identity{}, err
	}
	if nativeUIMaps {
		_, err = index.InitializeUIAddon(ctx, db, o.scope, o.lease, index.UIWriteAuthority{
			Writer: o.writer, JobID: o.operation,
			Authorize: func(ctx context.Context, current index.Identity) error {
				if current != identity {
					return ErrStateChanged
				}
				return o.check(ctx)
			},
		})
		if err != nil {
			return index.Identity{}, err
		}
	}
	expected := backupSourceMetadata{Kind: "index", WorktreeID: o.scope, Incarnation: identity.Incarnation}
	err = db.QueryRowContext(ctx, "SELECT schema_version,min_reader_version FROM schema_meta WHERE singleton=1").
		Scan(&expected.SchemaVersion, &expected.MinReaderVersion)
	if err != nil {
		return index.Identity{}, err
	}
	if err := verifyBackupContents(ctx, o, destination, expected, db); err != nil {
		return index.Identity{}, err
	}
	var busy, pages, done int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &done); err != nil || busy != 0 || pages != done {
		return index.Identity{}, errors.Join(ErrStateUnsafe, err)
	}
	return identity, nil
}
