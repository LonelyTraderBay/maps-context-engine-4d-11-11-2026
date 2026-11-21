package compose

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

// readSymbolCatalog uses the published index keyset when a whole-workspace
// relation projection exceeds its cap. Continuations follow the same pinned
// generation even when a later generation becomes active.
func (e *ExplorerReader) readSymbolCatalog(ctx context.Context, p app.CorePrincipal, observed ScopeObservation,
	binding contextpack.CursorBinding, guard contextpack.CursorGuard, claims *contextpack.SignedCursor,
	nodeLimit int64, options app.CoreCallOptions,
) ([][]byte, error) {
	position := contextpack.SignedPosition{LastKey: []string{}}
	after := ""
	if claims != nil {
		position = claims.Position
		var err error
		after, err = search.DecodeLocateKey(position.LastKey)
		if err != nil || after == "" || position.NextItem == 0 || position.NextCoverageGap != 0 ||
			position.NextWarningGap != 0 || position.NextByteOffset != 0 || binding.ResultID != "" {
			return nil, contextpack.ErrCursorInvalid
		}
	}
	input, _, err := e.publishedRead(p, observed)
	if err != nil {
		return nil, err
	}
	member := observed.Vector.Members[0]
	limit := min(100, int(nodeLimit), int(e.core.limits.MaxGraphNodes))
	found, err := e.core.Reader.Index.Search(ctx, search.Request{
		ProfileID: member.ProfileID, GenerationID: member.GenerationID, Mode: "catalog", Keyset: true, AfterID: after,
		Limit: limit, MaxOutputBytes: 1 << 20, MaxManifestBytes: input.MaxManifestBytes, MaxFileBytes: input.MaxFileBytes,
		AuthorizeGeneration: input.AuthorizeGeneration, AuthorizePath: input.AuthorizePath, Scanner: input.Scanner,
	})
	if err != nil {
		return nil, err
	}
	if found.WorktreeID != member.WorktreeID || found.ProfileID != member.ProfileID || found.GenerationID != member.GenerationID ||
		found.IndexIncarnation != member.IndexIncarnation || found.SnapshotDigest != member.SnapshotDigest {
		return nil, ErrScopeChanged
	}
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	nodes := make([]model.GraphNode, 0, len(found.Symbols))
	last := after
	for _, hit := range found.Symbols {
		if hit.Symbol.SymbolID <= last {
			return nil, app.ErrCoreBackend
		}
		last = hit.Symbol.SymbolID
		nodes = append(nodes, overview.CapturedSymbolNode(identity, hit.Symbol, hit.Source))
	}
	if claims != nil && len(nodes) == 0 || found.Truncated && (len(nodes) == 0 || found.NextKey != last) {
		return nil, app.ErrCoreBackend
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, observed.Binding.Policy.MaxOutputBytes, e.core.limits.MaxOutputBytes)
	base := baseResponse(options, observed.Vector)
	base.Status = "partial"
	base.Coverage = model.Coverage{Status: "partial", Capabilities: []model.Capability{},
		ScopeDescription: "Bounded symbol catalog for the pinned workspace.",
		Gaps: []model.Gap{{Code: "GRAPH_PROJECTION_LIMIT", Reason: "Symbol pages are available within the viewport. Workspace relations were not enumerated.",
			SourceRefs: []model.SourceRef{}}}}
	fit := func(count int) ([][]byte, error) {
		page := base
		edges := []model.GraphEdge{}
		digest, err := model.SubgraphDigest(observed.Vector, nodes[:count], edges, maxProjectionBytes)
		if err != nil {
			return nil, err
		}
		page.Data = model.GraphData{Operation: "graph_query", Nodes: nodes[:count], Edges: edges, SubgraphDigest: digest}
		page.Pagination.ReturnedCount = int64(count)
		if found.Truncated || count < len(nodes) {
			keys, err := search.EncodeLocateKey(found.Symbols[count-1].Symbol.SymbolID)
			if err != nil {
				return nil, err
			}
			next := contextpack.SignedPosition{NextItem: position.NextItem + int64(count), LastKey: keys}
			if !contextpack.CursorAdvances(position, next) {
				return nil, contextpack.ErrCursorInvalid
			}
			token, err := e.codec.Issue(ctx, binding, next, guard)
			if err != nil {
				return nil, err
			}
			reason := "limit"
			page.Pagination = model.Pagination{Truncated: true, ReturnedCount: int64(count), NextCursor: &token, LimitReason: &reason}
		}
		return encodeResponse(page)
	}
	var blocks [][]byte
	for count := len(nodes); ; count = max(1, count/2) {
		blocks, err = fit(count)
		if err == nil {
			break
		}
		if count <= 1 || !errors.Is(err, contextpack.ErrBudgetTooSmall) {
			return nil, err
		}
	}
	if claims != nil {
		if err = e.codec.Recheck(ctx, *claims, guard); err != nil {
			return nil, err
		}
	}
	return blocks, nil
}
