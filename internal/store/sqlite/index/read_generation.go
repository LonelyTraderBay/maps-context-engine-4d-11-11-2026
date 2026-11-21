package index

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// GenerationObservation is a validated published index identity. It contains
// no source content and is not a cross-store atomic snapshot or access grant.
type GenerationObservation struct {
	Identity       Identity
	ProfileID      string
	GenerationID   string
	SnapshotDigest string
	PolicyDigest   string
	PolicyEpoch    int64
	ProviderDigest string
	State          string
}

// ReadGeneration resolves an active generation once, or checks an explicitly
// pinned published generation. It validates the immutable captured manifest
// and checks actor/policy and lifecycle both before and after that read. The
// caller must authorize the scope before invoking it and recheck catalog and
// records independently before returning any result.
func (s *Store) ReadGeneration(ctx context.Context, profileID, generationID string, authorize GenerationAuthorizer, maxManifestBytes int) (output GenerationObservation, resultErr error) {
	if ctx == nil || !s.readable() || !validHash(profileID) || generationID != "" && !validUUID(generationID) || authorize == nil || maxManifestBytes < 1 || maxManifestBytes > 64*1024*1024 {
		return GenerationObservation{}, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return GenerationObservation{}, ErrInvalidStore
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return GenerationObservation{}, err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return GenerationObservation{}, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			output = GenerationObservation{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireReadJournal(ctx, conn, s.image); err != nil {
		return GenerationObservation{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return GenerationObservation{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			output = GenerationObservation{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	identity, err := s.inspectPinned(ctx, tx)
	if err != nil {
		return GenerationObservation{}, err
	}
	fields := `CASE WHEN length(CAST(g.generation_id AS BLOB))=36 THEN g.generation_id END,CASE WHEN length(CAST(g.snapshot_digest AS BLOB))=64 THEN g.snapshot_digest END,CASE WHEN length(CAST(g.state AS BLOB))<=32 THEN g.state END,CASE WHEN length(CAST(g.policy_digest AS BLOB))=64 THEN g.policy_digest END,g.policy_epoch,CASE WHEN length(CAST(g.provider_fingerprint AS BLOB))=64 THEN g.provider_fingerprint END,length(CAST(g.manifest_json AS BLOB))`
	query := `SELECT ` + fields + ` FROM generations g JOIN active_generations a ON a.profile_id=g.profile_id AND a.generation_id=g.generation_id WHERE g.profile_id=? AND g.state IN ('READY','READY_PARTIAL')`
	args := []any{profileID}
	if generationID != "" {
		query = `SELECT ` + fields + ` FROM generations g WHERE g.profile_id=? AND g.generation_id=? AND g.state IN ('READY','READY_PARTIAL')`
		args = append(args, generationID)
	}
	var value GenerationObservation
	var manifestLength int64
	err = tx.QueryRowContext(ctx, query, args...).Scan(&value.GenerationID, &value.SnapshotDigest, &value.State, &value.PolicyDigest, &value.PolicyEpoch, &value.ProviderDigest, &manifestLength)
	if errors.Is(err, sql.ErrNoRows) {
		return GenerationObservation{}, ErrSnapshotUnavailable
	}
	if err != nil {
		return GenerationObservation{}, errors.Join(ErrCaptureMismatch, err)
	}
	if !validUUID(value.GenerationID) || !validHash(value.SnapshotDigest) || !validHash(value.PolicyDigest) || !validHash(value.ProviderDigest) || value.PolicyEpoch < 1 || value.PolicyEpoch > 9007199254740991 || manifestLength < 1 {
		return GenerationObservation{}, ErrCaptureMismatch
	}
	if manifestLength > int64(maxManifestBytes) {
		return GenerationObservation{}, ErrCaptureLimit
	}
	if err := authorize(ctx, identity.WorktreeID, profileID, value.PolicyDigest, value.PolicyEpoch); err != nil {
		return GenerationObservation{}, err
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT manifest_json FROM generations WHERE generation_id=?`, value.GenerationID).Scan(&raw); err != nil {
		return GenerationObservation{}, err
	}
	if int64(len(raw)) != manifestLength {
		return GenerationObservation{}, ErrCaptureMismatch
	}
	manifest, err := model.DecodeCaptureManifestJSON(raw, maxManifestBytes)
	if err != nil || model.VerifyCaptureDigest(manifest, maxManifestBytes) != nil || manifest.WorktreeID != identity.WorktreeID || manifest.ProfileID != profileID || manifest.PolicyDigest != value.PolicyDigest || manifest.ProviderDigest != value.ProviderDigest || manifest.SnapshotDigest != value.SnapshotDigest {
		return GenerationObservation{}, errors.Join(ErrCaptureMismatch, err)
	}
	if err := authorize(ctx, identity.WorktreeID, profileID, value.PolicyDigest, value.PolicyEpoch); err != nil {
		return GenerationObservation{}, err
	}
	if err := s.lease.Check(ctx, identity.WorktreeID, ports.SharedLease); err != nil {
		return GenerationObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return GenerationObservation{}, err
	}
	value.Identity, value.ProfileID = identity, profileID
	return value, nil
}
