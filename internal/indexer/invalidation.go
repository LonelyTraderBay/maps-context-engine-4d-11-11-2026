package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrInvalidationInput     = errors.New("invalid capture invalidation input")
	ErrInvalidationLimit     = errors.New("capture invalidation budget exceeded")
	ErrInvalidationAuthority = errors.New("capture invalidation authority could not be verified")
)

// ReuseVerifier must prove that the prior published generation and all
// effective analysis settings, resource retry conditions, and capabilities
// still match. A callback result is a trust boundary, not a freshness check.
type ReuseVerifier func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error)

type InvalidationInput struct {
	Before, After    *snapshot.Snapshot
	PolicyEpoch      int64
	Scanner          *policy.Scanner
	AuthorizeScope   func(context.Context, string, string, string, int64) error
	AuthorizePath    func(context.Context, string, string, string, string, int64) error
	VerifyReuse      ReuseVerifier
	MaxManifestBytes int
	MaxChanges       int
	MaxOutputBytes   int
}

type FileChange struct {
	Path   string              `json:"path"`
	Kind   string              `json:"kind"` // added, removed, modified
	Before *model.ManifestFile `json:"before"`
	After  *model.ManifestFile `json:"after"`
}

type InvalidationPlan struct {
	BeforeDigest    string       `json:"before_digest"`
	AfterDigest     string       `json:"after_digest"`
	Action          string       `json:"action"` // noop or rebuild
	IncrementalMode string       `json:"incremental_mode"`
	Reasons         []string     `json:"reasons"`
	Changes         []FileChange `json:"changes"`
}

