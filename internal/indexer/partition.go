package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var ErrPartitionLimit = errors.New("partition planning budget exceeded")

// Partition identifies one build unit and variant. Imports are local partition
// IDs, not import strings resolved by name; unresolved boundaries stay explicit.
type Partition struct {
	ID               string   `json:"id"`
	Variant          string   `json:"variant"`
	Files            []string `json:"files"`
	Imports          []string `json:"imports"`
	InputFingerprint string   `json:"input_fingerprint"`
	Unresolved       bool     `json:"unresolved"`
}

type PartitionVerifier func(context.Context, model.CaptureManifest, string) (bool, error)

type PartitionInput struct {
	Invalidation InvalidationInput
	Before       []Partition
	After        []Partition
	// VerifyPartitions independently establishes exact build-unit membership,
	// dependency edges, fingerprints and boundaries for the supplied digest.
	VerifyPartitions PartitionVerifier
	MaxPartitions    int
	MaxEdges         int
}

type PartitionPlan struct {
	Invalidation             InvalidationPlan `json:"invalidation"`
	SemanticMode             string           `json:"semantic_mode"`
	Affected                 []string         `json:"affected"`
	Components               [][]string       `json:"components"`
	ClosureVerified          bool             `json:"closure_verified"`
	NegativeCacheInvalidated bool             `json:"negative_cache_invalidated"`
	Reasons                  []string         `json:"reasons"`
}

