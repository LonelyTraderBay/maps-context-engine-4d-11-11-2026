package lsp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type RenameWireOptions struct {
	RequestID            string
	MaxOutputBytes       int64
	EffectiveMaxOutput   int64
	MaxEdits             int
	DeliveryMode         string
	VerifyStructuredHost func(context.Context) error
}

// ReadResultWirePage measures the exact serialized envelope, including any
// duplicated MCP content, and reduces the item allowance until it fits.
// Structured-only delivery requires a current host capability check.
func (s *ResultStore) ReadResultWirePage(ctx context.Context, codec *contextpack.CursorCodec, resultToken, cursor, principal string, cursorGuard contextpack.CursorGuard, resultGuardFn ResultGuard, options RenameWireOptions) (model.Response, [][]byte, error) {
	if s == nil || ctx == nil || codec == nil || options.EffectiveMaxOutput < 4096 || options.EffectiveMaxOutput > 131072 {
		return model.Response{}, nil, ErrResultInvalid
	}
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = 24576
		if options.MaxOutputBytes > options.EffectiveMaxOutput {
			options.MaxOutputBytes = options.EffectiveMaxOutput
		}
	}
	if options.MaxEdits == 0 {
		options.MaxEdits = 512
	}
	if options.DeliveryMode == "" {
		options.DeliveryMode = "mcp_dual"
	}
	if options.MaxOutputBytes < 4096 || options.MaxOutputBytes > options.EffectiveMaxOutput || options.MaxEdits < 1 || options.MaxEdits > 512 {
		return model.Response{}, nil, ErrResultInvalid
	}
	switch options.DeliveryMode {
	case "http", "cli", "mcp_text", "mcp_dual", "mcp_structured_verified":
	default:
		return model.Response{}, nil, ErrResultInvalid
	}
	checkHost := func() error {
		if options.DeliveryMode != "mcp_structured_verified" {
			return nil
		}
		if options.VerifyStructuredHost == nil {
			return ErrResultInvalid
		}
		if err := options.VerifyStructuredHost(ctx); err != nil {
			return err
		}
		return ctx.Err()
	}
	if err := checkHost(); err != nil {
		return model.Response{}, nil, err
	}
	claims, err := codec.VerifyPrincipal(ctx, resultToken, "rename-result", principal, cursorGuard)
	if err != nil {
		return model.Response{}, nil, err
	}
	factor := int64(1)
	if options.DeliveryMode == "mcp_dual" {
		factor = 2
	}
	itemLimit := options.MaxOutputBytes / factor
	if itemLimit > 131072 {
		itemLimit = 131072
	}
	for attempt := 0; attempt < 32 && itemLimit >= 4; attempt++ {
		page, err := s.ReadResultPage(ctx, codec, resultToken, cursor, principal, cursorGuard, resultGuardFn, options.MaxEdits, int(itemLimit))
		if err != nil {
			if errors.Is(err, ErrResultTooLarge) && options.MaxOutputBytes < options.EffectiveMaxOutput {
				return model.Response{}, nil, contextpack.ErrBudgetTooSmall
			}
			return model.Response{}, nil, err
		}
		pending := page.RemainingEdits + page.RemainingConflicts
		zero := int64(0)
		truncated := pending > 0
		var reason *string
		status := "ok"
		if truncated {
			status = "partial"
			value := "OUTPUT_BUDGET"
			reason = &value
		}
		scope := claims.Binding.Scope
		response := model.Response{SchemaVersion: 1, RequestID: options.RequestID, Status: status, Scope: &scope, Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "disk_only", Consistency: "captured_bytes_with_observed_stability", SourceView: "indexed_snapshot"}, Coverage: model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "Preview edits require full delivery and workspace validation."}, Readiness: "needs_context", Data: page.Data, Pagination: model.Pagination{Truncated: truncated, EnumerationComplete: true, ReturnedCount: int64(len(page.Data.Edits) + len(page.Data.Conflicts)), RequiredPending: &pending, OptionalPending: &zero, NextCursor: page.NextCursor, LimitReason: reason}, Budget: model.Budget{MaxOutputBytes: options.MaxOutputBytes, DeliveryMode: options.DeliveryMode}, Warnings: []model.Gap{}}
		blocks, actual, err := settleRenameResponse(&response)
		if errors.Is(err, contextpack.ErrBudgetTooSmall) {
			// Account for unused space below the previous item limit. Subtracting
			// only the overage can repeatedly select the same whole items.
			overhead := actual/factor - int64(page.SerializedItemBytes)
			nextLimit := options.MaxOutputBytes/factor - overhead
			if nextLimit >= itemLimit {
				nextLimit = itemLimit - 1
			}
			itemLimit = nextLimit
			continue
		}
		if err != nil {
			return model.Response{}, nil, err
		}
		if err = s.scanner.CheckContent(blocks[0]); err != nil {
			return model.Response{}, nil, err
		}
		if err = codec.Recheck(ctx, claims, cursorGuard); err != nil {
			return model.Response{}, nil, err
		}
		result, err := s.Read(ctx, claims.Binding.ResultID, principal, claims.Binding.Scope, resultGuardFn)
		if err != nil {
			return model.Response{}, nil, err
		}
		if result.Header.ResultDigest != claims.Binding.ResultDigest {
			return model.Response{}, nil, ErrResultInvalid
		}
		if err = checkHost(); err != nil {
			return model.Response{}, nil, err
		}
		if err = codec.Recheck(ctx, claims, cursorGuard); err != nil {
			return model.Response{}, nil, err
		}
		pageBinding := claims.Binding
		pageBinding.Purpose = "rename-page"
		if cursor != "" {
			if _, err = codec.Verify(ctx, cursor, pageBinding, cursorGuard); err != nil {
				return model.Response{}, nil, err
			}
		}
		if page.NextCursor != nil {
			if _, err = codec.Verify(ctx, *page.NextCursor, pageBinding, cursorGuard); err != nil {
				return model.Response{}, nil, err
			}
		}
		if err = s.validateResult(result, principal, claims.Binding.Scope); err != nil {
			return model.Response{}, nil, err
		}
		return response, blocks, nil
	}
	return model.Response{}, nil, contextpack.ErrBudgetTooSmall
}

func settleRenameResponse(response *model.Response) ([][]byte, int64, error) {
	count := int64(0)
	for i := 0; i < 8; i++ {
		response.Budget.SerializedBytes = count
		raw, err := json.Marshal(response)
		if err != nil {
			return nil, 0, ErrResultInvalid
		}
		actual := int64(len(raw))
		if response.Budget.DeliveryMode == "mcp_dual" {
			actual *= 2
		}
		if actual == count {
			if actual > response.Budget.MaxOutputBytes {
				return nil, actual, contextpack.ErrBudgetTooSmall
			}
			blocks := [][]byte{raw}
			if response.Budget.DeliveryMode == "mcp_dual" {
				blocks = append(blocks, append([]byte(nil), raw...))
			}
			if _, err = model.DecodeResponseJSON(raw, int(response.Budget.MaxOutputBytes), 4, "rename_preview", false); err != nil {
				return nil, actual, ErrResultInvalid
			}
			if err = model.ValidateDeliveredBytes(response.Budget, blocks); err != nil {
				return nil, actual, ErrResultInvalid
			}
			return blocks, actual, nil
		}
		count = actual
	}
	return nil, 0, ErrResultInvalid
}
