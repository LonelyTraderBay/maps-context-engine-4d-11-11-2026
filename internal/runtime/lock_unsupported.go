//go:build !linux && !windows

package runtime

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func acquireOwnedLock(context.Context, *paths.Root, string, bool, time.Duration, error) (*os.File, error) {
	return nil, paths.ErrUnsupported
}
func verifyOwnedLock(*paths.Root, *os.File, string) error { return paths.ErrUnsupported }
func releaseOwnedLock(*os.File) error                     { return paths.ErrUnsupported }
func errorsJoinHold(err error, hold *LifecycleHold) error { return errors.Join(err, hold.Close()) }
func errorsJoinWriter(err error, file *os.File, hold *LifecycleHold) error {
	return errors.Join(err, releaseOwnedLock(file), hold.Close())
}
