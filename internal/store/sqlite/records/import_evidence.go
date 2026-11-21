package records

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	migration "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/migrations/records"
)

const maxEvidenceArtifacts = 128

var ErrEvidenceMigrationRequired = errors.New("runtime evidence import requires an approved records schema upgrade")

type EvidenceImportResult struct {
	ArtifactID  string
	RecordEpoch int64
	ItemCount   int
	Replayed    bool
}

type evidenceWrite struct {
	writer       ports.IndexWriterLease
	job          string
	authorize    StateAuthorizer
	allowUpgrade bool
	recheck      func(context.Context, string) error
}

// A localEvidence value is assembled only from a validated artifact decoder.
type localEvidence struct {
	id, kind, version, metadata string
	items                       []localEvidenceItem
}

type localEvidenceItem struct {
	id, kind, body, binding string
}

func (s *Store) importEvidence(ctx context.Context, req evidenceWrite,
	admit func(context.Context) (localEvidence, error)) (out EvidenceImportResult, resultErr error) {
	if s == nil || s.image || s.db == nil || s.lease == nil || ctx == nil ||
		req.authorize == nil || req.writer == nil || !uuid(req.job) {
		return out, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return out, ErrInvalidStore
	}
	mode := ports.SharedLease
	if req.allowUpgrade {
		mode = ports.ExclusiveLease
	}
	name := s.identity.WorktreeID
	if err := s.lease.Check(ctx, name, mode); err != nil {
		return out, err
	}
	if err := req.authorize(ctx, s.identity); err != nil {
		return out, err
	}
	artifact, err := admit(ctx)
	if err != nil {
		return out, err
	}
	guard, err := req.writer.HoldWriter(ctx, name, req.job)
	if err != nil {
		return out, err
	}
	if guard == nil {
		return out, ErrInvalidStore
	}
	committed := false
	finish := func(err error) {
		if err != nil {
			out = EvidenceImportResult{}
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}
	defer func() { finish(guard.Close()) }()
	check := func() error {
		if err := s.lease.Check(ctx, name, mode); err != nil {
			return err
		}
		if err := req.writer.CheckWriter(ctx, name, req.job); err != nil {
			return err
		}
		if err := req.authorize(ctx, s.identity); err != nil {
			return err
		}
		if err := req.recheck(ctx, artifact.id); err != nil {
			return err
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return out, err
	}
	conn, err := borrow(ctx, s.db, false)
	if err != nil {
		return out, err
	}
	defer func() { finish(release(ctx, conn, false)) }()
	if err := wal(ctx, conn, false); err != nil {
		return out, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			finish(err)
		}
	}()
	version, identity, epoch, err := inspect(ctx, tx, name)
	if err != nil || version == 0 || identity != s.identity {
		return out, errors.Join(ErrStoreChanged, err)
	}
	if version == 1 {
		if !req.allowUpgrade {
			return out, ErrEvidenceMigrationRequired
		}
		if _, err := tx.ExecContext(ctx, migration.V2()); err != nil {
			return out, err
		}
		upgraded, observed, previousEpoch, err := inspect(ctx, tx, name)
		if err != nil || upgraded != 2 || observed != identity || previousEpoch != epoch {
			return out, errors.Join(ErrStoreChanged, err)
		}
	}
	replayed, err := insertEvidence(ctx, tx, artifact, epoch)
	if err != nil {
		return out, err
	}
	if err := check(); err != nil {
		return out, err
	}
	if !replayed {
		if err := tx.Commit(); err != nil {
			return out, errors.Join(ErrCommitUnknown, err)
		}
		epoch++
		committed = true
	}
	return EvidenceImportResult{ArtifactID: artifact.id, RecordEpoch: epoch, ItemCount: len(artifact.items), Replayed: replayed}, nil
}

func insertEvidence(ctx context.Context, tx *sql.Tx, artifact localEvidence, epoch int64) (bool, error) {
	id := artifact.id
	var kind, input, sanitizer, metadata string
	err := tx.QueryRowContext(ctx, `SELECT artifact_kind,input_hash,sanitizer_version,
		CASE WHEN length(CAST(metadata_json AS BLOB)) <= 4096 THEN metadata_json ELSE '' END
		FROM evidence_artifacts WHERE artifact_id=?`, id).Scan(&kind, &input, &sanitizer, &metadata)
	if err == nil {
		if kind != artifact.kind || input != id || sanitizer != artifact.version || metadata != artifact.metadata {
			return false, ErrIntegrity
		}
		if err := verifyEvidenceItems(ctx, tx, id, artifact.items); err != nil {
			return false, err
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM evidence_artifacts LIMIT ?)`,
		maxEvidenceArtifacts).Scan(&count); err != nil {
		return false, err
	}
	if count >= maxEvidenceArtifacts || epoch >= exactIntegerMax {
		return false, ErrLimit
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_artifacts
		(artifact_id,artifact_kind,input_hash,sanitizer_version,metadata_json,created_at)
		VALUES(?,?,?,?,?,?)`, id, artifact.kind, id, artifact.version, artifact.metadata,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return false, err
	}
	for _, item := range artifact.items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_items
			(artifact_id,item_id,kind,payload_json,source_binding_json) VALUES(?,?,?,?,?)`,
			id, item.id, item.kind, item.body, item.binding); err != nil {
			return false, err
		}
	}
	updated, err := tx.ExecContext(ctx, `UPDATE records_meta SET record_epoch=? WHERE singleton=1 AND record_epoch=?`, epoch+1, epoch)
	if err != nil {
		return false, err
	}
	if rows, err := updated.RowsAffected(); err != nil || rows != 1 {
		return false, errors.Join(ErrStoreChanged, err)
	}
	return false, nil
}

func verifyEvidenceItems(ctx context.Context, tx *sql.Tx, artifactID string, items []localEvidenceItem) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM
		(SELECT 1 FROM evidence_items WHERE artifact_id=? LIMIT ?)`, artifactID, len(items)+1).Scan(&count); err != nil {
		return err
	}
	if count != len(items) {
		return ErrIntegrity
	}
	for _, item := range items {
		var match int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM evidence_items WHERE artifact_id=? AND item_id=?
			AND kind=? AND payload_json=? AND source_binding_json=?`, artifactID, item.id, item.kind,
			item.body, item.binding).Scan(&match); err != nil || match != 1 {
			return errors.Join(ErrIntegrity, err)
		}
	}
	return nil
}
