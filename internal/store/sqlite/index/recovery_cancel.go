package index

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type CancelBuildingRequest struct {
	Capture   CaptureWrite
	Stream    ProviderStageStream // zero when no provider stream has begun
	Authorize GenerationAuthorizer
}

// CancelBuildingGeneration is for the current job after its controller has
// stopped the provider. A stream gate prevents a concurrent frame or publish;
// the caller releases that stream only after observing the terminal state.
func (s *Store) CancelBuildingGeneration(ctx context.Context, request CancelBuildingRequest) (resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || s.lease == nil || capture.Writer == nil || capture.Snapshot == nil || request.Authorize == nil || !validUUID(capture.GenerationID) || !validUUID(capture.JobID) || capture.PolicyEpoch < 1 || capture.PolicyEpoch > 9007199254740991 || capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	manifest := capture.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil {
		return ErrCaptureMismatch
	}
	if request.Stream.shared != nil {
		shared := request.Stream.shared
		select {
		case <-shared.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer func() { shared.gate <- struct{}{} }()
		s.streamsMu.Lock()
		registered := s.streams[shared.generationID] == shared
		s.streamsMu.Unlock()
		if !registered || shared.generationID != capture.GenerationID || shared.jobID != capture.JobID || shared.digest != manifest.SnapshotDigest {
			return ErrGenerationConflict
		}
	} else {
		lease, ok := capture.Writer.(ports.IndexStreamLease)
		if !ok {
			return ErrInvalidStore
		}
		reservation, err := lease.HoldIndexStream(ctx, s.identity.WorktreeID, capture.JobID)
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
	}
	guard, err := capture.Writer.HoldWriter(ctx, s.identity.WorktreeID, capture.JobID)
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
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return err
	}
	if err := request.Authorize(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
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
	_, err = s.inspectPinned(ctx, tx)
	if err != nil {
		return err
	}
	if err := verifyBuildingGeneration(ctx, tx, capture, manifest); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE generations SET state='CANCELLED',completed_at=? WHERE generation_id=? AND state='BUILDING' AND owner_job_id=? AND profile_id=? AND policy_digest=? AND policy_epoch=? AND provider_fingerprint=? AND snapshot_digest=? AND NOT EXISTS(SELECT 1 FROM active_generations a WHERE a.generation_id=generations.generation_id)`, time.Now().UTC().Format(time.RFC3339Nano), capture.GenerationID, capture.JobID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch, manifest.ProviderDigest, manifest.SnapshotDigest)
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
	if err := request.Authorize(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
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
