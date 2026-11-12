package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var ErrContentReuseLimit = errors.New("content reuse budget exceeded")

type ContentReuseInput struct {
	Invalidation   InvalidationInput
	MaxSourceBytes int64
	MaxReuseFiles  int
}

type ContentReuseResult struct {
	Invalidation    InvalidationPlan   `json:"invalidation"`
	IncrementalMode string             `json:"incremental_mode"`
	SemanticMode    string             `json:"semantic_mode"`
	ReusedPaths     []string           `json:"reused_paths"`
	Captured        *snapshot.Snapshot `json:"-"`
}

// ReuseCapturedContent rebuilds an immutable capture using previously
// captured bytes only where path, policy and content all match. It never
// reuses syntax facts, negative lookups or semantic graph results after a
// change. A noop requires the invalidation layer's separate prior-generation
// proof. The returned snapshot is an in-memory value, not a live freshness
// claim or proof that a durable blob already exists.
func ReuseCapturedContent(ctx context.Context, input ContentReuseInput) (ContentReuseResult, error) {
	if ctx == nil || input.MaxSourceBytes < 1 || input.MaxSourceBytes > 512*1024*1024 || input.MaxReuseFiles < 1 || input.MaxReuseFiles > 100000 {
		return ContentReuseResult{}, ErrInvalidationInput
	}
	plan, err := PlanInvalidation(ctx, input.Invalidation)
	if err != nil {
		return ContentReuseResult{}, err
	}
	result := ContentReuseResult{Invalidation: plan, IncrementalMode: "full", SemanticMode: "full_rebuild", ReusedPaths: []string{}}
	before, after := input.Invalidation.Before.Manifest(), input.Invalidation.After.Manifest()
	authorizePath := func(manifest model.CaptureManifest, path string) error {
		if err := input.Invalidation.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, input.Invalidation.PolicyEpoch); err != nil {
			return contentReuseAuthorityError(ctx)
		}
		if err := input.Invalidation.AuthorizePath(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, path, input.Invalidation.PolicyEpoch); err != nil {
			return contentReuseAuthorityError(ctx)
		}
		return ctx.Err()
	}
	files := make([]snapshot.File, 0, len(after.Files))
	var total int64
	used := 512
	for _, item := range after.Files {
		if err := ctx.Err(); err != nil {
			return ContentReuseResult{}, err
		}
		if item.Status != "captured" {
			continue
		}
		if err := authorizePath(after, item.Path); err != nil {
			return ContentReuseResult{}, err
		}
		current, ok := input.Invalidation.After.File(item.Path)
		if !ok || item.ContentHash == nil || item.ByteLength == nil || current.ContentHash() != *item.ContentHash || int64(current.Len()) != *item.ByteLength {
			return ContentReuseResult{}, ErrInvalidationInput
		}
		if int64(current.Len()) > input.MaxSourceBytes-total {
			return ContentReuseResult{}, ErrContentReuseLimit
		}
		total += int64(current.Len())
		currentBytes := current.Bytes()
		if err := input.Invalidation.Scanner.CheckContent(currentBytes); err != nil {
			return ContentReuseResult{}, err
		}
		chosen := current
		if before.PolicyDigest == after.PolicyDigest {
			prior, exists := input.Invalidation.Before.File(item.Path)
			if exists {
				if err := authorizePath(before, item.Path); err != nil {
					return ContentReuseResult{}, err
				}
			}
			if exists && prior.ContentHash() == current.ContentHash() && prior.Len() == current.Len() && bytes.Equal(prior.Bytes(), currentBytes) {
				if len(result.ReusedPaths) >= input.MaxReuseFiles {
					return ContentReuseResult{}, ErrContentReuseLimit
				}
				encoded, err := json.Marshal(item.Path)
				if err != nil || len(encoded)+1 > input.Invalidation.MaxOutputBytes-used {
					return ContentReuseResult{}, ErrContentReuseLimit
				}
				used += len(encoded) + 1
				result.ReusedPaths = append(result.ReusedPaths, item.Path)
				chosen = prior
			}
		}
		files = append(files, chosen)
	}
	if len(result.ReusedPaths) > 0 {
		result.IncrementalMode = "content_only"
	}
	if plan.Action == "noop" {
		result.IncrementalMode, result.SemanticMode, result.Captured = "content_only", "reuse_published", input.Invalidation.Before
	} else {
		rebuilt, err := snapshot.Freeze(after, files, input.Invalidation.MaxManifestBytes, input.MaxSourceBytes)
		if err != nil || rebuilt.Manifest().SnapshotDigest != after.SnapshotDigest {
			return ContentReuseResult{}, ErrInvalidationInput
		}
		result.Captured = rebuilt
	}
	if plan.Action == "noop" {
		ok, err := input.Invalidation.VerifyReuse(ctx, before, after)
		if err != nil || !ok {
			return ContentReuseResult{}, contentReuseAuthorityError(ctx)
		}
	}
	for _, manifest := range []model.CaptureManifest{before, after} {
		if err := input.Invalidation.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, input.Invalidation.PolicyEpoch); err != nil {
			return ContentReuseResult{}, contentReuseAuthorityError(ctx)
		}
		for _, item := range manifest.Files {
			if err := ctx.Err(); err != nil {
				return ContentReuseResult{}, err
			}
			if err := input.Invalidation.AuthorizePath(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, item.Path, input.Invalidation.PolicyEpoch); err != nil {
				return ContentReuseResult{}, contentReuseAuthorityError(ctx)
			}
		}
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > input.Invalidation.MaxOutputBytes {
		return ContentReuseResult{}, ErrContentReuseLimit
	}
	if err := ctx.Err(); err != nil {
		return ContentReuseResult{}, err
	}
	return result, nil
}

func contentReuseAuthorityError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrInvalidationAuthority
}
