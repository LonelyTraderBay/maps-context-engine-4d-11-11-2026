// Package compose connects already-owned adapters without creating credentials,
// opening target paths or providing transport endpoints.
package compose

import (
	"context"
	"errors"
	"reflect"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

var (
	ErrScopeInput           = errors.New("invalid scope composition input")
	ErrScopeChanged         = errors.New("scope changed during observation")
	ErrScopeSnapshotExpired = errors.New("pinned generation is no longer available")
)

// ScopeAccess is a caller-owned authentication and authorization boundary.
// It must check the current principal and explicit worktree/profile binding on
// every invocation and must not reenter any pool currently being read.
type ScopeAccess func(context.Context, app.CorePrincipal, string, string) error

// ScopeReader borrows verified stores and already-authorized root handles. The
// owner must retain their lifecycle leases and pools until all calls finish.
// It handles a single worktree/profile; group resolution remains unsupported.
type ScopeReader struct {
	Catalog          *catalog.Store
	Index            *index.Store
	Records          *records.Store
	WorktreeRoot     *paths.Root
	CommonRoot       *paths.Root
	LinkedRelative   string
	WorktreeID       string
	ProfileID        string
	Access           ScopeAccess
	MaxHeads         int
	MaxManifestBytes int
}

// ScopeObservation is a pair of matching short observations, not an atomic
// multi-database snapshot. ApprovedHeads counts declared current heads only.
type ScopeObservation struct {
	Vector     model.ScopeVector
	Binding    catalog.Binding
	Generation index.GenerationObservation
	Records    records.State
}

func (r *ScopeReader) valid(principal app.CorePrincipal) bool {
	return r != nil && r.Catalog != nil && r.Index != nil && r.Records != nil && r.WorktreeRoot != nil && r.CommonRoot != nil && r.Access != nil && r.MaxHeads >= 1 && r.MaxHeads <= 100000 && r.MaxManifestBytes >= 1024 && r.MaxManifestBytes <= 64*1024*1024 && principal.ID != "" && len(principal.ID) <= 1024 && utf8.ValidString(principal.ID) && hash(principal.Digest) && hash(principal.BindingDigest)
}

// Resolve observes an explicit published generation, or resolves the active
// generation exactly once when generationID is empty. It then rereads all
// three stores against that generation and rejects any changed observation.
func (r *ScopeReader) Resolve(ctx context.Context, principal app.CorePrincipal, generationID string) (ScopeObservation, error) {
	if ctx == nil || !r.valid(principal) {
		return ScopeObservation{}, ErrScopeInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return ScopeObservation{}, ErrScopeInput
	}
	first, err := r.observe(ctx, principal, generationID)
	if err != nil {
		if generationID != "" && errors.Is(err, index.ErrSnapshotUnavailable) {
			return ScopeObservation{}, ErrScopeSnapshotExpired
		}
		return ScopeObservation{}, err
	}
	second, err := r.observe(ctx, principal, first.Generation.GenerationID)
	if err != nil {
		if errors.Is(err, index.ErrSnapshotUnavailable) {
			return ScopeObservation{}, ErrScopeSnapshotExpired
		}
		return ScopeObservation{}, err
	}
	if model.ComparePinnedScope(first.Vector, second.Vector, 32768) != nil || !reflect.DeepEqual(first.Binding, second.Binding) || first.Generation != second.Generation || first.Records != second.Records {
		return ScopeObservation{}, ErrScopeChanged
	}
	if err := r.Access(ctx, principal, r.WorktreeID, r.ProfileID); err != nil {
		return ScopeObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return ScopeObservation{}, err
	}
	return second, nil
}

func (r *ScopeReader) observe(ctx context.Context, principal app.CorePrincipal, generationID string) (ScopeObservation, error) {
	if err := r.Access(ctx, principal, r.WorktreeID, r.ProfileID); err != nil {
		return ScopeObservation{}, err
	}
	authorize := func(ctx context.Context, worktree, profile string) error {
		if worktree != r.WorktreeID || profile != r.ProfileID {
			return ErrScopeChanged
		}
		return r.Access(ctx, principal, worktree, profile)
	}
	binding, err := r.Catalog.ReadBinding(ctx, r.WorktreeID, r.ProfileID, authorize)
	if err != nil {
		return ScopeObservation{}, err
	}
	if err := binding.VerifyRoots(r.WorktreeRoot, r.CommonRoot, r.LinkedRelative); err != nil {
		return ScopeObservation{}, err
	}
	state, err := r.Records.ReadState(ctx, r.MaxHeads, func(ctx context.Context, identity records.Identity) error {
		if identity.WorktreeID != r.WorktreeID {
			return ErrScopeChanged
		}
		return authorize(ctx, identity.WorktreeID, r.ProfileID)
	})
	if err != nil {
		return ScopeObservation{}, err
	}
	generation, err := r.Index.ReadGeneration(ctx, r.ProfileID, generationID, func(ctx context.Context, worktree, profile, digest string, epoch int64) error {
		if digest != binding.PolicyDigest || epoch != binding.PolicyEpoch {
			return ErrScopeChanged
		}
		return authorize(ctx, worktree, profile)
	}, r.MaxManifestBytes)
	if err != nil {
		return ScopeObservation{}, err
	}
	if generation.Identity.WorktreeID != r.WorktreeID || generation.ProviderDigest != binding.Profile.ProviderDigest {
		return ScopeObservation{}, ErrScopeChanged
	}
	if err := binding.VerifyRoots(r.WorktreeRoot, r.CommonRoot, r.LinkedRelative); err != nil {
		return ScopeObservation{}, err
	}
	member := model.ScopeMember{WorktreeID: r.WorktreeID, ProfileID: r.ProfileID, GenerationID: generation.GenerationID, SnapshotDigest: generation.SnapshotDigest, PolicyDigest: binding.PolicyDigest, PolicyEpoch: binding.PolicyEpoch, BindingRevision: binding.BindingRevision, RecordEpoch: state.RecordEpoch, IndexIncarnation: generation.Identity.Incarnation, RecordsIncarnation: state.Identity.Incarnation}
	vector := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: binding.CatalogIncarnation, UnavailableMembers: []model.MemberUnavailable{}}
	vector.VectorDigest, err = model.ComputeScopeVectorDigest(vector, 32768)
	if err != nil {
		return ScopeObservation{}, errors.Join(ErrScopeInput, err)
	}
	return ScopeObservation{Vector: vector, Binding: binding, Generation: generation, Records: state}, nil
}

