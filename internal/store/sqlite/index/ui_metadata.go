package index

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/uimap"
)

const uiAddonSchema = 4
const maxUIManifest = 16 << 20

var ErrUIConflict = errors.New("immutable UI metadata conflicts with existing identity")

type UIStore struct {
	db       *sql.DB
	lease    ports.LifecycleLease
	identity Identity
}

type UIWriteAuthority struct {
	Writer    ports.IndexWriterLease
	JobID     string
	Authorize func(context.Context, Identity) error
}

func InitializeUIAddon(ctx context.Context, db *sql.DB, worktree string, lease ports.LifecycleLease, request UIWriteAuthority) (Identity, error) {
	store := UIStore{db: db, lease: lease, identity: Identity{WorktreeID: worktree}}
	return store.transact(ctx, request, ports.ExclusiveLease, true, func(tx *sql.Tx, version int, identity Identity) error {
		switch version {
		case 2:
			if _, err := tx.ExecContext(ctx, migration.V3()); err != nil {
				return err
			}
			fallthrough
		case 3:
			if _, err := tx.ExecContext(ctx, migration.V4()); err != nil {
				return err
			}
		case uiAddonSchema:
		default:
			return ErrUnrecognizedStore
		}
		final, found, err := inspectStoreThrough(ctx, tx, worktree, uiAddonSchema)
		if err != nil || found != identity || final != uiAddonSchema {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return ErrUIConflict
		}
		return rows.Err()
	})
}

// OpenUIAddon opts into the sealed V4 metadata capability. Ordinary R1 Open
// still recognizes only V2. A native owner supplies the protected pool/lease.
func OpenUIAddon(ctx context.Context, db *sql.DB, worktree string, lease ports.LifecycleLease) (_ *UIStore, resultErr error) {
	if ctx == nil || !withinPoolLimit(db, 1) || lease == nil || !validUUID(worktree) {
		return nil, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInvalidStore
	}
	if err := lease.Check(ctx, worktree, ports.SharedLease); err != nil {
		return nil, err
	}
	conn, err := borrowConfigured(ctx, db, true)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, releaseConfigured(ctx, conn, true)) }()
	if err := requireWAL(ctx, conn, false); err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	version, identity, err := inspectStoreThrough(ctx, tx, worktree, uiAddonSchema)
	if err != nil {
		return nil, err
	}
	if version != uiAddonSchema {
		return nil, ErrMigrationRequired
	}
	if err := lease.Check(ctx, worktree, ports.SharedLease); err != nil {
		return nil, err
	}
	return &UIStore{db: db, lease: lease, identity: identity}, ctx.Err()
}

