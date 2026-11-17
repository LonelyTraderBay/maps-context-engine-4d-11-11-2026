package runtime

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// RuntimeConfigStore is an adapter over an already-authorized directory and a
// single validated file name. The caller owns the root and its lifetime.
type RuntimeConfigStore struct {
	root *paths.Root
	name string
}

func NewRuntimeConfigStore(root *paths.Root, name string) (*RuntimeConfigStore, error) {
	if root == nil || !validRuntimeConfigName(name) {
		return nil, ErrConfigUnsupported
	}
	return &RuntimeConfigStore{root: root, name: name}, nil
}

func (s *RuntimeConfigStore) Read(ctx context.Context, maxBytes int) ([]byte, bool, error) {
	if s == nil || ctx == nil {
		return nil, false, ErrConfigUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	data, present, err := readRuntimeConfigContextAt(ctx, s.root, s.name, maxBytes)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return data, present, nil
}

func (s *RuntimeConfigStore) Set(ctx context.Context, request []byte, availableCPUs int) (ports.RuntimeConfigWriteResult, error) {
	if s == nil {
		return ports.RuntimeConfigWriteResult{State: "not_applied"}, ErrConfigUnsupported
	}
	return SetRuntimeConfigAt(ctx, s.root, s.name, request, availableCPUs)
}

var _ ports.RuntimeConfigStore = (*RuntimeConfigStore)(nil)
