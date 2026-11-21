package index

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var ErrPublishIntegrity = errors.New("building generation failed publication integrity checks")

type PublishCapturedRequest struct {
	Capture             CaptureWrite
	AuthorizeGeneration GenerationAuthorizer
	Revalidate          func(context.Context, *snapshot.Snapshot) error
}

type publishPlan struct {
	Capture             CaptureWrite
	AuthorizeGeneration GenerationAuthorizer
	Revalidate          func(context.Context, *snapshot.Snapshot) error
	State               string
	AllowParseErrors    bool
	ValidateExtra       func(context.Context, publicationQueryer, model.CaptureManifest) error
	PersistExtra        func(context.Context, *sql.Tx) error
}

// PublishCapturedOnly publishes a captured-file generation as READY_PARTIAL.
// It refuses semantic facts and records that no semantic provider ran. The
// coordinator must hold the writer lease, supply actual full-workspace and
// dependency revalidation, and recheck current catalog/profile authority.
// This deliberately does not implement publication of analyzer results.
func (s *Store) PublishCapturedOnly(ctx context.Context, request PublishCapturedRequest) (resultErr error) {
	if s == nil || ctx == nil || request.Capture.Writer == nil || !validUUID(request.Capture.GenerationID) {
		return ErrInvalidStore
	}
	streamLease, ok := request.Capture.Writer.(ports.IndexStreamLease)
	if !ok {
		return ErrInvalidStore
	}
	reservation, err := streamLease.HoldIndexStream(ctx, s.identity.WorktreeID, request.Capture.JobID)
	if errors.Is(err, ports.ErrIndexStreamBusy) {
		return ErrGenerationConflict
	}
	if err != nil {
		return err
	}
	if reservation == nil {
		return ErrInvalidStore
	}
	defer func() {
		if err := reservation.Close(); err != nil {
			if resultErr == nil {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	sentinel := &providerStageState{}
	s.streamsMu.Lock()
	if s.streams == nil {
		s.streams = make(map[string]*providerStageState)
	}
	if _, exists := s.streams[request.Capture.GenerationID]; exists {
		s.streamsMu.Unlock()
		return ErrGenerationConflict
	}
	if len(s.streams) >= 256 {
		s.streamsMu.Unlock()
		return ErrCaptureLimit
	}
	s.streams[request.Capture.GenerationID] = sentinel
	s.streamsMu.Unlock()
	defer func() {
		s.streamsMu.Lock()
		if s.streams[request.Capture.GenerationID] == sentinel {
			delete(s.streams, request.Capture.GenerationID)
		}
		s.streamsMu.Unlock()
	}()
	plan := publishPlan{Capture: request.Capture, AuthorizeGeneration: request.AuthorizeGeneration, Revalidate: request.Revalidate, State: "READY_PARTIAL",
		ValidateExtra: func(ctx context.Context, conn publicationQueryer, _ model.CaptureManifest) error {
			return checkNoSemanticFacts(ctx, conn, request.Capture.GenerationID)
		},
		PersistExtra: func(ctx context.Context, tx *sql.Tx) error {
			return persistCapturedCoverage(ctx, tx, request.Capture.GenerationID)
		}}
	return s.publish(ctx, plan)
}

func (s *Store) publish(ctx context.Context, request publishPlan) (resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || capture.Writer == nil ||
		!validUUID(capture.GenerationID) || !validUUID(capture.JobID) || capture.PolicyEpoch < 1 || capture.PolicyEpoch > 9007199254740991 || capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 || capture.MaxSourceBytes < 1 || request.AuthorizeGeneration == nil || request.Revalidate == nil || request.ValidateExtra == nil || request.PersistExtra == nil || request.State != "READY" && request.State != "READY_PARTIAL" {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	manifest := capture.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil {
		return ErrCaptureMismatch
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(raw) > capture.MaxManifestBytes {
		return ErrCaptureLimit
	}
	if _, err := model.DecodeCaptureManifestJSON(raw, capture.MaxManifestBytes); err != nil {
		return ErrCaptureMismatch
	}
	committed := false
	guard, err := capture.Writer.HoldWriter(ctx, s.identity.WorktreeID, capture.JobID)
	if err != nil {
		return err
	}
	if guard == nil {
		return ErrInvalidStore
	}
	defer func() {
		if err := guard.Close(); err != nil {
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return err
	}
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return err
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
		return err
	}
	conn, err := borrowConfigured(ctx, s.writer, false)
	if err != nil {
		return err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, false); err != nil {
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return err
	}
	// Structural validation uses an explicit, rollback-only transaction. FTS5's
	// integrity command is spelled INSERT, so a read-only transaction is not
	// sufficient. End this transaction before observing source files; the final
	// publication still compares data_version on this same connection.
	validation, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := validation.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	_, err = s.inspectPinned(ctx, validation)
	if err != nil {
		return err
	}
	// Validation is deliberately outside the short publish transaction. Detect
	// commits from other connections before relying on that validation below.
	var validatedVersion int64
	if err := validation.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&validatedVersion); err != nil {
		return err
	}
	if err := validateCapturedRows(ctx, validation, s.identity.WorktreeID, capture, manifest, request.AllowParseErrors); err != nil {
		return err
	}
	if err := request.ValidateExtra(ctx, validation, manifest); err != nil {
		return err
	}
	if err := validateSearchDocuments(ctx, validation, s.identity.WorktreeID, capture, manifest); err != nil {
		return err
	}
	if err := checkForeignKeys(ctx, validation); err != nil {
		return err
	}
	// FTS5 rank=1 also compares the external-content index to its table.
	if _, err := validation.ExecContext(ctx, `INSERT INTO search_fts(search_fts,rank) VALUES('integrity-check',1)`); err != nil {
		return errors.Join(ErrPublishIntegrity, err)
	}
	if err := validation.Rollback(); err != nil {
		return err
	}
	if err := request.Revalidate(ctx, capture.Snapshot); err != nil {
		return err
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
		return err
	}
	for _, item := range manifest.Files {
		if err := capture.Authorize(ctx, s.identity.WorktreeID, item.Path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	if err := verifyBuildingGeneration(ctx, tx, capture, manifest); err != nil {
		return err
	}
	// verifyBuildingGeneration pins the read snapshot. If a different
	// connection committed while checks ran, reject it; a later competing
	// commit prevents this read transaction from upgrading to a WAL writer.
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&currentVersion); err != nil {
		return err
	}
	if currentVersion != validatedVersion {
		return ErrPublishIntegrity
	}
	if err := request.PersistExtra(ctx, tx); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE generations SET state=?,manifest_json=?,completed_at=? WHERE generation_id=? AND profile_id=? AND state='BUILDING' AND owner_job_id=? AND policy_digest=? AND policy_epoch=? AND snapshot_digest=?`, request.State, string(raw), time.Now().UTC().Format(time.RFC3339Nano), capture.GenerationID, manifest.ProfileID, capture.JobID, manifest.PolicyDigest, capture.PolicyEpoch, manifest.SnapshotDigest)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrGenerationConflict
	}
	result, err = tx.ExecContext(ctx, `INSERT INTO active_generations(profile_id,generation_id) VALUES(?,?) ON CONFLICT(profile_id) DO UPDATE SET generation_id=excluded.generation_id`, manifest.ProfileID, capture.GenerationID)
	if err != nil {
		return err
	}
	count, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrGenerationConflict
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
		return err
	}
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.Join(ErrCommitUnknown, err)
	}
	committed = true
	return nil
}

func persistCapturedCoverage(ctx context.Context, tx *sql.Tx, generation string) error {
	capability := model.Capability{Name: "go_semantics", ProviderID: "not_run", ProviderVersion: "not-run", Status: "unknown"}
	summary, err := json.Marshal(capability)
	if err != nil {
		return err
	}
	if _, err := model.DecodeCapabilityJSON(summary, 4096); err != nil {
		return ErrPublishIntegrity
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO capability_results(generation_id,capability_key,scope_key,status,summary_json) VALUES(?,'go_semantics','workspace','unknown',?)`, generation, string(summary)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO diagnostics(generation_id,diagnostic_id,path,package_key,code,severity,sanitized_message,scope_json) VALUES(?,'PROVIDER_NOT_RUN',NULL,NULL,'PROVIDER_NOT_RUN','warning','No semantic provider result was published.',?)`, generation, `{"scope_description":"workspace"}`)
	return err
}

type publicationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validateCapturedRows(ctx context.Context, db publicationQueryer, worktree string, capture CaptureWrite, manifest model.CaptureManifest, allowParseErrors bool) (resultErr error) {
	rows, err := db.QueryContext(ctx, `SELECT path,status,content_hash,byte_length,reason_code,kind,length((SELECT bytes FROM content_blobs WHERE content_hash=files.content_hash)) FROM files WHERE generation_id=? ORDER BY path`, capture.GenerationID)
	if err != nil {
		return err
	}
	var paths []string
	var total int64
	i := 0
	for rows.Next() {
		var name, status, kind string
		var hash, reason sql.NullString
		var length, blobLength sql.NullInt64
		if err := rows.Scan(&name, &status, &hash, &length, &reason, &kind, &blobLength); err != nil {
			resultErr = err
			break
		}
		if i >= len(manifest.Files) {
			resultErr = ErrPublishIntegrity
			break
		}
		item := manifest.Files[i]
		i++
		if name != item.Path || !validFileKind(kind) || policy.CheckPath(name) != nil || !equalNullString(hash, item.ContentHash) || !equalNullInt(length, item.ByteLength) {
			resultErr = ErrPublishIntegrity
			break
		}
		if err := capture.Scanner.CheckContent([]byte(name)); err != nil {
			resultErr = err
			break
		}
		if err := capture.Authorize(ctx, worktree, name, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			resultErr = err
			break
		}
		if item.Status == "captured" {
			if status != "indexed" && !(allowParseErrors && status == "parse_error" && reason.Valid && reason.String != "") || status == "indexed" && !equalNullString(reason, item.ReasonCode) || !hash.Valid || !length.Valid || !blobLength.Valid || length.Int64 < 0 || length.Int64 != blobLength.Int64 || length.Int64 > capture.MaxSourceBytes-total {
				resultErr = ErrPublishIntegrity
				break
			}
			total += length.Int64
			paths = append(paths, name)
		} else if status != item.Status || hash.Valid || !reason.Valid || !equalNullString(reason, item.ReasonCode) {
			resultErr = ErrPublishIntegrity
			break
		}
	}
	if err := rows.Err(); err != nil {
		resultErr = errors.Join(resultErr, err)
	}
	if err := rows.Close(); err != nil {
		resultErr = errors.Join(resultErr, err)
	}
	if resultErr != nil {
		return resultErr
	}
	if i != len(manifest.Files) {
		return ErrPublishIntegrity
	}
	for _, name := range paths {
		file, ok := capture.Snapshot.File(name)
		if !ok {
			return ErrPublishIntegrity
		}
		var stored []byte
		if err := db.QueryRowContext(ctx, `SELECT b.bytes FROM files f JOIN content_blobs b ON b.content_hash=f.content_hash WHERE f.generation_id=? AND f.path=?`, capture.GenerationID, name).Scan(&stored); err != nil {
			return err
		}
		if !bytes.Equal(stored, file.Bytes()) || file.ContentHash() == "" {
			return ErrPublishIntegrity
		}
		if err := capture.Scanner.CheckContent(stored); err != nil {
			return err
		}
	}
	return nil
}

func equalNullString(actual sql.NullString, expected *string) bool {
	return actual.Valid == (expected != nil) && (expected == nil || actual.String == *expected)
}
func equalNullInt(actual sql.NullInt64, expected *int64) bool {
	return actual.Valid == (expected != nil) && (expected == nil || actual.Int64 == *expected)
}

func checkNoSemanticFacts(ctx context.Context, db publicationQueryer, generation string) error {
	query := `SELECT 1 FROM packages WHERE generation_id=? UNION ALL SELECT 1 FROM symbols WHERE generation_id=? UNION ALL SELECT 1 FROM relations WHERE generation_id=? UNION ALL SELECT 1 FROM diagnostics WHERE generation_id=? UNION ALL SELECT 1 FROM capability_results WHERE generation_id=? UNION ALL SELECT 1 FROM modules WHERE generation_id=? UNION ALL SELECT 1 FROM module_members WHERE generation_id=? LIMIT 1`
	var found int
	err := db.QueryRowContext(ctx, query, generation, generation, generation, generation, generation, generation, generation).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrPublishIntegrity
}

func checkForeignKeys(ctx context.Context, db publicationQueryer) (resultErr error) {
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	if rows.Next() {
		return ErrPublishIntegrity
	}
	return rows.Err()
}
