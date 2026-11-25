package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
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

type generationManifestProof struct {
	generation GenerationObservation
	rawDigest  [sha256.Size]byte
	minBytes   int
}

// ReadGeneration resolves an active generation once, or checks an explicitly
// pinned published generation. It validates the immutable captured manifest
// and checks actor/policy and lifecycle both before and after that read. The
// caller must authorize the scope before invoking it and recheck catalog and
// records independently before returning any result.
func (s *Store) ReadGeneration(ctx context.Context, profileID, generationID string, authorize GenerationAuthorizer, maxManifestBytes int) (GenerationObservation, error) {
	return s.readGeneration(ctx, profileID, generationID, authorize, maxManifestBytes, nil)
}

// ReadGenerationWithCoverage reads the published summary when the observation
// matches, under the same transaction, authorization and lease.
func (s *Store) ReadGenerationWithCoverage(ctx context.Context, profileID, generationID string, authorize GenerationAuthorizer,
	maxManifestBytes, maxCoverageBytes int, scanner *policy.Scanner, matches func(GenerationObservation) bool) (GenerationObservation, model.Coverage, error) {
	if scanner == nil || matches == nil || maxCoverageBytes < 1 || maxCoverageBytes > 16<<20 {
		return GenerationObservation{}, model.Coverage{}, ErrInvalidStore
	}
	var coverage model.Coverage
	generation, err := s.readGeneration(ctx, profileID, generationID, authorize, maxManifestBytes, func(tx *sql.Tx, value GenerationObservation, _ []byte) error {
		if !matches(value) {
			return nil
		}
		observed, _, err := readPublishedCoverage(ctx, tx, value.GenerationID, value.State, scanner, maxCoverageBytes)
		coverage = observed
		return err
	})
	if err != nil {
		return GenerationObservation{}, model.Coverage{}, err
	}
	return generation, coverage, nil
}

func (s *Store) readGeneration(ctx context.Context, profileID, generationID string, authorize GenerationAuthorizer,
	maxManifestBytes int, readDetails func(*sql.Tx, GenerationObservation, []byte) error) (output GenerationObservation, resultErr error) {
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
	value.Identity, value.ProfileID = identity, profileID
	if err := s.verifyGenerationManifest(raw, value, maxManifestBytes); err != nil {
		return GenerationObservation{}, err
	}
	if readDetails != nil {
		if err := readDetails(tx, value, raw); err != nil {
			return GenerationObservation{}, err
		}
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
	return value, nil
}

// Reuse only the validation of identical manifest bytes. The live transaction,
// store identity, lease, and both authorizer calls are checked on every read.
func (s *Store) verifyGenerationManifest(raw []byte, value GenerationObservation, maxBytes int) error {
	sum := sha256.Sum256(raw)
	if proof := s.manifestProof.Load(); proof != nil && proof.generation == value &&
		proof.rawDigest == sum && proof.minBytes <= maxBytes {
		return nil
	}
	manifest, err := model.DecodeCaptureManifestJSON(raw, maxBytes)
	if err != nil {
		return errors.Join(ErrCaptureMismatch, err)
	}
	digest, minBytes, err := model.ComputeCaptureDigestAndSize(manifest, maxBytes)
	if err != nil || digest != manifest.SnapshotDigest || manifest.WorktreeID != value.Identity.WorktreeID ||
		manifest.ProfileID != value.ProfileID || manifest.PolicyDigest != value.PolicyDigest ||
		manifest.ProviderDigest != value.ProviderDigest || manifest.SnapshotDigest != value.SnapshotDigest {
		return ErrCaptureMismatch
	}
	s.manifestProof.Store(&generationManifestProof{generation: value, rawDigest: sum, minBytes: max(len(raw), minBytes)})
	return nil
}
