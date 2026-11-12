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

func acquireOwnedLock(ctx context.Context, root *paths.Root, name string, exclusive bool, wait time.Duration, busy error) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := lockDirectory(root)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	fd, openErr := unix.Openat(int(dir.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	closeErr := dir.Close()
	if openErr != nil {
		return nil, errors.Join(openErr, closeErr)
	}
	file := os.NewFile(uintptr(fd), "owned-store-lock")
	if closeErr != nil {
		return nil, errors.Join(closeErr, file.Close())
	}
	if err := lockOwnerOnly(file, false); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	bounded, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	mode := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		mode = unix.LOCK_EX | unix.LOCK_NB
	}
	for {
		if err := bounded.Err(); err != nil {
			if ctx.Err() != nil {
				return nil, errors.Join(ctx.Err(), file.Close())
			}
			return nil, errors.Join(busy, file.Close())
		}
		err = unix.Flock(int(file.Fd()), mode)
		if err == nil {
			if err = bounded.Err(); err == nil {
				err = verifyOwnedLock(root, file, name)
			}
			if err != nil {
				return nil, errors.Join(err, releaseOwnedLock(file))
			}
			return file, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, errors.Join(err, file.Close())
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-bounded.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func verifyOwnedLock(root *paths.Root, file *os.File, name string) error {
	if root == nil || file == nil {
		return ErrInvalidLease
	}
	dir, err := lockDirectory(root)
	if err != nil {
		return errors.Join(ErrLockReplaced, err)
	}
	if err := dir.Close(); err != nil {
		return err
	}
	if err := lockOwnerOnly(file, false); err != nil {
		return errors.Join(ErrLockReplaced, err)
	}
	current, err := root.OpenRegular(name)
	if err != nil {
		return errors.Join(ErrLockReplaced, err)
	}
	a, errA := file.Stat()
	b, errB := current.Stat()
	closeErr := current.Close()
	if err := errors.Join(errA, errB, closeErr); err != nil {
		return err
	}
	if !os.SameFile(a, b) {
		return ErrLockReplaced
	}
	return root.Verify()
}

func lockDirectory(root *paths.Root) (*os.File, error) {
	if err := root.Verify(); err != nil {
		return nil, err
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	if err := lockOwnerOnly(dir, true); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	return dir, nil
}

func lockOwnerOnly(file *os.File, directory bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	want := uint32(unix.S_IFREG)
	if directory {
		want = unix.S_IFDIR
	}
	if st.Mode&unix.S_IFMT != want || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || (!directory && st.Nlink != 1) {
		return ErrLockUnsafe
	}
	return nil
}

func releaseOwnedLock(file *os.File) error {
	return errors.Join(unix.Flock(int(file.Fd()), unix.LOCK_UN), file.Close())
}

func errorsJoinHold(err error, hold *LifecycleHold) error { return errors.Join(err, hold.Close()) }
func errorsJoinWriter(err error, file *os.File, hold *LifecycleHold) error {
	return errors.Join(err, releaseOwnedLock(file), hold.Close())
}
