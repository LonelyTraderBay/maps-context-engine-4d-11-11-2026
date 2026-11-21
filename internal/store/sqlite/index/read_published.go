package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var (
	ErrSnapshotUnavailable      = errors.New("published snapshot is unavailable")
	ErrPublishedFileUnavailable = errors.New("published file is unavailable")
)

type PublishedFileRequest struct {
	ProfileID           string
	GenerationID        string // empty selects the active generation once
	Path                string
	AuthorizeGeneration GenerationAuthorizer
	AuthorizePath       CaptureAuthorizer
	Scanner             *policy.Scanner
	MaxManifestBytes    int
	MaxFileBytes        int
}

type PublishedFile struct {
	WorktreeID       string
	IndexIncarnation string
	ProfileID        string
	GenerationID     string
	SnapshotDigest   string
	PolicyDigest     string
	PolicyEpoch      int64
	State            string
	Path             string
	ContentHash      string
	Bytes            []byte
}

// ReadPublishedFile resolves the active or explicit generation and reads the
// matching captured bytes in one short SQLite read transaction. It performs
// no live-workspace freshness claim. Callers must not expose data before this
// method returns successfully and must separately verify current scope epochs.
func (s *Store) ReadPublishedFile(ctx context.Context, request PublishedFileRequest) (output PublishedFile, resultErr error) {
	return s.readPublishedFile(ctx, request, nil)
}

