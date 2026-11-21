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

// FileClassification is supplied by the coordinator for one manifest entry.
// It is metadata, not a claim that semantic analysis is complete.
type FileClassification struct {
	Path      string
	Kind      string
	Generated bool
}

type StageFilesRequest struct {
	Capture CaptureWrite
	Offset  int
	Files   []FileClassification
}

type preparedFile struct {
	manifest       model.ManifestFile
	classification FileClassification
	bytes          []byte
}

// StageFiles writes at most 256 matching manifest entries and their verified
// blobs in a single BUILDING transaction. Replaying an existing path is a
// conflict; reconcile uncertain commits by generation/job before retrying.
func (s *Store) StageFiles(ctx context.Context, request StageFilesRequest) (resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) ||
		capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || capture.Writer == nil ||
		!validUUID(capture.GenerationID) || !validUUID(capture.JobID) || capture.PolicyEpoch < 1 || capture.PolicyEpoch > 9007199254740991 ||
		capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 || capture.MaxSourceBytes < 1 ||
		request.Offset < 0 || len(request.Files) < 1 || len(request.Files) > 256 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	manifest := capture.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil ||
		request.Offset > len(manifest.Files) || len(request.Files) > len(manifest.Files)-request.Offset {
		return ErrCaptureMismatch
	}
	files := make([]preparedFile, 0, len(request.Files))
	var total int64
	for i, classification := range request.Files {
		item := manifest.Files[request.Offset+i]
		if classification.Path != item.Path || !validFileKind(classification.Kind) {
			return ErrCaptureMismatch
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := policy.CheckPath(item.Path); err != nil {
			return err
		}
		if err := capture.Scanner.CheckContent([]byte(item.Path)); err != nil {
			return err
		}
		if err := capture.Authorize(ctx, s.identity.WorktreeID, item.Path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
		file := preparedFile{manifest: item, classification: classification}
		if item.Status == "captured" {
			source, ok := capture.Snapshot.File(item.Path)
			if !ok || item.ContentHash == nil || item.ByteLength == nil || source.ContentHash() != *item.ContentHash || int64(source.Len()) != *item.ByteLength {
				return ErrCaptureMismatch
			}
			if int64(source.Len()) > capture.MaxSourceBytes-total {
				return ErrCaptureLimit
			}
			total += int64(source.Len())
			file.bytes = source.Bytes()
			if err := capture.Scanner.CheckContent(file.bytes); err != nil {
				return err
			}
			digest := sha256.Sum256(file.bytes)
			if hex.EncodeToString(digest[:]) != *item.ContentHash {
				return ErrCaptureMismatch
			}
		} else if item.ContentHash != nil || item.ReasonCode == nil || *item.ReasonCode == "" {
			return ErrCaptureMismatch
		}
		files = append(files, file)
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
	_, err = s.inspectPinned(ctx, tx)
	if err != nil {
		return err
	}
	if err := verifyBuildingGeneration(ctx, tx, capture, manifest); err != nil {
		return err
	}
	for _, file := range files {
		item := file.manifest
		var hash, length, reason any
		status := item.Status
		if item.ContentHash != nil {
			hash = *item.ContentHash
		}
		if item.ByteLength != nil {
			length = *item.ByteLength
		}
		if item.ReasonCode != nil {
			reason = *item.ReasonCode
		}
		if item.Status == "captured" {
			status = "indexed"
			if err := insertVerifiedBlob(ctx, tx, captureBlob{hash: *item.ContentHash, bytes: file.bytes}); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO files(generation_id,path,content_hash,kind,status,byte_length,generated,reason_code) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(generation_id,path) DO NOTHING`, capture.GenerationID, item.Path, hash, file.classification.Kind, status, length, boolInt(file.classification.Generated), reason)
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
	for _, file := range files {
		if err := capture.Authorize(ctx, s.identity.WorktreeID, file.manifest.Path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
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

func validFileKind(kind string) bool {
	if len(kind) < 1 || len(kind) > 64 {
		return false
	}
	for _, r := range kind {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}
