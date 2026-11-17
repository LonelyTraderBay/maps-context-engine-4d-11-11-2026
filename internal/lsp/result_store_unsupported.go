//go:build !linux

package lsp

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func (s *ResultStore) Create(context.Context, model.ScopeVector, string, RenameResultPayload, ResultGuard) (RenameResultHeader, error) {
	return RenameResultHeader{}, ErrResultUnsupported
}

func (s *ResultStore) Read(context.Context, string, string, model.ScopeVector, ResultGuard) (RenameStoredResult, error) {
	return RenameStoredResult{}, ErrResultUnsupported
}
