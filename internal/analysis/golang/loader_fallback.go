package golang

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// CapturedSyntaxFallback analyzes only explicitly requested captured files and
// caller-supplied package identities. It does not infer full workspace or
// build-variant coverage when the guarded loader is unavailable.
type CapturedSyntaxFallback struct {
	Loader         CapturedLoaderInput
	Files          []SyntaxInput
	MaxOutputBytes int
}

type CapturedSyntaxFallbackResult struct {
	Loader CapturedLoaderResult
	Syntax []SyntaxResult
}

// LoadOrAnalyzeCaptured preserves partial reasons and returns no data on
// authorization changes, cancellation, or a whole-result budget failure.
func LoadOrAnalyzeCaptured(ctx context.Context, input CapturedSyntaxFallback) (CapturedSyntaxFallbackResult, error) {
	if ctx == nil || len(input.Files) < 1 || len(input.Files) > 128 || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 8*1024*1024 {
		return CapturedSyntaxFallbackResult{}, ErrLoaderInput
	}
	loaded, err := LoadCapturedPackages(ctx, input.Loader)
	if err != nil {
		return CapturedSyntaxFallbackResult{}, err
	}
	preflight := input.Loader.Preflight
	manifest := preflight.Snapshot.Manifest()
	result := CapturedSyntaxFallbackResult{Loader: loaded, Syntax: []SyntaxResult{}}
	initial, err := json.Marshal(result)
	if err != nil || len(initial) > input.MaxOutputBytes {
		return CapturedSyntaxFallbackResult{}, ErrSyntaxLimit
	}
	used := len(initial)
	selected := make(map[string]bool)
	if loaded.BuildSelectionVerified {
		if len(loaded.Packages) != 1 {
			return CapturedSyntaxFallbackResult{}, ErrInputMismatch
		}
		for _, filename := range loaded.Packages[0].Files {
			selected[filename] = true
		}
	}
	recheck := func(filename string) error {
		if err := preflight.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
			return loaderAuthorityError(ctx)
		}
		if err := preflight.AuthorizePath(ctx, manifest.WorktreeID, filename, manifest.PolicyDigest, preflight.PolicyEpoch); err != nil {
			return loaderAuthorityError(ctx)
		}
		return ctx.Err()
	}
	seen := make(map[string]bool, len(input.Files))
	for _, requested := range input.Files {
		key := requested.Path + "\x00" + requested.Variant
		if seen[key] {
			return CapturedSyntaxFallbackResult{}, ErrLoaderInput
		}
		seen[key] = true
		if policy.CheckPath(requested.Path) != nil {
			return CapturedSyntaxFallbackResult{}, ErrInputMismatch
		}
		if _, ok := preflight.Snapshot.File(requested.Path); !ok {
			return CapturedSyntaxFallbackResult{}, ErrInputMismatch
		}
		if loaded.BuildSelectionVerified && (!selected[requested.Path] || requested.Variant != "production" || requested.ImportPath != loaded.Packages[0].ImportPath || requested.PackageName != loaded.Packages[0].Name) {
			return CapturedSyntaxFallbackResult{}, ErrInputMismatch
		}
		if err := recheck(requested.Path); err != nil {
			return CapturedSyntaxFallbackResult{}, err
		}
		requested.Snapshot, requested.Profile, requested.Scanner = preflight.Snapshot, preflight.Profile, preflight.Scanner
		requested.MaxManifestBytes, requested.MaxFileBytes = preflight.MaxManifestBytes, preflight.MaxFileBytes
		if requested.MaxOutputBytes > input.MaxOutputBytes {
			requested.MaxOutputBytes = input.MaxOutputBytes
		}
		parsed, err := AnalyzeSyntaxFile(ctx, requested)
		if err != nil {
			return CapturedSyntaxFallbackResult{}, err
		}
		parsed.BuildSelectionVerified = loaded.BuildSelectionVerified
		if err := recheck(requested.Path); err != nil {
			return CapturedSyntaxFallbackResult{}, err
		}
		raw, err := json.Marshal(parsed)
		addition := len(raw)
		if len(result.Syntax) > 0 {
			addition++ // JSON array separator
		}
		if err != nil || addition > input.MaxOutputBytes-used {
			return CapturedSyntaxFallbackResult{}, ErrSyntaxLimit
		}
		used += addition
		result.Syntax = append(result.Syntax, parsed)
	}
	// Recheck every returned path at the publication boundary, not only the
	// most recently parsed file; scope identity remains the original capture.
	for _, parsed := range result.Syntax {
		if err := recheck(parsed.Path); err != nil {
			return CapturedSyntaxFallbackResult{}, err
		}
	}
	if err := model.VerifyCaptureDigest(manifest, preflight.MaxManifestBytes); err != nil {
		return CapturedSyntaxFallbackResult{}, ErrInputMismatch
	}
	return result, nil
}
