//go:build !linux

package lsp

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type Session struct{}

func StartSession(context.Context, SessionConfig) (*Session, error) {
	return nil, ErrLSPUnsupported
}

func (*Session) Preview(context.Context, model.PositionSelector, string) (ValidatedWorkspaceEdit, error) {
	return ValidatedWorkspaceEdit{}, ErrLSPUnsupported
}

func (*Session) Close(context.Context) error { return ErrLSPUnsupported }
