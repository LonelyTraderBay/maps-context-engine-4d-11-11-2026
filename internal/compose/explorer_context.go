package compose

import (
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/explorer"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func (e *ExplorerReader) selectionPack(ctx context.Context, principal app.CorePrincipal, request model.PackRequest,
	options app.CoreCallOptions) (explorer.ReadResult, error) {
	if e.selection == nil {
		return explorer.ReadResult{}, app.ErrCoreUnsupported
	}
	if request.Scope == nil || request.Scope.PinnedVector == nil {
		return explorer.ReadResult{}, app.ErrCoreInput
	}
	pinned := *request.Scope.PinnedVector
	switch ref := request.Selector.(type) {
	case *model.RefSelector:
		if _, err := model.MatchPinnedMember(pinned, ref.Ref, ref.ServiceID); err != nil {
			return explorer.ReadResult{}, app.ErrCoreInput
		}
	case *model.PositionSelector:
		if pinned.Kind != "single" || len(pinned.Members) != 1 || len(pinned.UnavailableMembers) != 0 {
			return explorer.ReadResult{}, app.ErrCoreInput
		}
	default:
		return explorer.ReadResult{}, app.ErrCoreInput
	}
	if _, err := e.core.Reader.Recheck(ctx, principal, pinned); err != nil {
		return explorer.ReadResult{}, coreStoreError(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return explorer.ReadResult{}, app.ErrCoreInput
	}
	const coreOperation = "mce4d_context_pack"
	reply, err := e.selection.Call(ctx, coreOperation, raw, options)
	if err != nil {
		return explorer.ReadResult{}, err
	}
	if len(reply.Blocks) != 1 {
		return explorer.ReadResult{}, app.ErrCoreBackend
	}
	body := reply.Blocks[0]
	response, err := e.registry.DecodeResponse(contracts.HTTP, "POST /context/pack", body, int(options.MaxOutputBytes), 1)
	if err != nil || response.Scope != nil && model.ComparePinnedScope(pinned, *response.Scope, 32768) != nil ||
		response.Error == nil && response.Scope == nil {
		return explorer.ReadResult{}, app.ErrCoreBackend
	}
	recheck := func(ctx context.Context) error {
		if response.Scope == nil {
			return coreStoreError(e.core.Authority.samePrincipal(ctx, principal))
		}
		return coreStoreError(e.core.Authority.AuthorizeResponse(ctx, principal, coreOperation, request, response))
	}
	return explorer.ReadResult{Body: body, Recheck: recheck}, nil
}
