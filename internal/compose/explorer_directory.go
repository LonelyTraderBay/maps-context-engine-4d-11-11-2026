package compose

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

const explorerDirectoryOrder = "explorer-directory-v1"

func (e *ExplorerReader) readDirectory(ctx context.Context, principal app.CorePrincipal, request model.GraphQueryRequest,
	options app.CoreCallOptions) ([][]byte, error) {
	cursor := request.Cursor
	request.Cursor = nil
	digest, err := model.RequestDigest("graph_query", principal.ID, "GraphQueryRequest", request, 65536, 1)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	guard := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if binding.PrincipalDigest != principal.Digest || binding.BindingDigest != principal.BindingDigest ||
			binding.RequestDigest != digest || binding.OrderVersion != explorerDirectoryOrder {
			return contextpack.ErrCursorInvalid
		}
		_, err := e.core.Reader.Recheck(ctx, principal, binding.Scope)
		return err
	}
	var observed ScopeObservation
	var position contextpack.SignedPosition
	if cursor == nil {
		observed, err = e.core.resolveRequestScope(ctx, principal, request.Scope)
	} else {
		var claims contextpack.SignedCursor
		claims, err = e.codec.VerifyPrincipal(ctx, *cursor, "list", principal.Digest, guard)
		if err == nil {
			position = claims.Position
			observed, err = e.core.Reader.Recheck(ctx, principal, claims.Binding.Scope)
		}
	}
	if err != nil {
		return nil, err
	}
	input, _, err := e.publishedRead(principal, observed)
	if err != nil {
		return nil, err
	}
	directory := *request.Directory
	checkPath := e.core.Authority.CheckMetadataPath
	if checkPath == nil {
		checkPath = policy.CheckConfiguredPath
	}
	allowed := func(name string) bool {
		return checkPath(name, observed.Binding.Policy.DeniedGlobs) == nil &&
			input.Scanner.CheckContent([]byte(name)) == nil
	}
	if directory != "" && !allowed(directory) {
		return nil, ErrAuthority
	}
	generation, files, err := e.core.Reader.Index.ReadPublishedInventory(ctx, input)
	if err != nil {
		return nil, err
	}
	member := observed.Vector.Members[0]
	if generation.Identity.Incarnation != member.IndexIncarnation || generation.SnapshotDigest != member.SnapshotDigest {
		return nil, ErrScopeChanged
	}
	summary := buildExplorerDirectory(directory, files, allowed)
	if position.NextItem < 0 || position.NextItem > int64(len(summary.Entries)) {
		return nil, contextpack.ErrCursorInvalid
	}
	start := int(position.NextItem)
	count := min(100, len(summary.Entries)-start)
	if request.NodeLimit != nil {
		count = min(count, int(*request.NodeLimit))
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, observed.Binding.Policy.MaxOutputBytes, e.core.limits.MaxOutputBytes)
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: principal.Digest, BindingDigest: principal.BindingDigest,
		Scope: observed.Vector, RequestDigest: digest, OrderVersion: explorerDirectoryOrder}
	nodes, edges := []model.GraphNode{}, []model.GraphEdge{}
	emptyDigest, err := model.SubgraphDigest(observed.Vector, nodes, edges, maxProjectionBytes)
	if err != nil {
		return nil, err
	}
	for {
		page := summary
		page.Entries = summary.Entries[start : start+count]
		response := baseResponse(options, observed.Vector)
		response.Coverage.ScopeDescription = "Authorized file inventory of the captured directory."
		response.Data = model.GraphData{Operation: "graph_query", Nodes: nodes, Edges: edges, SubgraphDigest: emptyDigest, Directory: &page}
		response.Pagination.ReturnedCount = int64(count)
		if end := start + count; end < len(summary.Entries) {
			next := contextpack.SignedPosition{NextItem: int64(end), LastKey: []string{}}
			token, err := e.codec.Issue(ctx, binding, next, guard)
			if err != nil {
				return nil, err
			}
			reason := "limit"
			pending := int64(len(summary.Entries) - end)
			response.Status = "partial"
			response.Pagination.Truncated = true
			response.Pagination.EnumerationComplete = false
			response.Pagination.RequiredPending = &pending
			response.Pagination.NextCursor = &token
			response.Pagination.LimitReason = &reason
		}
		blocks, err := encodeResponse(response)
		if err == nil || count <= 1 || !errors.Is(err, contextpack.ErrBudgetTooSmall) {
			return blocks, err
		}
		count /= 2
	}
}

func buildExplorerDirectory(directory string, files []model.ManifestFile, allowed func(string) bool) model.GraphDirectory {
	summary := model.GraphDirectory{Path: directory, Entries: []model.GraphDirectoryEntry{}}
	prefix := ""
	if directory != "" {
		prefix = directory + "/"
	}
	children := make(map[string]model.GraphDirectoryEntry)
	for _, file := range files {
		tail, inside := strings.CutPrefix(file.Path, prefix)
		if !inside || tail == "" || !allowed(file.Path) {
			continue
		}
		first, _, nested := strings.Cut(tail, "/")
		childPath := prefix + first
		if !allowed(childPath) {
			continue
		}
		child := children[childPath]
		child.Path = childPath
		if nested {
			child.Kind = "directory"
		} else {
			child.Kind = "file"
			child.ContentHash = file.ContentHash
			child.ReasonCode = file.ReasonCode
			if file.ContentHash == nil && child.ReasonCode == nil {
				reason := "OUTSIDE_CAPTURE_SCOPE"
				child.ReasonCode = &reason
			}
		}
		if file.ContentHash != nil {
			child.Indexed++
			summary.Indexed++
		} else {
			child.Skipped++
			summary.Skipped++
		}
		children[childPath] = child
	}
	for _, child := range children {
		summary.Entries = append(summary.Entries, child)
	}
	slices.SortFunc(summary.Entries, func(a, b model.GraphDirectoryEntry) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.Path, b.Path)
	})
	return summary
}
