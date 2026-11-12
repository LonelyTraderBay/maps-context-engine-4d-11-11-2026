//go:build linux

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

const runtimeConfigLimit = 1 << 20

// SetRuntimeConfig performs a bounded, cooperative file-lock CAS on an
// authorized owner-only state root. All writers must follow the same lock
// protocol. The caller authenticates the administrator and owns the root.
// An unexpected pending file is never deleted. A non-cooperating same-user
// writer cannot be excluded by flock; an uncertain result must be reconciled.
func SetRuntimeConfig(ctx context.Context, root *paths.Root, raw []byte, availableCPUs int) (result ConfigWriteResult, resultErr error) {
	return SetRuntimeConfigAt(ctx, root, runtimeConfigName, raw, availableCPUs)
}

// SetRuntimeConfigAt uses an explicitly authorized parent and file name. All
// config files under the same parent share the bounded lock and pending slot.
func SetRuntimeConfigAt(ctx context.Context, root *paths.Root, name string, raw []byte, availableCPUs int) (result ConfigWriteResult, resultErr error) {
	result.State = "not_applied"
	if ctx == nil || root == nil || !validRuntimeConfigName(name) {
		return result, ErrConfigUnsupported
	}
	request, err := model.DecodeRuntimeConfigRequestJSON(raw, runtimeConfigLimit, availableCPUs)
	if err != nil {
		return result, err
	}
	if request.Action != "set" || request.Config == nil || request.Effective == nil {
		return result, ErrConfigUnsupported
	}
	next, err := json.Marshal(request.Config)
	if err != nil || len(next) > runtimeConfigLimit {
		return result, errors.Join(ErrConfigLimit, err)
	}
	digest, err := model.RuntimeConfigDigest(*request.Effective, runtimeConfigLimit)
	if err != nil {
		return result, err
	}
	result.Config, result.Digest = *request.Effective, digest
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = bounded.Err(); err != nil {
		return result, err
	}
	dir, err := configDirectory(root)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	lockFD, err := unix.Openat(int(dir.Fd()), runtimeConfigLock, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return result, err
	}
	lock := os.NewFile(uintptr(lockFD), "runtime-config-lock")
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err = configOwnerOnly(lock, false); err != nil {
		return result, err
	}
	if err = acquireConfigLock(bounded, lock); err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Flock(int(lock.Fd()), unix.LOCK_UN)) }()
	if err = verifyConfigLock(root, lock); err != nil {
		return result, err
	}
	pending, err := configPendingAt(root, dir)
	if err != nil || pending {
		return result, errors.Join(ErrConfigPending, err)
	}
	current, present, err := readRuntimeConfigUnlocked(root, name, runtimeConfigLimit)
	if err != nil {
		return result, err
	}
	currentDigest, err := runtimeConfigDigest(current, present, availableCPUs)
	if err != nil {
		return result, err
	}
	if err = model.CheckExpectedConfigDigest(request, currentDigest); err != nil {
		return result, err
	}
	if err = bounded.Err(); err != nil {
		return result, err
	}
	fd, err := unix.Openat(int(dir.Fd()), runtimeConfigPendingName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if errors.Is(err, unix.EEXIST) {
		return result, ErrConfigPending
	}
	if err != nil {
		return result, err
	}
	pendingFile := os.NewFile(uintptr(fd), "runtime-config-pending")
	defer func() { resultErr = errors.Join(resultErr, pendingFile.Close()) }()
	pendingInfo, err := pendingFile.Stat()
	if err != nil {
		return result, errors.Join(ErrConfigPending, err)
	}
	ownedPending := true
	defer func() {
		if ownedPending {
			if cleanupErr := removeMatchingPending(root, dir, lock, pendingInfo); cleanupErr != nil {
				resultErr = errors.Join(resultErr, ErrConfigPending, cleanupErr)
			}
		}
	}()
	if err = configOwnerOnly(pendingFile, false); err != nil {
		return result, err
	}
	if n, writeErr := pendingFile.Write(next); writeErr != nil || n != len(next) {
		err = errors.Join(writeErr, io.ErrShortWrite)
		return result, err
	}
	if err = pendingFile.Sync(); err != nil {
		return result, err
	}
	if err = bounded.Err(); err != nil {
		return result, err
	}
	if err = verifyConfigLock(root, lock); err != nil {
		return result, err
	}
	observed, stillPresent, err := readRuntimeConfigUnlocked(root, name, runtimeConfigLimit)
	if err != nil {
		return result, err
	}
	if present != stillPresent || !bytes.Equal(current, observed) {
		return result, model.ErrConfigConflict
	}
	if err = configOwnerOnly(dir, true); err != nil {
		return result, err
	}
	if err = verifyConfigLock(root, lock); err != nil {
		return result, err
	}
	if !present {
		err = unix.Renameat2(int(dir.Fd()), runtimeConfigPendingName, int(dir.Fd()), name, unix.RENAME_NOREPLACE)
		if errors.Is(err, unix.EEXIST) {
			return result, model.ErrConfigConflict
		}
		if err != nil {
			return result, configRenameError(err)
		}
		ownedPending = false
		result.State = "unknown"
		if err = verifyInstalledConfig(root, name, pendingInfo, next); err != nil {
			return result, err
		}
		result.State = "applied"
		if err = syncConfigDirectory(root, dir); err != nil {
			return result, err
		}
		return result, nil
	}
	if err = unix.Renameat2(int(dir.Fd()), runtimeConfigPendingName, int(dir.Fd()), name, unix.RENAME_EXCHANGE); err != nil {
		return result, configRenameError(err)
	}
	ownedPending = false
	result.State = "unknown"
	oldFile, err := root.OpenRegular(runtimeConfigPendingName)
	if err != nil {
		return result, errors.Join(ErrConfigPending, err)
	}
	oldInfo, statErr := oldFile.Stat()
	oldData, readErr := io.ReadAll(io.LimitReader(oldFile, runtimeConfigLimit+1))
	oldOwnerErr := configOwnerOnly(oldFile, false)
	closeErr := oldFile.Close()
	if statErr != nil || readErr != nil || oldOwnerErr != nil || closeErr != nil || !bytes.Equal(current, oldData) {
		if err := verifyConfigLock(root, lock); err != nil {
			return result, errors.Join(ErrConfigPending, statErr, readErr, oldOwnerErr, closeErr, err)
		}
		if err := verifyInstalledConfig(root, name, pendingInfo, next); err != nil {
			return result, errors.Join(ErrConfigPending, statErr, readErr, oldOwnerErr, closeErr, err)
		}
		if err := unix.Renameat2(int(dir.Fd()), runtimeConfigPendingName, int(dir.Fd()), name, unix.RENAME_EXCHANGE); err != nil {
			return result, errors.Join(ErrConfigPending, statErr, readErr, oldOwnerErr, closeErr, err)
		}
		ownedPending = true
		result.State = "not_applied"
		return result, errors.Join(model.ErrConfigConflict, statErr, readErr, oldOwnerErr, closeErr)
	}
	if err = verifyInstalledConfig(root, name, pendingInfo, next); err != nil {
		return result, errors.Join(ErrConfigPending, err)
	}
	result.State = "applied"
	if err = removeMatchingPending(root, dir, lock, oldInfo); err != nil {
		return result, errors.Join(ErrConfigPending, err)
	}
	if err = syncConfigDirectory(root, dir); err != nil {
		return result, err
	}
	return result, nil
}

