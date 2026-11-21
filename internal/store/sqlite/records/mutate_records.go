package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type MutationRequest struct {
	Mutation        model.MutationIdentity
	RecordID        string
	ExpectedVersion int64
	Action          string
	Reason          string
	Mapping         *model.UIMappingPayload
	Writer          ports.IndexWriterLease
	JobID           string
	Authorize       StateAuthorizer
	CheckEvidence   func(context.Context, model.UIMappingPayload) error
	Expected        *State
}

type MutationResult struct {
	Record   Version
	State    State
	Replayed bool
}

func (s *Store) MutateMapping(ctx context.Context, request MutationRequest) (out MutationResult, resultErr error) {
	if request.CheckEvidence == nil || (request.Action == "propose") != (request.Mapping != nil) {
		return out, ErrInvalidStore
	}
	return s.mutate(ctx, request, "ui_mapping", func(worktree string) (Version, error) {
		body, err := json.Marshal(request.Mapping)
		if err != nil {
			return Version{}, ErrInvalidStore
		}
		mapping, err := model.DecodeUIMappingPayloadJSON(body, maxRecordBytes)
		if err != nil || mapping.SourceRef.WorktreeID != worktree {
			return Version{}, ErrInvalidStore
		}
		return Version{UIMapping: &mapping, SourceRefs: []model.SourceRef{mapping.SourceRef}}, nil
	}, func(ctx context.Context, record Version) error {
		if record.UIMapping == nil {
			return ErrIntegrity
		}
		return request.CheckEvidence(ctx, *record.UIMapping)
	})
}

// mutate appends one version and atomically advances the head, epoch and
// receipt. The server supplies the principal, policy and held OS writer.
// Authorization and evidence callbacks may not reenter this records pool.
func (s *Store) mutate(ctx context.Context, request MutationRequest, kind string, propose func(string) (Version, error),
	checkEvidence func(context.Context, Version) error) (out MutationResult, resultErr error) {
	if s == nil || s.image || s.db == nil || s.lease == nil || ctx == nil || s.db.Stats().MaxOpenConnections != 1 ||
		request.Writer == nil || request.Authorize == nil || !uuid(request.JobID) ||
		!uuid(request.Mutation.MutationID) || len(request.Mutation.PrincipalID) > 1024 || !utf8.ValidString(request.Reason) ||
		request.Reason == "" || utf8.RuneCountInString(request.Reason) > 2048 {
		return out, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return out, ErrInvalidStore
	}
	if _, err := model.CheckMutationReplay(nil, request.Mutation); err != nil {
		return out, ErrInvalidStore
	}
	if request.Action == "propose" {
		if request.RecordID != "" || request.ExpectedVersion != 0 {
			return out, ErrInvalidStore
		}
	} else if !uuid(request.RecordID) || request.ExpectedVersion < 1 ||
		request.ExpectedVersion >= exactIntegerMax {
		return out, ErrInvalidStore
	}
	if _, err := nextRecordStatus(request.Action, "proposed"); err != nil {
		return out, err
	}
	if err := request.Authorize(ctx, s.identity); err != nil {
		return out, err
	}
	worktree := s.identity.WorktreeID
	guard, err := request.Writer.HoldWriter(ctx, worktree, request.JobID)
	if err != nil {
		return out, err
	}
	if guard == nil {
		return out, ErrInvalidStore
	}
	committed := false
	finish := func(err error) {
		if err != nil {
			out = MutationResult{}
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}
	defer func() { finish(guard.Close()) }()
	check := func() error {
		if err := s.lease.Check(ctx, worktree, ports.SharedLease); err != nil {
			return err
		}
		if err := request.Writer.CheckWriter(ctx, worktree, request.JobID); err != nil {
			return err
		}
		return request.Authorize(ctx, s.identity)
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
	version, identity, epoch, err := inspect(ctx, tx, worktree)
	if err != nil || version == 0 || identity != s.identity {
		return out, errors.Join(ErrStoreChanged, err)
	}
	replay, err := mutationReplay(ctx, tx, worktree, request.Mutation)
	if err != nil {
		return out, err
	}
	if replay != nil {
		if replay.Kind != kind {
			return out, model.ErrRecordConflict
		}
		if err := check(); err != nil {
			return out, err
		}
		return MutationResult{Record: *replay, State: State{Identity: identity, RecordEpoch: epoch}, Replayed: true}, ctx.Err()
	}
	if request.Expected != nil && (request.Expected.Identity != identity || request.Expected.RecordEpoch != epoch) {
		return out, ErrRecordChanged
	}
	if epoch >= exactIntegerMax {
		return out, ErrLimit
	}
	record, err := prepareRecord(ctx, tx, worktree, request, kind, propose)
	if err != nil {
		return out, err
	}
	if record.DeclaredStatus == "approved" || record.DeclaredStatus == "proposed" {
		if err := checkEvidence(ctx, record); err != nil {
			return out, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := persistRecord(ctx, tx, request, record, now, epoch); err != nil {
		return out, err
	}
	if record.DeclaredStatus == "approved" || record.DeclaredStatus == "proposed" {
		// Repeat source authority just before publication. Explicit revocation
		// and supersession remain possible once a source snapshot is gone.
		if err := checkEvidence(ctx, record); err != nil {
			return out, err
		}
	}
	if err := check(); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, errors.Join(ErrCommitUnknown, err)
	}
	committed = true
	return MutationResult{Record: record, State: State{Identity: identity, RecordEpoch: epoch + 1}}, nil
}

func mutationReplay(ctx context.Context, tx *sql.Tx, worktree string, incoming model.MutationIdentity) (*Version, error) {
	var existing model.MutationIdentity
	var recordID string
	var version int64
	existing.MutationID = incoming.MutationID
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(principal_id AS BLOB)) BETWEEN 1 AND 1024 THEN principal_id END,
		CASE WHEN length(CAST(request_hash AS BLOB))=64 THEN request_hash END,
		CASE WHEN length(CAST(record_id AS BLOB))=36 THEN record_id END,result_version
		FROM mutation_receipts WHERE client_mutation_id=?`, incoming.MutationID).
		Scan(&existing.PrincipalID, &existing.RequestHash, &recordID, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Join(ErrIntegrity, err)
	}
	if _, err := model.CheckMutationReplay(&existing, incoming); err != nil {
		return nil, err
	}
	record, err := scanVersion(tx.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM record_versions v WHERE v.record_id=? AND v.version=?`,
		recordID, version), worktree)
	if err != nil {
		return nil, errors.Join(ErrIntegrity, err)
	}
	return &record, nil
}

