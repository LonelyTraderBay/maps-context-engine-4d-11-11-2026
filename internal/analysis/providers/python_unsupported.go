//go:build !linux

package providers

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func (*PythonProvider) Analyze(context.Context, ports.ProviderJob, ports.ProviderSink) (model.ProviderDone, error) {
	return model.ProviderDone{}, ErrUnsupported
}

func PythonStdlibDigest(context.Context, string) (string, error) {
	return "", ErrUnsupported
}
