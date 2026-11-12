// Package ports contains small interfaces with actual consumers at module
// boundaries; it does not grant access to external resources.
package ports

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// RuntimeConfigWriteResult preserves the commit state when a later flush or
// cleanup fails. Unknown or applied results must not be blindly retried.
type RuntimeConfigWriteResult struct {
	State  string
	Config model.RuntimeConfig
	Digest string
}

// RuntimeConfigStore is a caller-authorized configuration location. The
// adapter implements bounded reads and cooperative, atomic file CAS.
type RuntimeConfigStore interface {
	Read(ctx context.Context, maxBytes int) ([]byte, bool, error)
	Set(ctx context.Context, request []byte, availableCPUs int) (RuntimeConfigWriteResult, error)
}
