//go:build linux

package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

func openResultLock(anchor *os.File) (*os.File, error) {
	return openResultAt(anchor, resultQuotaLock, unix.O_RDWR|unix.O_CREAT)
}

func openPendingResult(anchor *os.File, name string) (*os.File, error) {
	return openResultAt(anchor, name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
}

func openResultAt(anchor *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(anchor.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func resultOwner(file *os.File, directory bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	want := uint32(unix.S_IFREG)
	if directory {
		want = unix.S_IFDIR
	}
	if st.Mode&unix.S_IFMT != want || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || !directory && st.Nlink != 1 {
		return ErrResultStoreUnsafe
	}
	return nil
}

func acquireResultLock(ctx context.Context, lock *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func releaseResultLock(lock *os.File) error { return unix.Flock(int(lock.Fd()), unix.LOCK_UN) }

func inspectPendingResult(dir *paths.Root, _ *os.File, name string, limit int64) (os.FileInfo, []byte, error) {
	file, err := dir.OpenRegular(name)
	if err != nil {
		return nil, nil, err
	}
	info, statErr := file.Stat()
	raw, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	return info, raw, errors.Join(statErr, readErr, file.Close())
}

func publishPendingResult(anchor *os.File, _ *os.File, pending, installed string) error {
	if err := unix.Renameat2(int(anchor.Fd()), pending, int(anchor.Fd()), installed, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
			return errors.Join(ErrResultUnsupported, err)
		}
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	syncDir, err := openResultListing(nil, anchor)
	if err != nil {
		return errors.Join(ErrResultUncertain, err)
	}
	if err := errors.Join(syncDir.Sync(), syncDir.Close()); err != nil {
		return errors.Join(ErrResultUncertain, err)
	}
	return nil
}

func openResultListing(_ *paths.Root, anchor *os.File) (*os.File, error) {
	return openResultAt(anchor, ".", unix.O_RDONLY|unix.O_DIRECTORY)
}

func inspectResultSize(_ *paths.Root, anchor *os.File, name string) (int64, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(int(anchor.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || st.Nlink != 1 {
		return 0, ErrResultStoreUnsafe
	}
	return st.Size, nil
}
