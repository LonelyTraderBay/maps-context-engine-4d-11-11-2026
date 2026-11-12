//go:build !linux

package inventory

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func WalkCandidates(context.Context, *paths.Root, WalkBudget, func(context.Context) error, WalkAuthorization, WalkExclusion) (CandidateWalk, error) {
	return CandidateWalk{}, paths.ErrUnsupported
}