func runtimeConfigPending(root *paths.Root) (bool, error) {
	dir, err := configDirectory(root)
	if err != nil {
		return false, err
	}
	pending, checkErr := configPendingAt(root, dir)
	return pending, errors.Join(checkErr, dir.Close())
}

func configDirectory(root *paths.Root) (*os.File, error) {
	if err := root.Verify(); err != nil {
		return nil, err
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	if err = configOwnerOnly(dir, true); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	return dir, nil
}

func configOwnerOnly(file *os.File, directory bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	wantType := uint32(unix.S_IFREG)
	if directory {
		wantType = unix.S_IFDIR
	}
	if st.Mode&unix.S_IFMT != wantType || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || (!directory && st.Nlink != 1) {
		return ErrConfigUnsupported
	}
	return nil
}

func verifyRuntimeConfigFile(file *os.File) error { return configOwnerOnly(file, false) }

func configPendingAt(root *paths.Root, dir *os.File) (bool, error) {
	var st unix.Stat_t
	err := unix.Fstatat(int(dir.Fd()), runtimeConfigPendingName, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return false, err
	}
	if verifyErr := root.Verify(); verifyErr != nil {
		return false, verifyErr
	}
	return err == nil, nil
}

func acquireConfigLock(ctx context.Context, lock *os.File) error {
	for {
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

func verifyInstalledConfig(root *paths.Root, name string, expected os.FileInfo, expectedData []byte) error {
	installed, err := root.OpenRegular(name)
	if err != nil {
		return err
	}
	info, statErr := installed.Stat()
	data, readErr := io.ReadAll(io.LimitReader(installed, runtimeConfigLimit+1))
	ownerErr := configOwnerOnly(installed, false)
	closeErr := installed.Close()
	if statErr != nil || readErr != nil || ownerErr != nil || closeErr != nil {
		return errors.Join(statErr, readErr, ownerErr, closeErr)
	}
	if !os.SameFile(expected, info) || !bytes.Equal(data, expectedData) {
		return ErrConfigUnstable
	}
	return root.Verify()
}

func verifyConfigLock(root *paths.Root, lock *os.File) error {
	if err := configOwnerOnly(lock, false); err != nil {
		return err
	}
	pathLock, err := root.OpenRegular(runtimeConfigLock)
	if err != nil {
		return err
	}
	a, errA := lock.Stat()
	b, errB := pathLock.Stat()
	closeErr := pathLock.Close()
	if errA != nil || errB != nil || closeErr != nil {
		return errors.Join(errA, errB, closeErr)
	}
	if !os.SameFile(a, b) {
		return ErrConfigUnstable
	}
	return root.Verify()
}

func runtimeConfigDigest(raw []byte, present bool, cpus int) (*string, error) {
	if !present {
		return nil, nil
	}
	config, err := model.DecodeRuntimeConfigJSON(raw, runtimeConfigLimit, cpus)
	if err != nil {
		return nil, err
	}
	digest, err := model.RuntimeConfigDigest(config, runtimeConfigLimit)
	if err != nil {
		return nil, err
	}
	return &digest, nil
}

func removeMatchingPending(root *paths.Root, dir, lock *os.File, expected os.FileInfo) error {
	if err := verifyConfigLock(root, lock); err != nil {
		return err
	}
	file, err := root.OpenRegular(runtimeConfigPendingName)
	if err != nil {
		return err
	}
	actual, statErr := file.Stat()
	ownerErr := configOwnerOnly(file, false)
	closeErr := file.Close()
	if statErr != nil || ownerErr != nil || closeErr != nil {
		return errors.Join(statErr, ownerErr, closeErr)
	}
	if !os.SameFile(expected, actual) {
		return ErrConfigUnstable
	}
	return unix.Unlinkat(int(dir.Fd()), runtimeConfigPendingName, 0)
}

func syncConfigDirectory(root *paths.Root, dir *os.File) error {
	if err := root.Verify(); err != nil {
		return err
	}
	fd, err := unix.Openat(int(dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "runtime-config-directory")
	syncErr := file.Sync()
	return errors.Join(syncErr, file.Close(), root.Verify())
}

func configRenameError(err error) error {
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
		return errors.Join(ErrConfigUnsupported, err)
	}
	return err
}