// PlanInvalidation compares frozen capture inputs only. It does not observe
// the live worktree, establish freshness, delete old facts, or authorize a
// selective semantic cache. A move is represented as removal plus addition.
func PlanInvalidation(ctx context.Context, input InvalidationInput) (InvalidationPlan, error) {
	if ctx == nil || input.Before == nil || input.After == nil || input.Scanner == nil || input.AuthorizeScope == nil || input.AuthorizePath == nil ||
		input.PolicyEpoch < 1 || input.PolicyEpoch > 9007199254740991 || input.MaxManifestBytes < 1024 || input.MaxManifestBytes > 64*1024*1024 ||
		input.MaxChanges < 1 || input.MaxChanges > 100000 || input.MaxOutputBytes < 1024 || input.MaxOutputBytes > 16*1024*1024 {
		return InvalidationPlan{}, ErrInvalidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return InvalidationPlan{}, ErrInvalidationInput
	}
	if err := ctx.Err(); err != nil {
		return InvalidationPlan{}, err
	}
	before, after := input.Before.Manifest(), input.After.Manifest()
	if model.VerifyCaptureDigest(before, input.MaxManifestBytes) != nil || model.VerifyCaptureDigest(after, input.MaxManifestBytes) != nil || before.WorktreeID != after.WorktreeID {
		return InvalidationPlan{}, ErrInvalidationInput
	}
	authorityError := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrInvalidationAuthority
	}
	authorize := func(manifest model.CaptureManifest) error {
		if err := input.AuthorizeScope(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
			return authorityError()
		}
		for _, item := range manifest.Files {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := policy.CheckPath(item.Path); err != nil {
				return err
			}
			if err := input.Scanner.CheckContent([]byte(item.Path)); err != nil {
				return err
			}
			if item.ReasonCode != nil {
				if err := input.Scanner.CheckContent([]byte(*item.ReasonCode)); err != nil {
					return err
				}
			}
			if err := input.AuthorizePath(ctx, manifest.WorktreeID, manifest.ProfileID, manifest.PolicyDigest, item.Path, input.PolicyEpoch); err != nil {
				return authorityError()
			}
		}
		return nil
	}
	if err := authorize(before); err != nil {
		return InvalidationPlan{}, err
	}
	if err := authorize(after); err != nil {
		return InvalidationPlan{}, err
	}
	plan := InvalidationPlan{BeforeDigest: before.SnapshotDigest, AfterDigest: after.SnapshotDigest, Action: "rebuild", IncrementalMode: "full", Reasons: []string{}, Changes: []FileChange{}}
	reasons := map[string]bool{}
	if before.ProfileID != after.ProfileID {
		reasons["PROFILE_CHANGED"] = true
	}
	if before.PolicyDigest != after.PolicyDigest {
		reasons["POLICY_CHANGED"] = true
	}
	if before.ProviderDigest != after.ProviderDigest {
		reasons["PROVIDER_CHANGED"] = true
	}
	if !slices.Equal(before.DependencyInputDigests, after.DependencyInputDigests) {
		reasons["DEPENDENCY_INPUTS_CHANGED"] = true
	}
	for _, files := range [][]model.ManifestFile{before.Files, after.Files} {
		for _, item := range files {
			if item.Status != "captured" {
				reasons["CAPTURE_INCOMPLETE"] = true
			}
		}
	}
	used := 512
	appendChange := func(change FileChange) error {
		if len(plan.Changes) >= input.MaxChanges {
			return ErrInvalidationLimit
		}
		raw, err := json.Marshal(change)
		if err != nil || len(raw)+1 > input.MaxOutputBytes-used {
			return ErrInvalidationLimit
		}
		used += len(raw) + 1
		plan.Changes = append(plan.Changes, change)
		if change.Kind == "added" || change.Kind == "removed" {
			reasons["MEMBERSHIP_CHANGED"] = true
		} else {
			reasons["FILE_INPUT_CHANGED"] = true
		}
		switch path.Base(change.Path) {
		case "go.mod", "go.sum", "go.work", "go.work.sum":
			reasons["GO_BUILD_INPUT_CHANGED"] = true
		}
		return nil
	}
	for i, j := 0, 0; i < len(before.Files) || j < len(after.Files); {
		if err := ctx.Err(); err != nil {
			return InvalidationPlan{}, err
		}
		switch {
		case j == len(after.Files) || i < len(before.Files) && before.Files[i].Path < after.Files[j].Path:
			old := before.Files[i]
			if err := appendChange(FileChange{Path: old.Path, Kind: "removed", Before: &old}); err != nil {
				return InvalidationPlan{}, err
			}
			i++
		case i == len(before.Files) || after.Files[j].Path < before.Files[i].Path:
			current := after.Files[j]
			if err := appendChange(FileChange{Path: current.Path, Kind: "added", After: &current}); err != nil {
				return InvalidationPlan{}, err
			}
			j++
		default:
			old, current := before.Files[i], after.Files[j]
			if !sameManifestEntry(old, current) {
				if err := appendChange(FileChange{Path: old.Path, Kind: "modified", Before: &old, After: &current}); err != nil {
					return InvalidationPlan{}, err
				}
			}
			i++
			j++
		}
	}
	if before.SnapshotDigest == after.SnapshotDigest && len(reasons) == 0 {
		if input.VerifyReuse == nil {
			reasons["REUSE_PROOF_MISSING"] = true
		} else {
			ok, err := input.VerifyReuse(ctx, input.Before.Manifest(), input.After.Manifest())
			if err != nil {
				return InvalidationPlan{}, authorityError()
			}
			if ok {
				plan.Action, plan.IncrementalMode = "noop", "content_only"
			} else {
				reasons["REUSE_UNVERIFIED"] = true
			}
		}
	}
	if err := authorize(before); err != nil {
		return InvalidationPlan{}, err
	}
	if err := authorize(after); err != nil {
		return InvalidationPlan{}, err
	}
	if plan.Action == "noop" {
		ok, err := input.VerifyReuse(ctx, input.Before.Manifest(), input.After.Manifest())
		if err != nil {
			return InvalidationPlan{}, authorityError()
		}
		if !ok {
			plan.Action, plan.IncrementalMode = "rebuild", "full"
			reasons["REUSE_UNVERIFIED"] = true
		}
		if err := authorize(before); err != nil {
			return InvalidationPlan{}, err
		}
		if err := authorize(after); err != nil {
			return InvalidationPlan{}, err
		}
	}
	for reason := range reasons {
		plan.Reasons = append(plan.Reasons, reason)
	}
	sort.Strings(plan.Reasons)
	raw, err := json.Marshal(plan)
	if err != nil || len(raw) > input.MaxOutputBytes {
		return InvalidationPlan{}, ErrInvalidationLimit
	}
	if err := ctx.Err(); err != nil {
		return InvalidationPlan{}, err
	}
	return plan, nil
}

func sameManifestEntry(a, b model.ManifestFile) bool {
	return a.Path == b.Path && a.Status == b.Status && sameOptional(a.ContentHash, b.ContentHash) && sameOptional(a.ByteLength, b.ByteLength) && sameOptional(a.ReasonCode, b.ReasonCode)
}

func sameOptional[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
