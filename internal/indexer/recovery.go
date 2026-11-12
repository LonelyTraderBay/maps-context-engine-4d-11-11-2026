package indexer

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var ErrInvalidRecovery = errors.New("invalid recovery request")

type RecoveryRequest struct {
	RecoveryID        string
	Lease             ports.IndexRecoveryLease
	Store             ports.RecoveryStore
	Authorize         ports.RecoveryAuthorizer
	AfterGenerationID string
	BatchSize         int
	MaxCandidates     int
}

type RecoveryReport struct {
	Failed           int
	LastGenerationID string
	MayHaveMore      bool
	Pending          *ports.RecoveryCandidate
}

// RecoverAbandoned marks only a bounded number of abandoned BUILDING rows.
// It never retries an uncertain write: Pending identifies the exact candidate
// which must be reconciled by the caller before resuming.
func RecoverAbandoned(ctx context.Context, request RecoveryRequest) (RecoveryReport, error) {
	var report RecoveryReport
	if ctx == nil || request.Lease == nil || request.Store == nil || request.Authorize == nil || request.BatchSize < 1 || request.BatchSize > 128 || request.MaxCandidates < 1 || request.MaxCandidates > 4096 {
		return report, ErrInvalidRecovery
	}
	if _, ok := ctx.Deadline(); !ok {
		return report, ErrInvalidRecovery
	}
	after := request.AfterGenerationID
	report.LastGenerationID = after
	for report.Failed < request.MaxCandidates {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		limit := request.BatchSize
		if limit > request.MaxCandidates-report.Failed {
			limit = request.MaxCandidates - report.Failed
		}
		candidates, err := request.Store.ListBuildingForRecovery(ctx, request.RecoveryID, request.Lease, after, limit, request.Authorize)
		if err != nil {
			return report, err
		}
		if len(candidates) > limit {
			return report, ErrInvalidRecovery
		}
		for _, candidate := range candidates {
			if candidate.GenerationID <= after {
				return report, ErrInvalidRecovery
			}
			if err := request.Store.FailAbandonedGeneration(ctx, request.RecoveryID, request.Lease, candidate, request.Authorize); err != nil {
				pending := candidate
				report.Pending = &pending
				return report, err
			}
			report.Failed++
			after = candidate.GenerationID
			report.LastGenerationID = after
		}
		if len(candidates) < limit {
			return report, nil
		}
	}
	report.MayHaveMore = true
	return report, nil
}