func nextRecordStatus(action, previous string) (string, error) {
	switch action {
	case "propose":
		return "proposed", nil
	case "approve":
		if previous == "proposed" {
			return "approved", nil
		}
	case "revoke":
		if previous == "approved" || previous == "proposed" {
			return "revoked", nil
		}
	case "supersede":
		if previous == "approved" || previous == "proposed" {
			return "superseded", nil
		}
	default:
		return "", ErrInvalidStore
	}
	return "", model.ErrRecordConflict
}

func prepareRecord(ctx context.Context, tx *sql.Tx, worktree string, request MutationRequest, kind string, propose func(string) (Version, error)) (Version, error) {
	var record Version
	if request.Action == "propose" {
		proposed, err := propose(worktree)
		if err != nil {
			return Version{}, err
		}
		record = proposed
		record.Version, record.Kind, record.DeclaredStatus = 1, kind, "proposed"
		record.RecordID, err = newUUID()
		if err != nil {
			return Version{}, err
		}
	} else {
		head, err := scanVersion(tx.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM record_heads h
			JOIN record_versions v ON v.record_id=h.record_id AND v.version=h.version WHERE h.record_id=?`, request.RecordID), worktree)
		if err != nil {
			return Version{}, err
		}
		if head.Version != request.ExpectedVersion || head.Kind != kind || (kind == "state_model" && !head.StateModelSource) {
			return Version{}, model.ErrRecordConflict
		}
		record = head
		record.Version++
		record.DeclaredStatus, err = nextRecordStatus(request.Action, head.DeclaredStatus)
		if err != nil {
			return Version{}, err
		}
	}
	return record, nil
}

func persistRecord(ctx context.Context, tx *sql.Tx, request MutationRequest, record Version, now string, epoch int64) error {
	payload, digest, err := recordPayload(record)
	if err != nil {
		return err
	}
	refs, err := json.Marshal(record.SourceRefs)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO record_versions(record_id,version,record_kind,declared_status,payload_json,source_refs_json,actor_id,reason,payload_hash,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, record.RecordID, record.Version, record.Kind, record.DeclaredStatus, string(payload), string(refs),
		request.Mutation.PrincipalID, request.Reason, digest, now); err != nil {
		return err
	}
	if record.Version == 1 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO record_heads(record_id,version) VALUES(?,1)`, record.RecordID); err != nil {
			return err
		}
	} else {
		updated, err := tx.ExecContext(ctx, `UPDATE record_heads SET version=? WHERE record_id=? AND version=?`, record.Version, record.RecordID, request.ExpectedVersion)
		if err := exactlyOne(updated, err); err != nil {
			return err
		}
	}
	updated, err := tx.ExecContext(ctx, `UPDATE records_meta SET record_epoch=? WHERE singleton=1 AND record_epoch=?`, epoch+1, epoch)
	if err := exactlyOne(updated, err); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO mutation_receipts(client_mutation_id,principal_id,request_hash,record_id,result_version,created_at)
		VALUES(?,?,?,?,?,?)`, request.Mutation.MutationID, request.Mutation.PrincipalID, request.Mutation.RequestHash, record.RecordID, record.Version, now)
	return err
}

func recordPayload(record Version) ([]byte, string, error) {
	if record.Kind == "state_model" {
		return stateModelPayload(record.SourceRefs)
	}
	if record.UIMapping == nil {
		return nil, "", ErrIntegrity
	}
	payload, err := json.Marshal(record.UIMapping)
	if err != nil {
		return nil, "", err
	}
	digest, err := model.UIMappingPayloadDigest(*record.UIMapping, maxRecordBytes)
	return payload, digest, err
}

func exactlyOne(change sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := change.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return model.ErrRecordConflict
	}
	return nil
}
