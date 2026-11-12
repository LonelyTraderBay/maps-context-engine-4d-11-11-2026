package index

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type PublicationOutcomeRequest struct {
	GenerationID     string
	JobID            string
	PolicyEpoch      int64
	Snapshot         *snapshot.Snapshot
	Authorize        GenerationAuthorizer
	MaxManifestBytes int
}

type PublicationOutcome struct {
	GenerationID     string
	IndexIncarnation string
	Status           string // building, published_active, published_historical, retired, failed, cancelled, unavailable
}

// PublicationOutcomeByJob observes generation and active pointer in one read
// transaction after an uncertain commit. Unavailable is deliberately not a
// claim that publication never happened: a retained generation may have been
// garbage-collected. The method never retries or mutates a generation.
func (s *Store) PublicationOutcomeByJob(ctx context.Context, request PublicationOutcomeRequest) (output PublicationOutcome, resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || !validUUID(request.GenerationID) || !validUUID(request.JobID) ||
		request.PolicyEpoch < 1 || request.PolicyEpoch > 9007199254740991 || request.Snapshot == nil || request.Authorize == nil || request.MaxManifestBytes < 1 || request.MaxManifestBytes > 64*1024*1024 {
		return PublicationOutcome{}, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return PublicationOutcome{}, ErrInvalidStore
	}
	manifest := request.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, request.MaxManifestBytes) != nil {
		return PublicationOutcome{}, ErrCaptureMismatch
	}
	if err := request.Authorize(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, request.PolicyEpoch); err != nil {
		return PublicationOutcome{}, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return PublicationOutcome{}, err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return PublicationOutcome{}, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			output = PublicationOutcome{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return PublicationOutcome{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return PublicationOutcome{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			output = PublicationOutcome{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, err := inspectStore(ctx, tx, s.identity.WorktreeID)
	if err != nil {
		return PublicationOutcome{}, err
	}
	if version != supportedSchema || identity != s.identity {
		return PublicationOutcome{}, ErrStoreChanged
	}
	var state, job, profile, policyDigest, provider string
	var policyEpoch int64
	var active, digest sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT g.state,g.owner_job_id,g.profile_id,g.policy_digest,g.policy_epoch,g.provider_fingerprint,g.snapshot_digest,a.generation_id FROM generations g LEFT JOIN active_generations a ON a.profile_id=g.profile_id WHERE g.generation_id=?`, request.GenerationID).Scan(&state, &job, &profile, &policyDigest, &policyEpoch, &provider, &digest, &active)
	status := "unavailable"
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PublicationOutcome{}, err
	}
	if err == nil {
		if job != request.JobID || profile != manifest.ProfileID || policyDigest != manifest.PolicyDigest || policyEpoch != request.PolicyEpoch || provider != manifest.ProviderDigest || !digest.Valid || digest.String != manifest.SnapshotDigest {
			return PublicationOutcome{}, ErrGenerationConflict
		}
		if state != "READY" && state != "READY_PARTIAL" && active.Valid && active.String == request.GenerationID {
			return PublicationOutcome{}, ErrPublishIntegrity
		}
		switch state {
		case "BUILDING":
			status = "building"
		case "READY", "READY_PARTIAL":
			if active.Valid && active.String == request.GenerationID {
				status = "published_active"
			} else {
				status = "published_historical"
			}
		case "RETIRED":
			status = "retired"
		case "FAILED":
			status = "failed"
		case "CANCELLED":
			status = "cancelled"
		default:
			return PublicationOutcome{}, ErrPublishIntegrity
		}
	}
	if err := request.Authorize(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, request.PolicyEpoch); err != nil {
		return PublicationOutcome{}, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return PublicationOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return PublicationOutcome{}, err
	}
	return PublicationOutcome{GenerationID: request.GenerationID, IndexIncarnation: s.identity.Incarnation, Status: status}, nil
}
