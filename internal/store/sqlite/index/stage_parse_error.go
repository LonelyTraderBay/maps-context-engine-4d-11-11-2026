package index

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type ParseFailure struct {
	Path         string
	DiagnosticID string
}

type MarkParseErrorsRequest struct {
	Capture  CaptureWrite
	Failures []ParseFailure
}

// MarkParseErrors changes only the result projection for captured files. Each
// path must already have a staged error diagnostic; the capture manifest and
// digest remain immutable. A replay is a conflict and requires reconciliation.
func (s *Store) MarkParseErrors(ctx context.Context, request MarkParseErrorsRequest) (resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || capture.Writer == nil ||
		!validUUID(capture.GenerationID) || !validUUID(capture.JobID) || capture.PolicyEpoch < 1 || capture.PolicyEpoch > 9007199254740991 || capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 || len(request.Failures) < 1 || len(request.Failures) > 256 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	manifest := capture.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil {
		return ErrCaptureMismatch
	}
	seen := map[string]bool{}
	for _, failure := range request.Failures {
		if seen[failure.Path] || policy.CheckPath(failure.Path) != nil || len(failure.DiagnosticID) > 768 || !utf8.ValidString(failure.DiagnosticID) || utf8.RuneCountInString(failure.DiagnosticID) < 1 || utf8.RuneCountInString(failure.DiagnosticID) > 192 {
			return ErrCaptureMismatch
		}
		seen[failure.Path] = true
		if err := capture.Scanner.CheckContent([]byte(failure.Path)); err != nil {
			return err
		}
		if err := capture.Scanner.CheckContent([]byte(failure.DiagnosticID)); err != nil {
			return err
		}
		if err := capture.Authorize(ctx, s.identity.WorktreeID, failure.Path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
		if _, ok := capture.Snapshot.File(failure.Path); !ok {
			return ErrCaptureMismatch
		}
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
	version, identity, err := inspectStore(ctx, tx, s.identity.WorktreeID)
	if err != nil {
		return err
	}
	if version != supportedSchema || identity != s.identity {
		return ErrStoreChanged
	}
	if err := verifyBuildingGeneration(ctx, tx, capture, manifest); err != nil {
		return err
	}
	for _, failure := range request.Failures {
		file, _ := capture.Snapshot.File(failure.Path)
		var code string
		err := tx.QueryRowContext(ctx, `SELECT code FROM diagnostics WHERE generation_id=? AND diagnostic_id=? AND path=? AND severity='error'`, capture.GenerationID, failure.DiagnosticID, failure.Path).Scan(&code)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCaptureMismatch
		}
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE files SET status='parse_error',reason_code=? WHERE generation_id=? AND path=? AND status='indexed' AND content_hash=?`, code, capture.GenerationID, failure.Path, file.ContentHash())
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
	}
	for _, failure := range request.Failures {
		if err := capture.Authorize(ctx, s.identity.WorktreeID, failure.Path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
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
