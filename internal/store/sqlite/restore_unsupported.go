//go:build !linux

package sqlite

import (
	"context"
	"time"
)

func RestoreProtectedStore(context.Context, string, string, bool, time.Duration) (ProtectedRestoreSummary, error) {
	return ProtectedRestoreSummary{}, ErrStateUnsupported
}
func RecoverProtectedStore(context.Context, string, string, string, bool, time.Duration) (ProtectedRestoreSummary, error) {
	return ProtectedRestoreSummary{}, ErrStateUnsupported
}
