package records

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	testevidence "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/tests"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const unboundTestEvidence = `{"status":"unbound","reason":"source_capture_not_verified"}`

var ErrTestEvidenceUnavailable = errors.New("test or coverage evidence is unavailable")

type TestEvidenceImportRequest struct {
	Root         *paths.Root
	RelativePath string
	Approval     testevidence.Approval
	Writer       ports.IndexWriterLease
	JobID        string
	Authorize    StateAuthorizer
	AllowUpgrade bool
}

func (s *Store) ImportTestEvidence(ctx context.Context, req TestEvidenceImportRequest) (EvidenceImportResult, error) {
	return s.importEvidence(ctx, evidenceWrite{
		writer: req.Writer, job: req.JobID, authorize: req.Authorize, allowUpgrade: req.AllowUpgrade,
		recheck: func(ctx context.Context, id string) error {
			if err := req.Approval.Recheck(ctx, req.Root, req.RelativePath, id); err != nil {
				return testevidence.ErrApproval
			}
			return nil
		},
	}, func(ctx context.Context) (localEvidence, error) {
		if req.Approval.WorktreeID != s.identity.WorktreeID {
			return localEvidence{}, testevidence.ErrApproval
		}
		report, err := testevidence.Read(ctx, req.Root, req.RelativePath, req.Approval)
		if err != nil {
			return localEvidence{}, err
		}
		return prepareTestEvidence(report)
	})
}

func prepareTestEvidence(report testevidence.Report) (localEvidence, error) {
	meta := report.Metadata()
	raw, err := json.Marshal(meta)
	if err != nil {
		return localEvidence{}, err
	}
	out := localEvidence{id: report.InputHash(), kind: meta.Kind, version: testevidence.SanitizerVersion,
		metadata: string(raw)}
	size := len(`{"metadata":,"tests":[],"coverage":[]}`) + len(raw)
	appendItem := func(kind string, body any) error {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		size += len(payload)
		if len(out.items) != 0 {
			size++ // separator between consecutive items of the report's kind
		}
		if size > testevidence.MaxBytes {
			return testevidence.ErrReport
		}
		out.items = append(out.items, localEvidenceItem{id: testItemID(len(out.items)), kind: kind,
			body: string(payload), binding: unboundTestEvidence})
		return nil
	}
	for _, test := range report.Tests() {
		if err := appendItem("test_result", test); err != nil {
			return localEvidence{}, err
		}
	}
	for _, hit := range report.Coverage() {
		if err := appendItem("coverage_hit", hit); err != nil {
			return localEvidence{}, err
		}
	}
	return out, nil
}

func testItemID(index int) string {
	return fmt.Sprintf("%04d", index)
}

// ReadTestEvidence returns authorized metadata from the records snapshot. Use
// Alignment with the approved capture before relying on any source match.
func (s *Store) ReadTestEvidence(ctx context.Context, id string, authorize StateAuthorizer) (testevidence.Report, State, error) {
	if !model.ValidDigest(id) {
		return testevidence.Report{}, State{}, ErrInvalidStore
	}
	var report testevidence.Report
	state, err := s.readRecords(ctx, authorize, func(tx *sql.Tx, current State) error {
		var version int
		if err := tx.QueryRowContext(ctx, `SELECT schema_version FROM schema_meta WHERE singleton=1`).Scan(&version); err != nil {
			return err
		}
		if version < 2 {
			return ErrTestEvidenceUnavailable
		}
		loaded, err := readTestEvidenceItems(ctx, tx, id, current.Identity.WorktreeID)
		if err != nil {
			return err
		}
		report = loaded
		return nil
	})
	if err != nil {
		return testevidence.Report{}, State{}, err
	}
	return report, state, nil
}

func readTestEvidenceItems(ctx context.Context, tx *sql.Tx, id, worktree string) (testevidence.Report, error) {
	var metadata, kind string
	err := tx.QueryRowContext(ctx, `SELECT artifact_kind,CASE WHEN length(CAST(metadata_json AS BLOB)) <= 4096
		THEN metadata_json ELSE '' END FROM evidence_artifacts
		WHERE artifact_id=? AND artifact_kind IN('test_report','coverage_report') AND input_hash=? AND sanitizer_version=?`,
		id, id, testevidence.SanitizerVersion).Scan(&kind, &metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return testevidence.Report{}, ErrTestEvidenceUnavailable
	}
	if err != nil {
		return testevidence.Report{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,
		CASE WHEN length(CAST(item_id AS BLOB))=4 THEN item_id ELSE '' END,
		CASE WHEN length(CAST(payload_json AS BLOB))<=? THEN payload_json ELSE '' END,
		source_binding_json=? FROM evidence_items WHERE artifact_id=? ORDER BY item_id LIMIT ?`,
		testevidence.MaxBytes, unboundTestEvidence, id, testevidence.MaxItems+1)
	if err != nil {
		return testevidence.Report{}, err
	}
	defer rows.Close()
	var tests, coverage []json.RawMessage
	total := len(metadata)
	for rows.Next() {
		var itemKind, itemID, payload string
		var unbound bool
		if err := rows.Scan(&itemKind, &itemID, &payload, &unbound); err != nil {
			return testevidence.Report{}, err
		}
		count := len(tests) + len(coverage)
		total += len(payload)
		if count >= testevidence.MaxItems || total > testevidence.MaxBytes || itemID != testItemID(count) || !unbound {
			return testevidence.Report{}, ErrIntegrity
		}
		switch itemKind {
		case "test_result":
			tests = append(tests, json.RawMessage(payload))
		case "coverage_hit":
			coverage = append(coverage, json.RawMessage(payload))
		default:
			return testevidence.Report{}, ErrIntegrity
		}
	}
	if err := rows.Err(); err != nil {
		return testevidence.Report{}, err
	}
	report, err := testevidence.DecodeStored(id, json.RawMessage(metadata), tests, coverage)
	m := report.Metadata()
	if err != nil || m.Kind != kind || m.Capture.WorktreeID != worktree {
		return testevidence.Report{}, ErrIntegrity
	}
	return report, nil
}
