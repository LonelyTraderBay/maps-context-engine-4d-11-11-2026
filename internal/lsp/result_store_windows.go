package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

func openResultLock(anchor *os.File) (*os.File, error) {
	return openResultWindows(anchor, resultQuotaLock, false, windows.FILE_OPEN_IF)
}

func openPendingResult(anchor *os.File, name string) (*os.File, error) {
	return openResultWindows(anchor, name, true, windows.FILE_CREATE)
}

func openResultWindows(anchor *os.File, name string, rename bool, disposition uint32) (*os.File, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	if err := runtime.VerifyWindowsPrivateACL(anchor, user.User.Sid); err != nil {
		return nil, err
	}
	descriptor, err := runtime.WindowsPrivateDescriptor(user.User.Sid)
	if err != nil {
		return nil, err
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
	if rename {
		// No concurrent writer or deletion can change these pending bytes.
		share = windows.FILE_SHARE_READ
	}
	return paths.OpenWindowsEntry(anchor, name, descriptor, rename, disposition, share)
}

func resultOwner(file *os.File, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	if err := runtime.VerifyWindowsPrivateACL(file, user.User.Sid); err != nil {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	if !directory {
		if err := paths.VerifyRegularFile(file); err != nil {
			return errors.Join(ErrResultStoreUnsafe, err)
		}
	}
	return nil
}

func acquireResultLock(ctx context.Context, lock *os.File) error {
	ticks := time.NewTicker(10 * time.Millisecond)
	defer ticks.Stop()
	for ctx.Err() == nil {
		var offset windows.Overlapped
		err := windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &offset)
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return err
		}
		select {
		case <-ctx.Done():
		case <-ticks.C:
		}
	}
	return ctx.Err()
}

func releaseResultLock(lock *os.File) error {
	var offset windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, &offset)
}

func inspectPendingResult(dir *paths.Root, file *os.File, _ string, limit int64) (os.FileInfo, []byte, error) {
	if err := errors.Join(dir.Verify(), resultOwner(file, false)); err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	raw, err := io.ReadAll(io.NewSectionReader(file, 0, limit+1))
	return info, raw, err
}

func publishPendingResult(_ *os.File, file *os.File, _, installed string) error {
	if err := paths.RenameWindowsEntry(file, installed, false); err != nil {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	if err := file.Sync(); err != nil {
		return errors.Join(ErrResultUncertain, err)
	}
	return nil
}

func openResultListing(dir *paths.Root, _ *os.File) (*os.File, error) {
	name, err := dir.CanonicalPath()
	if err != nil {
		return nil, err
	}
	fresh, err := paths.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	list, err := fresh.DirectoryHandle()
	if err := errors.Join(err, fresh.Close(), dir.Verify()); err != nil {
		if list != nil {
			return nil, errors.Join(err, list.Close())
		}
		return nil, err
	}
	return list, nil
}

func inspectResultSize(dir *paths.Root, _ *os.File, name string) (_ int64, resultErr error) {
	file, err := dir.OpenRegular(name)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := resultOwner(file, false); err != nil {
		return 0, err
	}
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