// PlanPartitions computes a conservative reverse closure over the union of
// before/after graphs, including cycles. It never enables selective semantic
// reuse; unverified input closure or boundaries fall back to a full rebuild.
func PlanPartitions(ctx context.Context, input PartitionInput) (PartitionPlan, error) {
	if ctx == nil || input.MaxPartitions < 1 || input.MaxPartitions > 100000 || input.MaxEdges < 1 || input.MaxEdges > 1000000 || input.Before == nil || input.After == nil {
		return PartitionPlan{}, ErrInvalidationInput
	}
	if len(input.Before) > input.MaxPartitions || len(input.After) > input.MaxPartitions {
		return PartitionPlan{}, ErrPartitionLimit
	}
	plan, err := PlanInvalidation(ctx, input.Invalidation)
	if err != nil {
		return PartitionPlan{}, err
	}
	before, beforeDigest, beforeUnknown, err := normalizePartitions(ctx, input.Before, input.Invalidation.Before, input)
	if err != nil {
		return PartitionPlan{}, err
	}
	after, afterDigest, afterUnknown, err := normalizePartitions(ctx, input.After, input.Invalidation.After, input)
	if err != nil {
		return PartitionPlan{}, err
	}
	verified := false
	if input.VerifyPartitions != nil {
		first, err := input.VerifyPartitions(ctx, input.Invalidation.Before.Manifest(), beforeDigest)
		if err != nil {
			return PartitionPlan{}, contentReuseAuthorityError(ctx)
		}
		second, err := input.VerifyPartitions(ctx, input.Invalidation.After.Manifest(), afterDigest)
		if err != nil {
			return PartitionPlan{}, contentReuseAuthorityError(ctx)
		}
		verified = first && second
	}
	graph, reverse, ids, unknown, err := partitionGraph(ctx, before, after, input.MaxPartitions, input.MaxEdges)
	if err != nil {
		return PartitionPlan{}, err
	}
	unknown = unknown || beforeUnknown || afterUnknown
	seeds := map[string]bool{}
	beforeByID := partitionMap(before)
	afterByID := partitionMap(after)
	files := map[string][]string{}
	for _, parts := range [][]Partition{before, after} {
		for _, part := range parts {
			for _, filename := range part.Files {
				files[filename] = append(files[filename], part.ID)
			}
		}
	}
	for _, id := range ids {
		old, had := beforeByID[id]
		current, has := afterByID[id]
		if !had || !has || !samePartition(old, current) {
			seeds[id] = true
		}
	}
	global := false
	for _, change := range plan.Changes {
		owners := files[change.Path]
		if len(owners) == 0 {
			global = true
		}
		for _, id := range owners {
			seeds[id] = true
		}
	}
	for _, reason := range plan.Reasons {
		switch reason {
		case "PROFILE_CHANGED", "POLICY_CHANGED", "PROVIDER_CHANGED", "DEPENDENCY_INPUTS_CHANGED", "GO_BUILD_INPUT_CHANGED", "CAPTURE_INCOMPLETE":
			global = true
		}
	}
	changed := plan.Action != "noop" || beforeDigest != afterDigest || len(seeds) > 0
	if plan.Action == "rebuild" && len(seeds) == 0 {
		global = true
	}
	reasons := []string{}
	if !verified {
		reasons = append(reasons, "PARTITION_CLOSURE_UNVERIFIED")
		global = true
	}
	if unknown {
		reasons = append(reasons, "UNRESOLVED_DEPENDENCY_BOUNDARY")
		global = true
	}
	mode := "full_rebuild"
	if changed {
		reasons = append(reasons, "SEMANTIC_SELECTIVE_NOT_PROVEN")
	} else if verified && !unknown {
		mode = "reuse_published"
	} else {
		changed = true
	}
	affected := map[string]bool{}
	queue := make([]string, 0, len(ids))
	for _, id := range ids {
		if global || seeds[id] {
			affected[id] = true
			queue = append(queue, id)
		}
	}
	for i := 0; i < len(queue); i++ {
		if err := ctx.Err(); err != nil {
			return PartitionPlan{}, err
		}
		for _, consumer := range reverse[queue[i]] {
			if !affected[consumer] {
				affected[consumer] = true
				queue = append(queue, consumer)
			}
		}
	}
	components, err := partitionComponents(ctx, graph, reverse, ids)
	if err != nil {
		return PartitionPlan{}, err
	}
	result := PartitionPlan{Invalidation: plan, SemanticMode: mode, Affected: []string{}, Components: components, ClosureVerified: verified && !unknown, NegativeCacheInvalidated: changed, Reasons: reasons}
	for _, id := range ids {
		if affected[id] {
			result.Affected = append(result.Affected, id)
		}
	}
	sort.Strings(result.Reasons)
	for _, manifest := range []model.CaptureManifest{input.Invalidation.Before.Manifest(), input.Invalidation.After.Manifest()} {
		if err := input.Invalidation.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, input.Invalidation.PolicyEpoch); err != nil {
			return PartitionPlan{}, contentReuseAuthorityError(ctx)
		}
		for _, file := range manifest.Files {
			if err := ctx.Err(); err != nil {
				return PartitionPlan{}, err
			}
			if err := input.Invalidation.AuthorizePath(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, file.Path, input.Invalidation.PolicyEpoch); err != nil {
				return PartitionPlan{}, contentReuseAuthorityError(ctx)
			}
		}
	}
	if verified {
		first, err := input.VerifyPartitions(ctx, input.Invalidation.Before.Manifest(), beforeDigest)
		if err != nil {
			return PartitionPlan{}, contentReuseAuthorityError(ctx)
		}
		second, err := input.VerifyPartitions(ctx, input.Invalidation.After.Manifest(), afterDigest)
		if err != nil {
			return PartitionPlan{}, contentReuseAuthorityError(ctx)
		}
		if !first || !second {
			return PartitionPlan{}, ErrInvalidationAuthority
		}
	}
	if mode == "reuse_published" {
		ok, err := input.Invalidation.VerifyReuse(ctx, input.Invalidation.Before.Manifest(), input.Invalidation.After.Manifest())
		if err != nil || !ok {
			return PartitionPlan{}, contentReuseAuthorityError(ctx)
		}
	}
	for _, manifest := range []model.CaptureManifest{input.Invalidation.Before.Manifest(), input.Invalidation.After.Manifest()} {
		if err := input.Invalidation.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, input.Invalidation.PolicyEpoch); err != nil {
			return PartitionPlan{}, contentReuseAuthorityError(ctx)
		}
	}
	outputBound := 512
	for _, id := range ids {
		encoded, err := json.Marshal(id)
		if err != nil {
			return PartitionPlan{}, ErrInvalidationInput
		}
		count := 1
		if affected[id] {
			count++
		}
		if len(encoded)+4 > (input.Invalidation.MaxOutputBytes-outputBound)/count {
			return PartitionPlan{}, ErrPartitionLimit
		}
		outputBound += (len(encoded) + 4) * count
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > input.Invalidation.MaxOutputBytes {
		return PartitionPlan{}, ErrPartitionLimit
	}
	if err := ctx.Err(); err != nil {
		return PartitionPlan{}, err
	}
	return result, nil
}

