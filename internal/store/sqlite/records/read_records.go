package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var (
	ErrRecordMissing = errors.New("record version is unavailable")
	ErrRecordChanged = errors.New("record epoch or incarnation changed")
)

const maxRecordBytes = 256 << 10

type Version = ports.StoredRecord

type VersionRead struct {
	Record Version
	State  State
}

type HeadPage struct {
	Records []Version
	State   State
	HasMore bool
}

type HeadPageRequest struct {
	AfterID   string
	Kind      string
	Limit     int
	Expected  *State
	Authorize StateAuthorizer
}

const versionColumns = `
 CASE WHEN length(CAST(v.record_id AS BLOB))=36 THEN v.record_id END,
 v.version,v.record_kind,v.declared_status,
 CASE WHEN length(CAST(v.source_refs_json AS BLOB))<=262144 THEN v.source_refs_json END,
 CASE WHEN v.record_kind='ui_mapping' AND length(CAST(v.payload_json AS BLOB))<=262144 THEN v.payload_json END,
 CASE WHEN length(CAST(v.payload_hash AS BLOB))=64 THEN v.payload_hash END
`

type rowScanner interface{ Scan(...any) error }

func scanVersion(row rowScanner, worktree string) (Version, error) {
	var value Version
	var sources, digest string
	var payload sql.NullString
	if err := row.Scan(&value.RecordID, &value.Version, &value.Kind, &value.DeclaredStatus, &sources, &payload, &digest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Version{}, ErrRecordMissing
		}
		return Version{}, errors.Join(ErrIntegrity, err)
	}
	if !uuid(value.RecordID) || value.Version < 1 || value.Version > exactIntegerMax || !knownKind(value.Kind) || !model.ValidDigest(digest) {
		return Version{}, ErrIntegrity
	}
	switch value.DeclaredStatus {
	case "proposed", "approved", "revoked", "superseded":
	default:
		return Version{}, ErrIntegrity
	}
	if _, err := model.DecodeIdentityJSON([]byte(sources), maxRecordBytes); err != nil {
		return Version{}, ErrIntegrity
	}
	var refs []json.RawMessage
	if json.Unmarshal([]byte(sources), &refs) != nil || refs == nil || len(refs) > 128 {
		return Version{}, ErrIntegrity
	}
	value.SourceRefs = make([]model.SourceRef, 0, len(refs))
	for _, item := range refs {
		ref, err := model.DecodeSourceRefJSON(item, maxRecordBytes)
		if err != nil {
			return Version{}, ErrIntegrity
		}
		value.SourceRefs = append(value.SourceRefs, ref)
	}
	if value.Kind != "ui_mapping" {
		return value, nil
	}
	if !payload.Valid || len(value.SourceRefs) != 1 || value.SourceRefs[0].WorktreeID != worktree {
		return Version{}, ErrIntegrity
	}
	mapping, err := model.DecodeUIMappingPayloadJSON([]byte(payload.String), maxRecordBytes)
	if err != nil || value.SourceRefs[0] != mapping.SourceRef {
		return Version{}, ErrIntegrity
	}
	got, err := model.UIMappingPayloadDigest(mapping, maxRecordBytes)
	if err != nil || got != digest {
		return Version{}, ErrIntegrity
	}
	value.UIMapping = &mapping
	return value, nil
}

func knownKind(kind string) bool {
	switch kind {
	case "decision", "constraint", "vocabulary", "ui_mapping", "intended_architecture", "state_model":
		return true
	}
	return false
}

// ReadVersion looks up an explicit immutable version, or the current head
// when version is zero. The caller authorizes source before returning a body.
func (s *Store) ReadVersion(ctx context.Context, recordID string, version int64, authorize StateAuthorizer) (VersionRead, error) {
	if !uuid(recordID) || version < 0 || version > exactIntegerMax {
		return VersionRead{}, ErrInvalidStore
	}
	var out VersionRead
	state, err := s.readRecords(ctx, authorize, func(tx *sql.Tx, state State) error {
		row := tx.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM record_versions v
			WHERE v.record_id=? AND v.version=CASE WHEN ?=0 THEN (SELECT version FROM record_heads WHERE record_id=?) ELSE ? END`,
			recordID, version, recordID, version)
		var err error
		out.Record, err = scanVersion(row, state.Identity.WorktreeID)
		return err
	})
	if err != nil {
		return VersionRead{}, err
	}
	out.State = state
	return out, nil
}

// ReadHeadPage returns one bounded stable keyset page under a single epoch.
func (s *Store) ReadHeadPage(ctx context.Context, request HeadPageRequest) (HeadPage, error) {
	if request.Limit < 1 || request.Limit > 100 ||
		request.AfterID != "" && !uuid(request.AfterID) || request.Kind != "" && !knownKind(request.Kind) {
		return HeadPage{}, ErrInvalidStore
	}
	out := HeadPage{Records: make([]Version, 0, request.Limit)}
	state, err := s.readRecords(ctx, request.Authorize, func(tx *sql.Tx, state State) error {
		if request.Expected != nil && (state.Identity != request.Expected.Identity || state.RecordEpoch != request.Expected.RecordEpoch) {
			return ErrRecordChanged
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+versionColumns+` FROM record_heads h
			LEFT JOIN record_versions v ON v.record_id=h.record_id AND v.version=h.version
			WHERE h.record_id>? AND (?='' OR v.record_kind=? OR v.record_kind IS NULL)
			ORDER BY h.record_id LIMIT ?`, request.AfterID, request.Kind, request.Kind, request.Limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			value, err := scanVersion(rows, state.Identity.WorktreeID)
			if err != nil {
				return err
			}
			if len(out.Records) == request.Limit {
				out.HasMore = true
				break
			}
			out.Records = append(out.Records, value)
		}
		return rows.Err()
	})
	if err != nil {
		return HeadPage{}, err
	}
	out.State = state
	return out, nil
}

func (s *Store) readRecords(ctx context.Context, authorize StateAuthorizer, visit func(*sql.Tx, State) error) (_ State, resultErr error) {
	if s == nil || s.db == nil || s.lease == nil || ctx == nil || authorize == nil || s.db.Stats().MaxOpenConnections != 1 {
		return State{}, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return State{}, ErrInvalidStore
	}
	if err := authorize(ctx, s.identity); err != nil {
		return State{}, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return State{}, err
	}
	conn, err := borrow(ctx, s.db, true)
	if err != nil {
		return State{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, release(ctx, conn, true)) }()
	if err := s.requireReadJournal(ctx, conn); err != nil {
		return State{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return State{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, epoch, err := inspect(ctx, tx, s.identity.WorktreeID)
	if err != nil || version != 1 || identity != s.identity {
		return State{}, errors.Join(ErrStoreChanged, err)
	}
	state := State{Identity: identity, RecordEpoch: epoch}
	if err := visit(tx, state); err != nil {
		return State{}, err
	}
	if err := s.lease.Check(ctx, identity.WorktreeID, ports.SharedLease); err != nil {
		return State{}, err
	}
	if err := authorize(ctx, identity); err != nil {
		return State{}, err
	}
	return state, ctx.Err()
}
