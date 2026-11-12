package index

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var ErrGenerationConflict = errors.New("generation already exists or is not owned by this job")

// GenerationAuthorizer checks the current actor, registered root, profile and
// policy revision. It must not acquire a long-held catalog lock.
type GenerationAuthorizer func(context.Context, string, string, string, int64) error

type BeginGenerationRequest struct {
	GenerationID     string
	JobID            string
	PolicyEpoch      int64
	Snapshot         *snapshot.Snapshot
	Writer           ports.IndexWriterLease
	Authorize        GenerationAuthorizer
	MaxManifestBytes int
}

// BeginGeneration pins a capture digest to a new BUILDING row. It does not
// write source bytes, change the active generation, or establish freshness.
// A commit error has unknown outcome: reconcile by generation/job before retry.
func (s *Store) BeginGeneration(ctx context.Context, request BeginGenerationRequest) (resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) ||
		!validUUID(request.GenerationID) || !validUUID(request.JobID) || request.PolicyEpoch < 1 || request.PolicyEpoch > 9007199254740991 ||
		request.Snapshot == nil || request.Writer == nil || request.Authorize == nil || request.MaxManifestBytes < 1 || request.MaxManifestBytes > 64*1024*1024 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	manifest := request.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, request.MaxManifestBytes) != nil {
		return ErrCaptureMismatch
	}
	committed := false
	guard, err := request.Writer.HoldWriter(ctx, s.identity.WorktreeID, request.JobID)
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
	if err := request.Writer.CheckWriter(ctx, s.identity.WorktreeID, request.JobID); err != nil {
		return err
	}
	if err := request.Authorize(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, request.PolicyEpoch); err != nil {
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
	result, err := tx.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,snapshot_digest,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,?,'BUILDING',?,?,?,?,?) ON CONFLICT(generation_id) DO NOTHING`, request.GenerationID, manifest.ProfileID, manifest.SnapshotDigest, request.JobID, manifest.PolicyDigest, request.PolicyEpoch, manifest.ProviderDigest, time.Now().UTC().Format(time.RFC3339Nano))
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
	if err := request.Authorize(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, request.PolicyEpoch); err != nil {
		return err
	}
	if err := request.Writer.CheckWriter(ctx, s.identity.WorktreeID, request.JobID); err != nil {
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
