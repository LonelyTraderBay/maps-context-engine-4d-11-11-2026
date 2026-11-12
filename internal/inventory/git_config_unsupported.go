//go:build !linux

package inventory

import (
	"context"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func ParseGitConfigFields(context.Context, string, []byte, *policy.Scanner, int, int, func(context.Context) error) (GitConfigFields, error) {
	return GitConfigFields{}, paths.ErrUnsupported
}