// Recheck never changes a pinned generation to the active generation. The
// result must be discarded if any authority, identity or epoch has changed.
func (r *ScopeReader) Recheck(ctx context.Context, principal app.CorePrincipal, pinned model.ScopeVector) (ScopeObservation, error) {
	if !r.valid(principal) || model.VerifyScopeVectorDigest(pinned, 32768) != nil || pinned.Kind != "single" || len(pinned.Members) != 1 || len(pinned.UnavailableMembers) != 0 || pinned.Members[0].WorktreeID != r.WorktreeID || pinned.Members[0].ProfileID != r.ProfileID {
		return ScopeObservation{}, ErrScopeInput
	}
	if ctx == nil {
		return ScopeObservation{}, ErrScopeInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return ScopeObservation{}, ErrScopeInput
	}
	current, err := r.observe(ctx, principal, pinned.Members[0].GenerationID)
	if err != nil {
		if errors.Is(err, index.ErrSnapshotUnavailable) {
			return ScopeObservation{}, ErrScopeSnapshotExpired
		}
		return ScopeObservation{}, err
	}
	if model.ComparePinnedScope(pinned, current.Vector, 32768) != nil {
		return ScopeObservation{}, ErrScopeChanged
	}
	if err := r.Access(ctx, principal, r.WorktreeID, r.ProfileID); err != nil {
		return ScopeObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return ScopeObservation{}, err
	}
	return current, nil
}

// NoApprovedRecords is an epoch-bound absence proof for R1 task slicing. A
// nonzero declared-approved count is deliberately not interpreted as an
// effective decision; record reading and lifecycle belong to T047.
func (r *ScopeReader) NoApprovedRecords(ctx context.Context, principal app.CorePrincipal, pinned model.ScopeVector) (bool, error) {
	current, err := r.Recheck(ctx, principal, pinned)
	if err != nil {
		return false, err
	}
	return current.Records.ApprovedHeads == 0, nil
}

// ObserveMissingIndex proves a registered single binding has no active
// generation at two bounded observations. It never invents a ScopeVector.
func (r *ScopeReader) ObserveMissingIndex(ctx context.Context, principal app.CorePrincipal) (catalog.Binding, error) {
	if ctx == nil || !r.valid(principal) {
		return catalog.Binding{}, ErrScopeInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return catalog.Binding{}, ErrScopeInput
	}
	var prior catalog.Binding
	for i := 0; i < 2; i++ {
		if err := r.Access(ctx, principal, r.WorktreeID, r.ProfileID); err != nil {
			return catalog.Binding{}, err
		}
		binding, err := r.Catalog.ReadBinding(ctx, r.WorktreeID, r.ProfileID, func(ctx context.Context, worktree, profile string) error {
			return r.Access(ctx, principal, worktree, profile)
		})
		if err != nil {
			return catalog.Binding{}, err
		}
		if err := binding.VerifyRoots(r.WorktreeRoot, r.CommonRoot, r.LinkedRelative); err != nil {
			return catalog.Binding{}, err
		}
		absent, err := r.Index.ActiveGenerationAbsent(ctx, r.ProfileID, func(ctx context.Context, identity index.Identity, profile string) error {
			if identity.WorktreeID != r.WorktreeID || profile != r.ProfileID {
				return ErrScopeChanged
			}
			return r.Access(ctx, principal, identity.WorktreeID, profile)
		})
		if err != nil {
			return catalog.Binding{}, err
		}
		if !absent || i > 0 && !reflect.DeepEqual(prior, binding) {
			return catalog.Binding{}, ErrScopeChanged
		}
		prior = binding
	}
	if err := r.Access(ctx, principal, r.WorktreeID, r.ProfileID); err != nil {
		return catalog.Binding{}, err
	}
	if err := prior.VerifyRoots(r.WorktreeRoot, r.CommonRoot, r.LinkedRelative); err != nil {
		return catalog.Binding{}, err
	}
	return prior, ctx.Err()
}

func hash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return true
}
