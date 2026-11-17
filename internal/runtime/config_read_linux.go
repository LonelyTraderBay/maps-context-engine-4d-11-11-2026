//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// Readers borrow an existing cooperative lock without creating state. This
// waits out a live setter's pending slot; an orphaned pending slot still fails
// closed once the lock is acquired. No file is recovered, removed or adopted.
func readRuntimeConfigContextAt(ctx context.Context, root *paths.Root, name string, maxBytes int) (data []byte, present bool, resultErr error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		if err := bounded.Err(); err != nil {
			return nil, false, err
		}
		lock, err := root.OpenRegular(runtimeConfigLock)
		if errors.Is(err, os.ErrNotExist) {
			data, present, err = ReadRuntimeConfigAt(root, name, maxBytes)
			// A first setter may have created the lock after our lookup. Reopen
			// once so its transient pending slot is handled under that lock.
			if attempt == 0 && (errors.Is(err, ErrConfigPending) || errors.Is(err, ErrConfigUnstable)) {
				continue
			}
			return data, present, errors.Join(err, bounded.Err())
		}
		if err != nil {
			return nil, false, err
		}
		defer func() {
			resultErr = errors.Join(resultErr, lock.Close())
			if resultErr != nil {
				data, present = nil, false
			}
		}()
		if err := verifyConfigLock(root, lock); err != nil {
			return nil, false, err
		}
		if err := acquireConfigLockMode(bounded, lock, unix.LOCK_SH); err != nil {
			return nil, false, err
		}
		defer func() { resultErr = errors.Join(resultErr, unix.Flock(int(lock.Fd()), unix.LOCK_UN)) }()
		if err := verifyConfigLock(root, lock); err != nil {
			return nil, false, err
		}
		data, present, err = ReadRuntimeConfigAt(root, name, maxBytes)
		return data, present, errors.Join(err, bounded.Err(), verifyConfigLock(root, lock))
	}
	return nil, false, ErrConfigUnstable
}
