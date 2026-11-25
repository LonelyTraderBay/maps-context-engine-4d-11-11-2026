package index

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

// ReadGoQuestionSources collects verified captured Go files in a single
// pinned transaction. File hints are only used to prioritize the bounded read.
func (s *Store) ReadGoQuestionSources(ctx context.Context, req PublishedFileRequest, question search.GoQuestion,
	maxBytes int64) (GenerationObservation, []PublishedFile, bool, error) {
	if req.Scanner == nil || req.AuthorizePath == nil || req.MaxFileBytes < 1 || maxBytes < 1 || maxBytes > 4<<20 {
		return GenerationObservation{}, nil, false, ErrInvalidStore
	}
	var files []PublishedFile
	var limited bool
	generation, err := s.readGeneration(ctx, req.ProfileID, req.GenerationID, req.AuthorizeGeneration,
		req.MaxManifestBytes, func(tx *sql.Tx, generation GenerationObservation, raw []byte) error {
			manifest, err := model.DecodeCaptureManifestJSON(raw, req.MaxManifestBytes)
			if err != nil {
				return err
			}
			type candidate struct {
				entry model.ManifestFile
				score float64
			}
			var candidates []candidate
			for _, file := range manifest.Files {
				if file.Status != "captured" || !strings.HasSuffix(strings.ToLower(file.Path), ".go") ||
					req.Path != "" && req.Path != file.Path ||
					req.Path == "" && !question.AllowsTestFiles() && strings.HasSuffix(strings.ToLower(file.Path), "_test.go") {
					continue
				}
				candidates = append(candidates, candidate{file, question.PathRank(file.Path)})
			}
			sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
			remaining := maxBytes
			for _, item := range candidates {
				if err := ctx.Err(); err != nil {
					return err
				}
				if len(files) >= 384 {
					limited = true
					break
				}
				entry := item.entry
				if entry.ByteLength == nil || entry.ContentHash == nil || *entry.ByteLength < 0 {
					return ErrCaptureMismatch
				}
				if *entry.ByteLength > remaining || *entry.ByteLength > int64(req.MaxFileBytes) {
					limited = true
					continue
				}
				if err := req.AuthorizePath(ctx, generation.Identity.WorktreeID, entry.Path, generation.PolicyDigest, generation.PolicyEpoch); err != nil {
					return err
				}
				body, err := readPublishedBlob(ctx, tx, generation.GenerationID, entry, req.Scanner, req.MaxFileBytes)
				if err != nil {
					return err
				}
				files = append(files, PublishedFile{Path: entry.Path, ContentHash: *entry.ContentHash, Bytes: body})
				remaining -= int64(len(body))
			}
			return nil
		})
	if err != nil {
		return GenerationObservation{}, nil, false, err
	}
	return generation, files, limited, nil
}
