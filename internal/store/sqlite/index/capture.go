package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrCaptureLimit    = errors.New("captured source exceeds store budget")
	ErrCaptureMismatch = errors.New("capture does not match the building generation")
	ErrBlobMismatch    = errors.New("existing content blob does not match its identity")
	ErrCommitUnknown   = errors.New("index commit outcome is unknown; reconcile before retry")
	ErrAfterCommit     = errors.New("index write committed but finalization failed")
)

// CaptureAuthorizer verifies the current principal, path and policy version.
// It must be bounded and must not take a catalog lock while the index writer
// is held. The owning coordinator remains responsible for cross-store policy
// revalidation and must not expose the store before T010 wiring.
type CaptureAuthorizer func(ctx context.Context, worktreeID, path, policyDigest string, policyEpoch int64) error

type CaptureWrite struct {
	GenerationID     string
	JobID            string
	PolicyEpoch      int64
	Snapshot         *snapshot.Snapshot
	Scanner          *policy.Scanner
	Authorize        CaptureAuthorizer
	Writer           ports.IndexWriterLease
	MaxManifestBytes int
	MaxSourceBytes   int64
}

type captureBlob struct {
	hash  string
	bytes []byte
}

// StoreCapturedBlobs writes only verified source blobs for an existing
// BUILDING generation. It does not create, publish, or select a generation.
// The caller must hold the shared lifecycle and worktree writer OS leases for
// their full required lifetimes and supply current authorization. No public
// transport may call this before T010 supplies those real implementations.
func (s *Store) StoreCapturedBlobs(ctx context.Context, request CaptureWrite) (resultErr error) {
	if s == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || s.lease == nil || ctx == nil || request.Snapshot == nil || request.Scanner == nil || request.Authorize == nil || request.Writer == nil || !validUUID(request.GenerationID) || !validUUID(request.JobID) || request.PolicyEpoch < 1 || request.PolicyEpoch > 9007199254740991 || request.MaxManifestBytes <= 0 || request.MaxSourceBytes <= 0 {
		return ErrInvalidStore
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
	manifest, paths, blobs, err := prepareCapture(ctx, s.identity.WorktreeID, request)
	if err != nil {
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
	if err := verifyBuildingGeneration(ctx, tx, request, manifest); err != nil {
		return err
	}
	for _, blob := range blobs {
		if err := insertVerifiedBlob(ctx, tx, blob); err != nil {
			return err
		}
	}
	for _, path := range paths {
		if err := request.Authorize(ctx, s.identity.WorktreeID, path, manifest.PolicyDigest, request.PolicyEpoch); err != nil {
			return err
		}
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

func prepareCapture(ctx context.Context, worktreeID string, request CaptureWrite) (model.CaptureManifest, []string, []captureBlob, error) {
	manifest := request.Snapshot.Manifest()
	if manifest.WorktreeID != worktreeID || model.VerifyCaptureDigest(manifest, request.MaxManifestBytes) != nil {
		return model.CaptureManifest{}, nil, nil, ErrCaptureMismatch
	}
	paths := make([]string, 0)
	blobs := make([]captureBlob, 0)
	seen := make(map[string][]byte)
	var total int64
	for _, item := range manifest.Files {
		if item.Status != "captured" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return model.CaptureManifest{}, nil, nil, err
		}
		if err := request.Authorize(ctx, worktreeID, item.Path, manifest.PolicyDigest, request.PolicyEpoch); err != nil {
			return model.CaptureManifest{}, nil, nil, err
		}
		if err := policy.CheckPath(item.Path); err != nil {
			return model.CaptureManifest{}, nil, nil, err
		}
		file, ok := request.Snapshot.File(item.Path)
		if !ok || item.ContentHash == nil || item.ByteLength == nil || file.Path() != item.Path || file.ContentHash() != *item.ContentHash || int64(file.Len()) != *item.ByteLength {
			return model.CaptureManifest{}, nil, nil, ErrCaptureMismatch
		}
		if int64(file.Len()) > request.MaxSourceBytes-total {
			return model.CaptureManifest{}, nil, nil, ErrCaptureLimit
		}
		total += int64(file.Len())
		data := file.Bytes()
		if err := request.Scanner.CheckContent(data); err != nil {
			return model.CaptureManifest{}, nil, nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != *item.ContentHash {
			return model.CaptureManifest{}, nil, nil, ErrCaptureMismatch
		}
		if previous, ok := seen[*item.ContentHash]; ok {
			if !bytes.Equal(previous, data) {
				return model.CaptureManifest{}, nil, nil, ErrBlobMismatch
			}
		} else {
			seen[*item.ContentHash] = data
			blobs = append(blobs, captureBlob{hash: *item.ContentHash, bytes: data})
		}
		paths = append(paths, item.Path)
	}
	return manifest, paths, blobs, nil
}

func verifyBuildingGeneration(ctx context.Context, tx *sql.Tx, request CaptureWrite, manifest model.CaptureManifest) error {
	var state, job, profile, policyDigest, provider string
	var policyEpoch int64
	var existingDigest sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT state,owner_job_id,profile_id,policy_digest,policy_epoch,provider_fingerprint,snapshot_digest FROM generations WHERE generation_id=?`, request.GenerationID).Scan(&state, &job, &profile, &policyDigest, &policyEpoch, &provider, &existingDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCaptureMismatch
	}
	if err != nil {
		return err
	}
	if state != "BUILDING" || job != request.JobID || profile != manifest.ProfileID || policyDigest != manifest.PolicyDigest || policyEpoch != request.PolicyEpoch || provider != manifest.ProviderDigest || (existingDigest.Valid && existingDigest.String != manifest.SnapshotDigest) {
		return ErrCaptureMismatch
	}
	return nil
}

func insertVerifiedBlob(ctx context.Context, tx *sql.Tx, blob captureBlob) error {
	data := blob.bytes
	if data == nil {
		data = []byte{}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO content_blobs(content_hash,bytes,byte_length) VALUES(?,?,?) ON CONFLICT(content_hash) DO NOTHING`, blob.hash, data, len(data))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	if count != 0 {
		return ErrBlobMismatch
	}
	var declared, actual int64
	if err := tx.QueryRowContext(ctx, `SELECT byte_length,length(bytes) FROM content_blobs WHERE content_hash=?`, blob.hash).Scan(&declared, &actual); err != nil {
		return err
	}
	if declared != int64(len(blob.bytes)) || actual != declared {
		return ErrBlobMismatch
	}
	var existing []byte
	if err := tx.QueryRowContext(ctx, `SELECT bytes FROM content_blobs WHERE content_hash=?`, blob.hash).Scan(&existing); err != nil {
		return err
	}
	if !bytes.Equal(existing, blob.bytes) {
		return ErrBlobMismatch
	}
	return nil
}
