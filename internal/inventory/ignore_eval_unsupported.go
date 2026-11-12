//go:build !linux

package inventory

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func EvaluateIgnoreRules(context.Context, *paths.Root, string, []IgnoreSource, []IgnoreQuery, *policy.Scanner, IgnoreOptions, IgnoreBudget, func(context.Context) error) ([]bool, error) {
	return nil, paths.ErrUnsupported
}