// readPublishedFile optionally reads additional typed data before releasing
// the same read transaction; only package-owned code may supply the callback.
func (s *Store) readPublishedFile(ctx context.Context, request PublishedFileRequest, readFacts func(context.Context, *sql.Tx, PublishedFile) error) (output PublishedFile, resultErr error) {
	if ctx == nil || !s.readable() ||
		!validHash(request.ProfileID) || request.GenerationID != "" && !validUUID(request.GenerationID) ||
		request.AuthorizeGeneration == nil || request.AuthorizePath == nil || request.Scanner == nil ||
		request.MaxManifestBytes < 1 || request.MaxManifestBytes > 64*1024*1024 || request.MaxFileBytes < 1 || request.MaxFileBytes > 64*1024*1024 {
		return PublishedFile{}, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return PublishedFile{}, ErrInvalidStore
	}
	if err := policy.CheckPath(request.Path); err != nil {
		return PublishedFile{}, err
	}
	if err := request.Scanner.CheckContent([]byte(request.Path)); err != nil {
		return PublishedFile{}, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return PublishedFile{}, err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return PublishedFile{}, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			output = PublishedFile{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireReadJournal(ctx, conn, s.image); err != nil {
		return PublishedFile{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return PublishedFile{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			output = PublishedFile{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	_, err = s.inspectPinned(ctx, tx)
	if err != nil {
		return PublishedFile{}, err
	}
	var generation, digest, state, policyDigest, provider string
	var epoch int64
	var manifestLen int64
	query := `SELECT g.generation_id,g.snapshot_digest,g.state,g.policy_digest,g.policy_epoch,g.provider_fingerprint,length(CAST(g.manifest_json AS BLOB)) FROM generations g JOIN active_generations a ON a.profile_id=g.profile_id AND a.generation_id=g.generation_id WHERE g.profile_id=? AND g.state IN ('READY','READY_PARTIAL')`
	args := []any{request.ProfileID}
	if request.GenerationID != "" {
		query = `SELECT generation_id,snapshot_digest,state,policy_digest,policy_epoch,provider_fingerprint,length(CAST(manifest_json AS BLOB)) FROM generations WHERE profile_id=? AND generation_id=? AND state IN ('READY','READY_PARTIAL')`
		args = append(args, request.GenerationID)
	}
	err = tx.QueryRowContext(ctx, query, args...).Scan(&generation, &digest, &state, &policyDigest, &epoch, &provider, &manifestLen)
	if errors.Is(err, sql.ErrNoRows) {
		return PublishedFile{}, ErrSnapshotUnavailable
	}
	if err != nil {
		return PublishedFile{}, err
	}
	if !validUUID(generation) || !validHash(digest) || !validHash(policyDigest) || !validHash(provider) || epoch < 1 || manifestLen < 1 {
		return PublishedFile{}, ErrCaptureMismatch
	}
	if manifestLen > int64(request.MaxManifestBytes) {
		return PublishedFile{}, ErrCaptureLimit
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, request.ProfileID, policyDigest, epoch); err != nil {
		return PublishedFile{}, err
	}
	if err := request.AuthorizePath(ctx, s.identity.WorktreeID, request.Path, policyDigest, epoch); err != nil {
		return PublishedFile{}, err
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT manifest_json FROM generations WHERE generation_id=?`, generation).Scan(&raw); err != nil {
		return PublishedFile{}, err
	}
	if len(raw) != int(manifestLen) {
		return PublishedFile{}, ErrCaptureMismatch
	}
	manifest, err := model.DecodeCaptureManifestJSON(raw, request.MaxManifestBytes)
	if err != nil {
		return PublishedFile{}, ErrCaptureMismatch
	}
	if model.VerifyCaptureDigest(manifest, request.MaxManifestBytes) != nil || manifest.WorktreeID != s.identity.WorktreeID || manifest.ProfileID != request.ProfileID || manifest.PolicyDigest != policyDigest || manifest.ProviderDigest != provider || manifest.SnapshotDigest != digest {
		return PublishedFile{}, ErrCaptureMismatch
	}
	var entry *model.ManifestFile
	for i := range manifest.Files {
		if manifest.Files[i].Path == request.Path {
			entry = &manifest.Files[i]
			break
		}
	}
	if entry == nil || entry.Status != "captured" || entry.ContentHash == nil || entry.ByteLength == nil {
		return PublishedFile{}, ErrPublishedFileUnavailable
	}
	var status, hash string
	var length, blobLength int64
	err = tx.QueryRowContext(ctx, `SELECT f.status,f.content_hash,f.byte_length,length(b.bytes) FROM files f JOIN content_blobs b ON b.content_hash=f.content_hash WHERE f.generation_id=? AND f.path=?`, generation, request.Path).Scan(&status, &hash, &length, &blobLength)
	if errors.Is(err, sql.ErrNoRows) {
		return PublishedFile{}, ErrPublishedFileUnavailable
	}
	if err != nil {
		return PublishedFile{}, err
	}
	if status != "indexed" && status != "parse_error" || hash != *entry.ContentHash || length != *entry.ByteLength || blobLength != length || length < 0 {
		return PublishedFile{}, ErrCaptureMismatch
	}
	if length > int64(request.MaxFileBytes) {
		return PublishedFile{}, ErrCaptureLimit
	}
	var data []byte
	if err := tx.QueryRowContext(ctx, `SELECT b.bytes FROM files f JOIN content_blobs b ON b.content_hash=f.content_hash WHERE f.generation_id=? AND f.path=?`, generation, request.Path).Scan(&data); err != nil {
		return PublishedFile{}, err
	}
	if int64(len(data)) != length {
		return PublishedFile{}, ErrCaptureMismatch
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return PublishedFile{}, ErrBlobMismatch
	}
	if err := request.Scanner.CheckContent(data); err != nil {
		return PublishedFile{}, err
	}
	file := PublishedFile{WorktreeID: s.identity.WorktreeID, IndexIncarnation: s.identity.Incarnation, ProfileID: request.ProfileID, GenerationID: generation, SnapshotDigest: digest, PolicyDigest: policyDigest, PolicyEpoch: epoch, State: state, Path: request.Path, ContentHash: hash, Bytes: data}
	if readFacts != nil {
		if err := readFacts(ctx, tx, file); err != nil {
			return PublishedFile{}, err
		}
	}
	if err := request.AuthorizePath(ctx, s.identity.WorktreeID, request.Path, policyDigest, epoch); err != nil {
		return PublishedFile{}, err
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, request.ProfileID, policyDigest, epoch); err != nil {
		return PublishedFile{}, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return PublishedFile{}, err
	}
	if err := ctx.Err(); err != nil {
		return PublishedFile{}, err
	}
	return file, nil
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
