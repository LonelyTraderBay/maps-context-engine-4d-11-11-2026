package index

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// ReadPublishedInventory reads only file metadata from one authenticated
// generation. The caller must authorize paths before disclosing them.
func (s *Store) ReadPublishedInventory(ctx context.Context, input PublishedFileRequest) (GenerationObservation, []model.ManifestFile, error) {
	var files []model.ManifestFile
	generation, err := s.readGeneration(ctx, input.ProfileID, input.GenerationID, input.AuthorizeGeneration,
		input.MaxManifestBytes, func(tx *sql.Tx, value GenerationObservation, raw []byte) (resultErr error) {
			manifest, err := model.DecodeCaptureManifestJSON(raw, input.MaxManifestBytes)
			if err != nil {
				return err
			}
			files = manifest.Files
			byPath := make(map[string]int, len(files))
			for i, file := range files {
				byPath[file.Path] = i
			}
			rows, err := tx.QueryContext(ctx, `SELECT path,status,content_hash,reason_code FROM files WHERE generation_id=? LIMIT ?`,
				value.GenerationID, len(files)+1)
			if err != nil {
				return err
			}
			defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
			for rows.Next() {
				var name, status string
				var hash, reason sql.NullString
				if err := rows.Scan(&name, &status, &hash, &reason); err != nil {
					return err
				}
				i, ok := byPath[name]
				if !ok {
					return ErrCaptureMismatch
				}
				delete(byPath, name)
				file := &files[i]
				if file.Status != "captured" {
					if hash.Valid || status == "indexed" || status == "parse_error" {
						return ErrCaptureMismatch
					}
					continue
				}
				if file.ContentHash == nil || !hash.Valid || hash.String != *file.ContentHash || status != "indexed" && status != "parse_error" {
					return ErrCaptureMismatch
				}
				if status == "parse_error" {
					const parseError = "PARSE_ERROR"
					code := parseError
					file.ContentHash, file.ReasonCode = nil, &code
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if len(byPath) != 0 {
				return ErrCaptureMismatch
			}
			return nil
		})
	if err != nil {
		return GenerationObservation{}, nil, err
	}
	return generation, files, nil
}