func (s *UIStore) transact(ctx context.Context, request UIWriteAuthority, leaseMode ports.LeaseMode, upgrading bool,
	run func(*sql.Tx, int, Identity) error) (_ Identity, resultErr error) {
	if s == nil || ctx == nil || !withinPoolLimit(s.db, 1) || !validUUID(s.identity.WorktreeID) || s.lease == nil ||
		request.Writer == nil || request.Authorize == nil || !validUUID(request.JobID) {
		return Identity{}, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return Identity{}, ErrInvalidStore
	}
	worktree := s.identity.WorktreeID
	if err := s.lease.Check(ctx, worktree, leaseMode); err != nil {
		return Identity{}, err
	}
	guard, err := request.Writer.HoldWriter(ctx, worktree, request.JobID)
	if err != nil {
		return Identity{}, err
	}
	if guard == nil {
		return Identity{}, ErrInvalidStore
	}
	committed := false
	finish := func(err error) {
		if err != nil {
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}
	defer func() { finish(guard.Close()) }()
	conn, err := borrowConfigured(ctx, s.db, false)
	if err != nil {
		return Identity{}, err
	}
	defer func() { finish(releaseConfigured(ctx, conn, false)) }()
	if err := requireWAL(ctx, conn, false); err != nil {
		return Identity{}, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return Identity{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			finish(err)
		}
	}()
	version, identity, err := inspectStoreThrough(ctx, tx, worktree, uiAddonSchema)
	if err != nil {
		return Identity{}, err
	}
	if !upgrading && (version != uiAddonSchema || identity != s.identity) {
		return Identity{}, ErrStoreChanged
	}
	if version < 2 {
		return Identity{}, ErrMigrationRequired
	}
	check := func() error {
		if err := s.lease.Check(ctx, worktree, leaseMode); err != nil {
			return err
		}
		if err := request.Writer.CheckWriter(ctx, worktree, request.JobID); err != nil {
			return err
		}
		return request.Authorize(ctx, identity)
	}
	if err := check(); err != nil {
		return Identity{}, err
	}
	if err := run(tx, version, identity); err != nil {
		return Identity{}, err
	}
	if err := check(); err != nil {
		return Identity{}, err
	}
	if err := tx.Commit(); err != nil {
		return Identity{}, errors.Join(ErrCommitUnknown, err)
	}
	committed = true
	return identity, nil
}

func (s *UIStore) ImportBuildMap(ctx context.Context, request UIWriteAuthority, artifact uimap.ImportedBuild) error {
	manifest, raw, err := artifact.Manifest()
	if err != nil {
		return err
	}
	_, err = s.transact(ctx, request, ports.SharedLease, false, func(tx *sql.Tx, _ int, identity Identity) error {
		if manifest.WorktreeID != identity.WorktreeID {
			return ErrWrongWorktree
		}
		stored, _, lookupErr := readUIBuild(ctx, tx, manifest.BuildMapID)
		if lookupErr == nil {
			if !bytes.Equal(raw, stored) {
				return ErrUIConflict
			}
			return verifyUIAnchors(ctx, tx, manifest)
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ui_build_maps(build_map_id,schema_version,profile_id,provider_version,
			provider_digest,build_config_digest,input_manifest_digest,mode,artifact_hash,metadata_json,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, manifest.BuildMapID, manifest.SchemaVersion, manifest.ProfileID,
			manifest.ProviderVersion, manifest.ProviderDigest, manifest.BuildConfigDigest, manifest.InputManifestDigest,
			manifest.Mode, artifact.ArtifactDigest(), string(raw), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		for _, item := range manifest.Anchors {
			if _, err := tx.ExecContext(ctx, `INSERT INTO ui_source_anchors(build_map_id,anchor_id,kind,path,source_hash,start_byte,end_byte,owner_anchor_id)
				VALUES(?,?,?,?,?,?,?,?)`, manifest.BuildMapID, item.AnchorID, item.Kind, item.Path, item.ContentHash,
				item.Range.StartByte, item.Range.EndByte, item.OwnerAnchorID); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func readUIBuild(ctx context.Context, tx *sql.Tx, buildID string) ([]byte, string, error) {
	var raw, artifact string
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(metadata_json AS BLOB))<=? THEN metadata_json END,
		CASE WHEN length(CAST(artifact_hash AS BLOB))=64 THEN artifact_hash END
		FROM ui_build_maps WHERE build_map_id=?`, maxUIManifest, buildID).Scan(&raw, &artifact)
	if err != nil {
		return nil, "", err
	}
	if !model.ValidDigest(artifact) {
		return nil, "", ErrUIConflict
	}
	return []byte(raw), artifact, nil
}

func verifyUIAnchors(ctx context.Context, tx *sql.Tx, manifest model.BuildManifest) error {
	items := make(map[string]model.AnchorEntry, len(manifest.Anchors))
	for _, item := range manifest.Anchors {
		items[item.AnchorID] = item
	}
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(anchor_id AS BLOB))=64 THEN anchor_id END,
		CASE WHEN length(CAST(kind AS BLOB))<=64 THEN kind END,
		CASE WHEN length(CAST(path AS BLOB))<=16384 THEN path END,
		CASE WHEN length(CAST(source_hash AS BLOB))=64 THEN source_hash END,
		start_byte,end_byte,CASE WHEN owner_anchor_id IS NULL OR length(CAST(owner_anchor_id AS BLOB))=64 THEN owner_anchor_id ELSE '' END
		FROM ui_source_anchors WHERE build_map_id=? LIMIT ?`, manifest.BuildMapID, len(items)+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var actual model.AnchorEntry
		if err := rows.Scan(&actual.AnchorID, &actual.Kind, &actual.Path, &actual.ContentHash, &actual.Range.StartByte, &actual.Range.EndByte, &actual.OwnerAnchorID); err != nil {
			return err
		}
		want, ok := items[actual.AnchorID]
		if !ok || actual.Kind != want.Kind || actual.Path != want.Path || actual.ContentHash != want.ContentHash || actual.Range != want.Range ||
			!sameOptionalString(actual.OwnerAnchorID, want.OwnerAnchorID) {
			return ErrUIConflict
		}
		delete(items, actual.AnchorID)
	}
	if len(items) != 0 {
		return ErrUIConflict
	}
	return rows.Err()
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
