package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/importtrace"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrTraceUnavailable = errors.New("runtime trace is unavailable")

// ReadTrace returns the exact artifact under the records lease and current
// authority. A caller must never use a partial result after a read error.
func (s *Store) ReadTrace(ctx context.Context, artifactID string, authorize StateAuthorizer) (importtrace.Artifact, State, error) {
	if !model.ValidDigest(artifactID) {
		return importtrace.Artifact{}, State{}, ErrInvalidStore
	}
	var artifact importtrace.Artifact
	state, err := s.readRecords(ctx, authorize, func(tx *sql.Tx, _ State) error {
		var version int
		if err := tx.QueryRowContext(ctx, `SELECT schema_version FROM schema_meta WHERE singleton=1`).Scan(&version); err != nil {
			return err
		}
		if version < 2 {
			return ErrTraceUnavailable
		}
		loaded, err := readTraceItems(ctx, tx, artifactID)
		if err != nil {
			return err
		}
		artifact = loaded
		return nil
	})
	if err != nil {
		return importtrace.Artifact{}, State{}, err
	}
	return artifact, state, nil
}

func readTraceItems(ctx context.Context, tx *sql.Tx, id string) (importtrace.Artifact, error) {
	var metadata string
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(metadata_json AS BLOB)) <= 4096
		THEN metadata_json ELSE '' END FROM evidence_artifacts
		WHERE artifact_id=? AND artifact_kind='runtime_trace' AND input_hash=? AND sanitizer_version=?`,
		id, id, importtrace.SanitizerVersion).Scan(&metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return importtrace.Artifact{}, ErrTraceUnavailable
	}
	if err != nil {
		return importtrace.Artifact{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,
		CASE WHEN length(CAST(item_id AS BLOB))=49 THEN item_id ELSE '' END,
		CASE WHEN length(CAST(payload_json AS BLOB)) <= 8192 THEN payload_json ELSE '' END,
		source_binding_json=? FROM evidence_items WHERE artifact_id=? ORDER BY item_id LIMIT ?`,
		unboundTrace, id, importtrace.MaxSpans+1)
	if err != nil {
		return importtrace.Artifact{}, err
	}
	defer rows.Close()
	values := make([]json.RawMessage, 0)
	total := len(metadata)
	for rows.Next() {
		var kind, itemID, payload string
		var unbound bool
		if err := rows.Scan(&kind, &itemID, &payload, &unbound); err != nil {
			return importtrace.Artifact{}, err
		}
		total += len(payload)
		if len(values) >= importtrace.MaxSpans || total > importtrace.MaxExportBytes || kind != "span" || !unbound {
			return importtrace.Artifact{}, ErrIntegrity
		}
		var key importtrace.SpanKey
		if json.Unmarshal([]byte(payload), &key) != nil || itemID != key.TraceID+":"+key.SpanID {
			return importtrace.Artifact{}, ErrIntegrity
		}
		values = append(values, json.RawMessage(payload))
	}
	if err := rows.Err(); err != nil {
		return importtrace.Artifact{}, err
	}
	artifact, err := importtrace.DecodeStored(id, json.RawMessage(metadata), values)
	if err != nil {
		return importtrace.Artifact{}, ErrIntegrity
	}
	return artifact, nil
}
