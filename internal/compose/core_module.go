package compose

import (
	"context"
	"encoding/hex"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// locateModules reads only the authorized, pinned overview; it does not
// discover a module from a directory name or resolve active on continuation.
func (b *CoreBackend) locateModules(ctx context.Context, principal app.CorePrincipal, request model.LocateRequest, options app.CoreCallOptions, observation ScopeObservation, binding contextpack.CursorBinding, guard contextpack.CursorGuard, base search.Request) ([][]byte, error) {
	if b.buildGate == nil || request.Query == nil || request.Position != nil || len(request.Kinds) != 0 || request.Package != nil {
		return nil, app.ErrCoreUnsupported
	}
	if strings.TrimSpace(*request.Query) == "" {
		return nil, app.ErrCoreInput
	}
	for _, r := range *request.Query {
		if unicode.IsControl(r) {
			return nil, app.ErrCoreInput
		}
	}
	if err := base.Scanner.CheckContent([]byte(*request.Query)); err != nil {
		return nil, coreStoreError(err)
	}
	select {
	case b.buildGate <- struct{}{}:
		defer func() { <-b.buildGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	member := observation.Vector.Members[0]
	maxFile := minInt64(int64(base.MaxFileBytes), b.limits.MaxFileBytes, packSourceLimit)
	maxSource := minInt64(observation.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot, packSourceLimit)
	if maxFile < 1 || maxSource < 1 {
		return nil, app.ErrCoreLimit
	}
	scanner, err := policy.NewScanner(int(maxInt64(maxFile, int64(b.Reader.MaxManifestBytes), packFactLimit)))
	if err != nil {
		return nil, err
	}
	base.Scanner = scanner
	graph, err := b.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: observation.Vector.VectorDigest, AuthorizeGeneration: base.AuthorizeGeneration, AuthorizePath: base.AuthorizePath, Scanner: base.Scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000})
	if err != nil {
		return nil, coreStoreError(err)
	}
	report, err := b.buildOverview(ctx, principal, observation.Vector, graph, base.Scanner, maxSource)
	if err != nil {
		return nil, coreStoreError(err)
	}
	summary, err := report.Read(ctx)
	if err != nil {
		return nil, coreStoreError(err)
	}
	blocks, err := moduleLocatePage(ctx, b.Cursor, binding, guard, principal.ID, request, options, summary.Modules)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if _, err := report.Read(ctx); err != nil {
		return nil, coreStoreError(err)
	}
	return blocks, nil
}

func moduleLocatePage(ctx context.Context, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard, principalID string, request model.LocateRequest, options app.CoreCallOptions, modules []overview.Module) ([][]byte, error) {
	if ctx == nil || codec == nil || guard == nil || principalID == "" || request.Mode != "module" || request.Query == nil || request.Position != nil || len(request.Kinds) != 0 || request.Package != nil || request.Limit < 1 || request.Limit > 100 || binding.Purpose != "list" || binding.OrderVersion != "locate-id-v1" || binding.ResultID != "" || binding.ResultDigest != "" || model.VerifyScopeVectorDigest(binding.Scope, 32768) != nil || binding.Scope.Kind != "single" || len(binding.Scope.Members) != 1 || len(binding.Scope.UnavailableMembers) != 0 {
		return nil, app.ErrCoreInput
	}
	member := binding.Scope.Members[0]
	if requested := request.Scope; requested != nil && (requested.GroupID != nil || requested.WorktreeID != nil && *requested.WorktreeID != member.WorktreeID || requested.ProfileID != nil && *requested.ProfileID != member.ProfileID || requested.GenerationID != nil && *requested.GenerationID != member.GenerationID || requested.PinnedVector != nil && model.ComparePinnedScope(*requested.PinnedVector, binding.Scope, 32768) != nil) {
		return nil, contextpack.ErrCursorInvalid
	}
	continuation := request.Cursor
	requestedLimit := request.Limit
	request.Cursor, request.Limit = nil, 20
	digest, err := model.RequestDigest("locate", principalID, "LocateRequest", request, 32768, 4)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	if binding.RequestDigest != "" && binding.RequestDigest != digest {
		return nil, contextpack.ErrCursorInvalid
	}
	binding.RequestDigest = digest
	var claims contextpack.SignedCursor
	if continuation == nil {
		anchor, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
		if err != nil {
			return nil, err
		}
		claims, err = codec.Verify(ctx, anchor, binding, guard)
	} else {
		claims, err = codec.Verify(ctx, *continuation, binding, guard)
	}
	if err != nil {
		return nil, err
	}
	position := claims.Position
	if position.NextCoverageGap != 0 || position.NextWarningGap != 0 || position.NextByteOffset != 0 || continuation == nil && (position.NextItem != 0 || len(position.LastKey) != 0) || continuation != nil && (position.NextItem == 0 || len(position.LastKey) != 1) {
		return nil, contextpack.ErrCursorInvalid
	}
	after := ""
	if continuation != nil {
		raw, err := hex.DecodeString(position.LastKey[0])
		if err != nil || hex.EncodeToString(raw) != position.LastKey[0] || !moduleLocateID(string(raw)) {
			return nil, contextpack.ErrCursorInvalid
		}
		after = string(raw)
	}
	selected := make([]overview.Module, 0, len(modules))
	seen := make(map[string]bool, len(modules))
	for _, module := range modules {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := module.Ref.ModuleID
		if !moduleLocateID(id) || seen[id] {
			return nil, app.ErrCoreBackend
		}
		seen[id] = true
		if _, err := model.MatchPinnedMember(binding.Scope, &module.Ref, nil); err != nil {
			return nil, app.ErrCoreBackend
		}
		if _, err := model.MatchPinnedSource(binding.Scope, module.Source, nil, 32768); err != nil {
			return nil, app.ErrCoreBackend
		}
		if id > after && (strings.Contains(module.ImportPath, *request.Query) || strings.Contains(module.Root, *request.Query)) {
			selected = append(selected, module)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Ref.ModuleID < selected[j].Ref.ModuleID })
	if continuation != nil && len(selected) == 0 {
		return nil, contextpack.ErrCursorInvalid
	}
	// A smaller limit may be needed when the complete wire envelope is large.
	for limit := int(requestedLimit); limit >= 1; limit-- {
		count := len(selected)
		if count > limit {
			count = limit
		}
		candidates := make([]model.Candidate, 0, count)
		for _, module := range selected[:count] {
			name := path.Base(module.ImportPath)
			if utf8.RuneCountInString(name) > 512 {
				return nil, app.ErrCoreLimit
			}
			ref := module.Ref
			candidates = append(candidates, model.Candidate{Ref: &ref, Name: name, QualifiedName: module.ImportPath, SourceRefs: []model.SourceRef{module.Source}, EvidenceRefs: []model.EvidenceRef{}})
		}
		response := baseResponse(options, binding.Scope)
		response.Data = model.LocateData{Operation: "locate", Candidates: candidates}
		response.Pagination.ReturnedCount = int64(count)
		if count < len(selected) {
			if count == 0 || position.NextItem > 9007199254740991-int64(count) {
				return nil, contextpack.ErrCursorInvalid
			}
			next := contextpack.SignedPosition{NextItem: position.NextItem + int64(count), LastKey: []string{hex.EncodeToString([]byte(selected[count-1].Ref.ModuleID))}}
			if !contextpack.CursorAdvances(position, next) {
				return nil, contextpack.ErrCursorInvalid
			}
			token, err := codec.Issue(ctx, binding, next, guard)
			if err != nil {
				return nil, err
			}
			reason := "limit"
			response.Status = "partial"
			response.Pagination.Truncated, response.Pagination.EnumerationComplete, response.Pagination.OptionalPending, response.Pagination.NextCursor, response.Pagination.LimitReason = true, false, nil, &token, &reason
		}
		blocks, err := encodeResponse(response)
		if errors.Is(err, contextpack.ErrBudgetTooSmall) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := codec.Recheck(ctx, claims, guard); err != nil {
			return nil, err
		}
		return blocks, nil
	}
	return nil, contextpack.ErrBudgetTooSmall
}

func moduleLocateID(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
