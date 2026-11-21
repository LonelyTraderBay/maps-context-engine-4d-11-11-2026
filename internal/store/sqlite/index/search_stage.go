package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path"
	"sort"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const maxSearchDocumentBytes = 8 * 1024 * 1024

type StageSearchDocumentsRequest struct {
	Capture             CaptureWrite
	AuthorizeGeneration GenerationAuthorizer
	Paths               []string
	MaxBatchBytes       int // sum of persisted text and body bytes
}

type searchDocument struct {
	path, id, name, body string
}

func documentID(path string) string {
	hash := sha256.Sum256([]byte("mce4d:search-file:v1\x00" + path))
	return "file-" + hex.EncodeToString(hash[:])
}

// StageSearchDocuments indexes only selected captured files. Missing documents
// make search coverage partial; this method does not establish completeness.
// All bodies come from the immutable capture, never from a caller or live disk.
func (s *Store) StageSearchDocuments(ctx context.Context, request StageSearchDocumentsRequest) (resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) ||
		capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || capture.Writer == nil || request.AuthorizeGeneration == nil ||
		!validUUID(capture.GenerationID) || !validUUID(capture.JobID) || capture.PolicyEpoch < 1 || capture.PolicyEpoch > 9007199254740991 ||
		capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 || capture.MaxSourceBytes < 1 ||
		len(request.Paths) < 1 || len(request.Paths) > 256 || request.MaxBatchBytes < 1 || request.MaxBatchBytes > 16*1024*1024 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	manifest := capture.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil {
		return ErrCaptureMismatch
	}
	entries := make(map[string]model.ManifestFile, len(manifest.Files))
	for _, entry := range manifest.Files {
		entries[entry.Path] = entry
	}
	paths := append([]string(nil), request.Paths...)
	sort.Strings(paths)
	documents := make([]searchDocument, 0, len(paths))
	total := 0
	var totalSource int64
	for i, filename := range paths {
		if i > 0 && paths[i-1] == filename {
			return ErrCaptureMismatch
		}
		entry, exists := entries[filename]
		file, ok := capture.Snapshot.File(filename)
		if !exists || entry.Status != "captured" || !ok || entry.ContentHash == nil || entry.ByteLength == nil ||
			file.ContentHash() != *entry.ContentHash || int64(file.Len()) != *entry.ByteLength {
			return ErrCaptureMismatch
		}
		if err := policy.CheckPath(filename); err != nil {
			return err
		}
		if err := capture.Authorize(ctx, s.identity.WorktreeID, filename, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
		id, name := documentID(filename), path.Base(filename)
		metadataBytes := len(capture.GenerationID) + len(id) + len(filename)*2 + len("file") + len(name)
		if file.Len() > maxSearchDocumentBytes || int64(file.Len()) > capture.MaxSourceBytes-totalSource || metadataBytes > request.MaxBatchBytes-total || file.Len() > request.MaxBatchBytes-total-metadataBytes {
			return ErrCaptureLimit
		}
		data := file.Bytes()
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return ErrCaptureMismatch
		}
		if err := capture.Scanner.CheckContent([]byte(filename)); err != nil {
			return err
		}
		if err := capture.Scanner.CheckContent(data); err != nil {
			return err
		}
		total += metadataBytes + len(data)
		totalSource += int64(len(data))
		documents = append(documents, searchDocument{path: filename, id: id, name: name, body: string(data)})
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
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
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
	for _, doc := range documents {
		file, _ := capture.Snapshot.File(doc.path)
		var status, hash string
		var length int64
		if err := tx.QueryRowContext(ctx, `SELECT status,content_hash,byte_length FROM files WHERE generation_id=? AND path=?`, capture.GenerationID, doc.path).Scan(&status, &hash, &length); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrCaptureMismatch
			}
			return err
		}
		if (status != "indexed" && status != "parse_error") || hash != file.ContentHash() || length != int64(file.Len()) {
			return ErrCaptureMismatch
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO search_documents(generation_id,item_id,path,kind,name,qualified_name,body) VALUES(?,?,?,'file',?,?,?) ON CONFLICT(generation_id,item_id) DO NOTHING`, capture.GenerationID, doc.id, doc.path, doc.name, doc.path, doc.body)
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
	for _, doc := range documents {
		if err := capture.Authorize(ctx, s.identity.WorktreeID, doc.path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
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

// validateSearchDocuments rechecks every indexed document against the frozen
// capture before publishing, including rows not inserted by this adapter.
func validateSearchDocuments(ctx context.Context, db publicationQueryer, worktree string, capture CaptureWrite, manifest model.CaptureManifest) (resultErr error) {
	var count, tooLarge int64
	if err := db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(CASE WHEN length(CAST(body AS BLOB))>? OR length(CAST(path AS BLOB))>16384 OR length(CAST(name AS BLOB))>16384 OR length(CAST(qualified_name AS BLOB))>16384 OR length(CAST(item_id AS BLOB))>128 OR length(CAST(kind AS BLOB))>32 THEN 1 ELSE 0 END),0) FROM search_documents WHERE generation_id=?`, maxSearchDocumentBytes, capture.GenerationID).Scan(&count, &tooLarge); err != nil {
		return err
	}
	if count > int64(len(manifest.Files)) || tooLarge != 0 {
		return ErrPublishIntegrity
	}
	entries := make(map[string]model.ManifestFile, len(manifest.Files))
	for _, item := range manifest.Files {
		entries[item.Path] = item
	}
	rows, err := db.QueryContext(ctx, `SELECT item_id,path,kind,name,qualified_name,body FROM search_documents WHERE generation_id=? ORDER BY path,item_id`, capture.GenerationID)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	var total int64
	previous := ""
	for rows.Next() {
		var id, filename, kind, name, qualified, body string
		if err := rows.Scan(&id, &filename, &kind, &name, &qualified, &body); err != nil {
			return err
		}
		entry, ok := entries[filename]
		file, exists := capture.Snapshot.File(filename)
		if !ok || entry.Status != "captured" || !exists || entry.ContentHash == nil || entry.ByteLength == nil || filename == previous ||
			id != documentID(filename) || kind != "file" || name != path.Base(filename) || qualified != filename ||
			file.ContentHash() != *entry.ContentHash || int64(file.Len()) != *entry.ByteLength || body != string(file.Bytes()) || !utf8.ValidString(body) || bytes.IndexByte([]byte(body), 0) >= 0 {
			return ErrPublishIntegrity
		}
		if int64(len(body)) > capture.MaxSourceBytes-total {
			return ErrCaptureLimit
		}
		total += int64(len(body))
		if err := policy.CheckPath(filename); err != nil {
			return err
		}
		if err := capture.Scanner.CheckContent([]byte(filename)); err != nil {
			return err
		}
		if err := capture.Scanner.CheckContent([]byte(body)); err != nil {
			return err
		}
		if err := capture.Authorize(ctx, worktree, filename, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
		previous = filename
	}
	return rows.Err()
}
