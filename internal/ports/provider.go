package ports

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// ProviderCapture exposes identity and range checks on immutable captured bytes.
type ProviderCapture interface {
	Manifest() model.CaptureManifest
	CheckCapturedRange(string, model.ByteRange) (string, error)
}

// ProviderJob binds an authorized scratch capture to one provider invocation.
// Recheck retains and verifies the owner's authority over the job and scratch.
type ProviderJob struct {
	Start   model.ProviderStart
	Capture ProviderCapture
	Recheck func(context.Context) error
}

// ProviderSink stages a validated batch in BUILDING state. The sink must honor
// cancellation and leave publication to the coordinator after a validated Done.
type ProviderSink func(context.Context, model.ProviderBatch) error

type LanguageProvider interface {
	Analyze(context.Context, ProviderJob, ProviderSink) (model.ProviderDone, error)
}
