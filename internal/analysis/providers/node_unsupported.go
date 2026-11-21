//go:build !linux

package providers

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func (*NodeProvider) Analyze(context.Context, ports.ProviderJob, ports.ProviderSink) (model.ProviderDone, error) {
	return model.ProviderDone{}, ErrUnsupported
}