func validPartitionID(id string) bool {
	return utf8.ValidString(id) && utf8.RuneCountInString(id) > 0 && utf8.RuneCountInString(id) <= 192
}

func normalizePartitions(ctx context.Context, parts []Partition, captured *snapshot.Snapshot, input PartitionInput) ([]Partition, string, bool, error) {
	manifest := captured.Manifest()
	entries := make(map[string]bool, len(manifest.Files))
	for _, entry := range manifest.Files {
		if entry.Status == "captured" {
			entries[entry.Path] = true
		}
	}
	result := make([]Partition, 0, len(parts))
	seen := map[string]bool{}
	edges, bytesUsed := 0, 0
	unknown := false
	for _, part := range parts {
		if err := ctx.Err(); err != nil {
			return nil, "", false, err
		}
		if !validPartitionID(part.ID) || seen[part.ID] || part.Files == nil || len(part.Files) == 0 || part.Imports == nil || len(part.InputFingerprint) != 64 || strings.Trim(part.InputFingerprint, "0123456789abcdef") != "" {
			return nil, "", false, ErrInvalidationInput
		}
		if len(part.Files) > input.MaxEdges-edges || len(part.Imports) > input.MaxEdges-edges-len(part.Files) {
			return nil, "", false, ErrPartitionLimit
		}
		switch part.Variant {
		case "production", "internal_test", "external_test", "other_supported":
		default:
			return nil, "", false, ErrInvalidationInput
		}
		seen[part.ID] = true
		edges += len(part.Files) + len(part.Imports)
		rawSize := len(part.ID) + len(part.Variant) + len(part.InputFingerprint) + 128
		for _, values := range [][]string{part.Files, part.Imports} {
			for _, value := range values {
				if len(value) > 16384 || len(value) > input.Invalidation.MaxOutputBytes-bytesUsed-rawSize {
					return nil, "", false, ErrPartitionLimit
				}
				rawSize += len(value)
			}
		}
		p := part
		p.Files = append([]string{}, part.Files...)
		p.Imports = append([]string{}, part.Imports...)
		sort.Strings(p.Files)
		sort.Strings(p.Imports)
		for i, filename := range p.Files {
			if len(filename) > 16384 || !entries[filename] || policy.CheckPath(filename) != nil || i > 0 && filename == p.Files[i-1] {
				return nil, "", false, ErrInvalidationInput
			}
			if err := input.Invalidation.Scanner.CheckContent([]byte(filename)); err != nil {
				return nil, "", false, err
			}
		}
		for i, dependency := range p.Imports {
			if !validPartitionID(dependency) || i > 0 && dependency == p.Imports[i-1] {
				return nil, "", false, ErrInvalidationInput
			}
			if err := input.Invalidation.Scanner.CheckContent([]byte(dependency)); err != nil {
				return nil, "", false, err
			}
		}
		if err := input.Invalidation.Scanner.CheckContent([]byte(p.ID)); err != nil {
			return nil, "", false, err
		}
		raw, err := json.Marshal(p)
		if err != nil || len(raw)+1 > input.Invalidation.MaxOutputBytes-bytesUsed {
			return nil, "", false, ErrPartitionLimit
		}
		bytesUsed += len(raw) + 1
		unknown = unknown || p.Unresolved
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > input.Invalidation.MaxOutputBytes {
		return nil, "", false, ErrPartitionLimit
	}
	value, err := model.DecodeIdentityJSON(raw, input.Invalidation.MaxOutputBytes)
	if err != nil {
		return nil, "", false, ErrInvalidationInput
	}
	_, digest, err := model.EncodeCanonical("partition-input-v1", value, input.Invalidation.MaxOutputBytes)
	if err != nil {
		return nil, "", false, ErrPartitionLimit
	}
	return result, digest, unknown, nil
}

func partitionMap(parts []Partition) map[string]Partition {
	result := make(map[string]Partition, len(parts))
	for _, p := range parts {
		result[p.ID] = p
	}
	return result
}
func samePartition(a, b Partition) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
