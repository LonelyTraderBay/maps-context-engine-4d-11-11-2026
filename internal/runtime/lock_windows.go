package runtime

import (
	"context"
	"errors"
	"os"
	"time"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/windows"
)

func acquireOwnedLock(ctx context.Context, root *paths.Root, name string, exclusive bool, wait time.Duration, busy error) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	dir, err := windowsLockDirectory(root, owner.User.Sid)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	file, openErr := openWindowsLock(dir, name, owner.User.Sid)
	closeErr := dir.Close()
	if openErr != nil {
		return nil, errors.Join(openErr, closeErr)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, file.Close())
	}
	if err := verifyOwnedLock(root, file, name); err != nil {
		return nil, errors.Join(err, file.Close())
	}

	bounded, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	ticks := time.NewTicker(10 * time.Millisecond)
	defer ticks.Stop()
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	for bounded.Err() == nil {
		var offset windows.Overlapped
		err = windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &offset)
		if err == nil {
			err = verifyOwnedLock(root, file, name)
			if err == nil && bounded.Err() == nil {
				return file, nil
			}
			if err == nil {
				err = ctx.Err()
				if err == nil {
					err = busy
				}
			}
			return nil, errors.Join(err, releaseOwnedLock(file))
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errors.Join(err, file.Close())
		}
		select {
		case <-bounded.Done():
		case <-ticks.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return nil, errors.Join(busy, file.Close())
}

func openWindowsLock(dir *os.File, name string, owner *windows.SID) (*os.File, error) {
	switch name {
	case "lifecycle.lock", "writer.lock", "catalog.lock":
	default:
		return nil, ErrInvalidLease
	}
	descriptor, err := WindowsPrivateDescriptor(owner)
	if err != nil {
		return nil, err
	}
	return paths.OpenWindowsEntry(dir, name, descriptor, false, windows.FILE_OPEN_IF, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
}

func windowsLockDirectory(root *paths.Root, owner *windows.SID) (*os.File, error) {
	dir, err := root.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	if err := VerifyWindowsPrivateACL(dir, owner); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	return dir, nil
}

func verifyOwnedLock(root *paths.Root, file *os.File, name string) error {
	if root == nil || file == nil {
		return ErrInvalidLease
	}
	owner, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	dir, err := windowsLockDirectory(root, owner.User.Sid)
	if err != nil {
		return errors.Join(ErrLockReplaced, err)
	}
	if err := dir.Close(); err != nil {
		return err
	}
	if err := errors.Join(paths.VerifyRegularFile(file), VerifyWindowsPrivateACL(file, owner.User.Sid)); err != nil {
		return errors.Join(ErrLockReplaced, err)
	}
	current, err := root.OpenRegular(name)
	if err != nil {
		return errors.Join(ErrLockReplaced, err)
	}
	var a, b windows.ByHandleFileInformation
	errA := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &a)
	errB := windows.GetFileInformationByHandle(windows.Handle(current.Fd()), &b)
	if err := errors.Join(errA, errB, current.Close()); err != nil {
		return err
	}
	if a.VolumeSerialNumber != b.VolumeSerialNumber || a.FileIndexHigh != b.FileIndexHigh || a.FileIndexLow != b.FileIndexLow {
		return ErrLockReplaced
	}
	return root.Verify()
}

// WindowsPrivateDescriptor creates the descriptor shared by owned Windows locks and spools.
func WindowsPrivateDescriptor(owner *windows.SID) (*windows.SECURITY_DESCRIPTOR, error) {
	if owner == nil || !owner.IsValid() {
		return nil, ErrLockUnsafe
	}
	return windows.SecurityDescriptorFromString("O:" + owner.String() + "D:P(A;;FA;;;SY)(A;;FA;;;" + owner.String() + ")")
}

// VerifyWindowsPrivateACL accepts only a protected DACL whose
// allow entries name the current owner or LocalSystem.
func VerifyWindowsPrivateACL(file *os.File, owner *windows.SID) error {
	return verifyWindowsACL(file, owner, nil)
}

// VerifyWindowsReadACL permits one named reader to read and traverse owned
// resources. The owner and LocalSystem retain control of the DACL.
func VerifyWindowsReadACL(file *os.File, owner, reader *windows.SID) error {
	if reader == nil || !reader.IsValid() || reader.IsWellKnown(windows.WinLocalSystemSid) || owner == nil || reader.Equals(owner) {
		return ErrLockUnsafe
	}
	return verifyWindowsACL(file, owner, reader)
}

func verifyWindowsACL(file *os.File, owner, reader *windows.SID) error {
	if file == nil || owner == nil || !owner.IsValid() {
		return ErrLockUnsafe
	}
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return errors.Join(ErrLockUnsafe, err)
	}
	actual, _, err := descriptor.Owner()
	if err != nil || actual == nil || !actual.Equals(owner) {
		return errors.Join(ErrLockUnsafe, err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return errors.Join(ErrLockUnsafe, err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return errors.Join(ErrLockUnsafe, err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return errors.Join(ErrLockUnsafe, err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return ErrLockUnsafe
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.Equals(owner) || sid.Equals(system) {
			continue
		}
		if reader == nil || !sid.Equals(reader) || ace.Mask & ^windows.ACCESS_MASK(windows.FILE_GENERIC_READ|windows.FILE_GENERIC_EXECUTE) != 0 {
			return ErrLockUnsafe
		}
	}
	return nil
}

func releaseOwnedLock(file *os.File) error {
	var offset windows.Overlapped
	return errors.Join(windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &offset), file.Close())
}

func errorsJoinHold(err error, hold *LifecycleHold) error { return errors.Join(err, hold.Close()) }
func errorsJoinWriter(err error, file *os.File, hold *LifecycleHold) error {
	return errors.Join(err, releaseOwnedLock(file), hold.Close())
}
