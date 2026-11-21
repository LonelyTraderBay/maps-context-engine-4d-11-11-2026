package tsprovider

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	domain "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// FrontendProvider owns a pinned worker that can emit both semantic facts
// and the separately sealed frontend analysis for the same profile.
type FrontendProvider struct{ *providers.NodeProvider }

func (p *FrontendProvider) Scan(ctx context.Context, job ports.ProviderJob) (domain.FrontendAnalysis, error) {
	if p == nil {
		return domain.FrontendAnalysis{}, providers.ErrAuthority
	}
	var output domain.FrontendAnalysis
	err := p.NodeProvider.AnalyzeFrontend(ctx, job, func(_ context.Context, job ports.ProviderJob, raw []byte) error {
		var err error
		output, err = domain.DecodeFrontendAnalysis(raw, job)
		return err
	})
	if err != nil {
		return domain.FrontendAnalysis{}, err
	}
	return output, nil
}
