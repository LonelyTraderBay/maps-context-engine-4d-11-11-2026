package index

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// ListBuildingForRecovery returns a bounded, authorized page while the caller
// holds newly acquired recovery ownership. No process metadata or timeout is
// used as a substitute for that ownership.
func (s *Store) ListBuildingForRecovery(ctx context.Context, recoveryID string, recovery ports.IndexRecoveryLease, after string, limit int, authorize ports.RecoveryAuthorizer) (output []ports.RecoveryCandidate, resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || recovery == nil || authorize == nil || !validUUID(recoveryID) || after != "" && !validUUID(after) || limit < 1 || limit > 128 || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) {
		return nil, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInvalidStore
	}
	if err := authorize(ctx, s.identity.WorktreeID, ports.RecoveryCandidate{}); err != nil {
		return nil, err
	}
	guard, err := recovery.HoldRecovery(ctx, s.identity.WorktreeID, recoveryID)
	if err != nil {
		return nil, err
	}
	if guard == nil {
		return nil, ErrInvalidStore
	}
	defer func() {
		if err := guard.Close(); err != nil {
			output = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := recovery.CheckRecovery(ctx, s.identity.WorktreeID, recoveryID); err != nil {
		return nil, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return nil, err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			output = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			output = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, err := inspectStore(ctx, tx, s.identity.WorktreeID)
	if err != nil {
		return nil, err
	}
	if version != supportedSchema || identity != s.identity {
		return nil, ErrStoreChanged
	}
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(g.generation_id AS BLOB))<=36 THEN g.generation_id END, CASE WHEN length(CAST(g.owner_job_id AS BLOB))<=36 THEN g.owner_job_id END, g.profile_id,g.policy_digest,g.policy_epoch,g.provider_fingerprint,g.snapshot_digest,EXISTS(SELECT 1 FROM active_generations a WHERE a.generation_id=g.generation_id) FROM generations g WHERE g.state='BUILDING' AND (?='' OR g.generation_id>?) ORDER BY g.generation_id LIMIT ?`, after, after, limit)
	if err != nil {
		return nil, err
	}
	var candidates []ports.RecoveryCandidate
	var scanErr error
	for rows.Next() {
		candidate, active, err := scanRecoveryCandidate(rows)
		if err != nil {
			scanErr = err
			break
		}
		if active || candidate.OwnerJobID == recoveryID {
			scanErr = ErrGenerationConflict
			break
		}
		candidates = append(candidates, candidate)
	}
	scanErr = errors.Join(scanErr, rows.Err(), rows.Close())
	if scanErr != nil {
		return nil, scanErr
	}
	for _, candidate := range candidates {
		if err := authorize(ctx, s.identity.WorktreeID, candidate); err != nil {
			return nil, err
		}
	}
	if err := recovery.CheckRecovery(ctx, s.identity.WorktreeID, recoveryID); err != nil {
		return nil, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if err := authorize(ctx, s.identity.WorktreeID, candidate); err != nil {
			return nil, err
		}
	}
	if err := authorize(ctx, s.identity.WorktreeID, ports.RecoveryCandidate{}); err != nil {
		return nil, err
	}
	if err := recovery.CheckRecovery(ctx, s.identity.WorktreeID, recoveryID); err != nil {
		return nil, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

type recoveryScanner interface{ Scan(...any) error }

func scanRecoveryCandidate(row recoveryScanner) (ports.RecoveryCandidate, bool, error) {
	var candidate ports.RecoveryCandidate
	var generation, job, digest sql.NullString
	var active int
	if err := row.Scan(&generation, &job, &candidate.ProfileID, &candidate.PolicyDigest, &candidate.PolicyEpoch, &candidate.ProviderDigest, &digest, &active); err != nil {
		return candidate, false, err
	}
	if !generation.Valid || !job.Valid {
		return candidate, false, ErrPublishIntegrity
	}
	candidate.GenerationID, candidate.OwnerJobID = generation.String, job.String
	if digest.Valid {
		candidate.SnapshotDigest = digest.String
	}
	if !validRecoveryCandidate(candidate) || active != 0 && active != 1 {
		return candidate, false, ErrPublishIntegrity
	}
	return candidate, active == 1, nil
}

func validRecoveryCandidate(candidate ports.RecoveryCandidate) bool {
	return validUUID(candidate.GenerationID) && validUUID(candidate.OwnerJobID) && validHash(candidate.ProfileID) && validHash(candidate.PolicyDigest) && validHash(candidate.ProviderDigest) && candidate.PolicyEpoch >= 1 && candidate.PolicyEpoch <= 9007199254740991 && (candidate.SnapshotDigest == "" || validHash(candidate.SnapshotDigest))
}

// ObserveRecoveryCandidate reconciles an uncertain recovery commit using the
// exact previously authorized identity. An absent row remains unavailable,
// because later retention may have removed evidence of its prior state.
func (s *Store) ObserveRecoveryCandidate(ctx context.Context, recoveryID string, recovery ports.IndexRecoveryLease, candidate ports.RecoveryCandidate, authorize ports.RecoveryAuthorizer) (output string, resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || recovery == nil || authorize == nil || !validUUID(recoveryID) || !validRecoveryCandidate(candidate) || candidate.OwnerJobID == recoveryID || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) {
		return "", ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return "", ErrInvalidStore
	}
	guard, err := recovery.HoldRecovery(ctx, s.identity.WorktreeID, recoveryID)
	if err != nil {
		return "", err
	}
	if guard == nil {
		return "", ErrInvalidStore
	}
	defer func() {
		if err := guard.Close(); err != nil {
			output = ""
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := authorize(ctx, s.identity.WorktreeID, candidate); err != nil {
		return "", err
	}
	if err := recovery.CheckRecovery(ctx, s.identity.WorktreeID, recoveryID); err != nil {
		return "", err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return "", err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			output = ""
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return "", err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			output = ""
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, err := inspectStore(ctx, tx, s.identity.WorktreeID)
	if err != nil {
		return "", err
	}
	if version != supportedSchema || identity != s.identity {
		return "", ErrStoreChanged
	}
	row := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(g.generation_id AS BLOB))<=36 THEN g.generation_id END, CASE WHEN length(CAST(g.owner_job_id AS BLOB))<=36 THEN g.owner_job_id END, g.profile_id,g.policy_digest,g.policy_epoch,g.provider_fingerprint,g.snapshot_digest,EXISTS(SELECT 1 FROM active_generations a WHERE a.generation_id=g.generation_id) FROM generations g WHERE g.generation_id=?`, candidate.GenerationID)
	current, active, err := scanRecoveryCandidate(row)
	status := "unavailable"
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil {
		if current != candidate {
			return "", ErrGenerationConflict
		}
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM generations WHERE generation_id=?`, candidate.GenerationID).Scan(&state); err != nil {
			return "", err
		}
		if active && state != "READY" && state != "READY_PARTIAL" {
			return "", ErrPublishIntegrity
		}
		switch state {
		case "BUILDING":
			status = "building"
		case "FAILED":
			status = "failed"
		case "CANCELLED":
			status = "cancelled"
		case "RETIRED":
			status = "retired"
		case "READY", "READY_PARTIAL":
			status = "published_historical"
			if active {
				status = "published_active"
			}
		default:
			return "", ErrPublishIntegrity
		}
	}
	if err := authorize(ctx, s.identity.WorktreeID, candidate); err != nil {
		return "", err
	}
	if err := recovery.CheckRecovery(ctx, s.identity.WorktreeID, recoveryID); err != nil {
		return "", err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return status, nil
}

// FailAbandonedGeneration atomically marks one exactly matched BUILDING row
// FAILED. It does not delete facts, blobs, source, catalog, or active pointers.
// An uncertain commit must be reconciled rather than blindly repeated.
func (s *Store) FailAbandonedGeneration(ctx context.Context, recoveryID string, recovery ports.IndexRecoveryLease, candidate ports.RecoveryCandidate, authorize ports.RecoveryAuthorizer) (resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || recovery == nil || authorize == nil || !validUUID(recoveryID) || !validRecoveryCandidate(candidate) || candidate.OwnerJobID == recoveryID || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	guard, err := recovery.HoldRecovery(ctx, s.identity.WorktreeID, recoveryID)
	if err != nil {
		return err
	}
	if guard == nil {
		return ErrInvalidStore
	}
	committed := false
	defer func() {
		if err := guard.Close(); err != nil {
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := recovery.CheckRecovery(ctx, s.identity.WorktreeID, recoveryID); err != nil {
		return err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return err
	}
	if err := authorize(ctx, s.identity.WorktreeID, candidate); err != nil {
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
	var digest any
	if candidate.SnapshotDigest != "" {
		digest = candidate.SnapshotDigest
	}
	result, err := tx.ExecContext(ctx, `UPDATE generations SET state='FAILED',completed_at=? WHERE generation_id=? AND state='BUILDING' AND owner_job_id=? AND profile_id=? AND policy_digest=? AND policy_epoch=? AND provider_fingerprint=? AND snapshot_digest IS ? AND NOT EXISTS(SELECT 1 FROM active_generations a WHERE a.generation_id=generations.generation_id)`, time.Now().UTC().Format(time.RFC3339Nano), candidate.GenerationID, candidate.OwnerJobID, candidate.ProfileID, candidate.PolicyDigest, candidate.PolicyEpoch, candidate.ProviderDigest, digest)
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
	if err := authorize(ctx, s.identity.WorktreeID, candidate); err != nil {
		return err
	}
	if err := recovery.CheckRecovery(ctx, s.identity.WorktreeID, recoveryID); err != nil {
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
