//go:build linux

package contextpack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

const (
	cursorKeyName    = "cursor.key"
	cursorKeyLock    = "cursor.key.lock"
	cursorKeyPending = "cursor.key.pending"
)

// LoadOrCreateCursorKey uses an already authorized owner-only catalog root.
// A cooperative process lock and no-replace rename avoid partially published
// keys. An interrupted pending file is preserved and blocks automatic retry.
// This operation does not authorize the root or back up the key.
func LoadOrCreateCursorKey(ctx context.Context, root *paths.Root) (key [32]byte, resultErr error) {
	defer func() {
		if resultErr != nil {
			key = [32]byte{}
		}
	}()
	if ctx == nil || root == nil {
		return key, ErrCursorKeyUnsafe
	}
	if _, ok := ctx.Deadline(); !ok {
		return key, ErrCursorKeyUnsafe
	}
	if err := ctx.Err(); err != nil {
		return key, err
	}
	dir, err := cursorKeyDirectory(root)
	if err != nil {
		return key, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	fd, err := unix.Openat(int(dir.Fd()), cursorKeyLock, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return key, errors.Join(ErrCursorKeyUnsafe, err)
	}
	lock := os.NewFile(uintptr(fd), "cursor-key-lock")
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err = cursorKeyOwner(lock, false); err != nil {
		return key, err
	}
	for {
		if err = ctx.Err(); err != nil {
			return key, err
		}
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return key, err
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
			return key, ctx.Err()
		case <-timer.C:
		}
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Flock(int(lock.Fd()), unix.LOCK_UN)) }()
	if err = verifyCursorKeyLock(root, lock); err != nil {
		return key, err
	}
	var pendingStat unix.Stat_t
	err = unix.Fstatat(int(dir.Fd()), cursorKeyPending, &pendingStat, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		return key, ErrCursorKeyPending
	}
	if !errors.Is(err, unix.ENOENT) {
		return key, errors.Join(ErrCursorKeyUnsafe, err)
	}
	key, present, err := readCursorKey(root)
	if err != nil {
		return [32]byte{}, err
	}
	if present {
		if err = verifyCursorKeyLock(root, lock); err != nil {
			return [32]byte{}, err
		}
		return key, ctx.Err()
	}
	key, err = GenerateCursorKey()
	if err != nil {
		return [32]byte{}, err
	}
	if err = ctx.Err(); err != nil {
		return [32]byte{}, err
	}
	fd, err = unix.Openat(int(dir.Fd()), cursorKeyPending, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	pending := os.NewFile(uintptr(fd), "cursor-key-pending")
	defer func() { resultErr = errors.Join(resultErr, pending.Close()) }()
	if err = cursorKeyOwner(pending, false); err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	info, err := pending.Stat()
	if err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	n, writeErr := pending.Write(key[:])
	if writeErr != nil || n != len(key) {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, writeErr, io.ErrShortWrite)
	}
	if err = pending.Sync(); err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	if err = ctx.Err(); err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	if err = verifyCursorKeyLock(root, lock); err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	observed, err := root.OpenRegular(cursorKeyPending)
	if err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	observedInfo, statErr := observed.Stat()
	ownerErr := cursorKeyOwner(observed, false)
	closeErr := observed.Close()
	if statErr != nil || ownerErr != nil || closeErr != nil || !os.SameFile(info, observedInfo) {
		return [32]byte{}, errors.Join(ErrCursorKeyPending, statErr, ownerErr, closeErr)
	}
	if err = unix.Renameat2(int(dir.Fd()), cursorKeyPending, int(dir.Fd()), cursorKeyName, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
			return [32]byte{}, errors.Join(ErrCursorKeyPending, ErrCursorKeyUnsupported, err)
		}
		return [32]byte{}, errors.Join(ErrCursorKeyPending, err)
	}
	actual, present, err := readCursorKey(root)
	if err != nil || !present || actual != key {
		return [32]byte{}, errors.Join(ErrCursorKeyUncertain, err)
	}
	installed, err := root.OpenRegular(cursorKeyName)
	if err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyUncertain, err)
	}
	installedInfo, statErr := installed.Stat()
	closeErr = installed.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(info, installedInfo) {
		return [32]byte{}, errors.Join(ErrCursorKeyUncertain, statErr, closeErr)
	}
	syncFD, err := unix.Openat(int(dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyUncertain, err)
	}
	syncDir := os.NewFile(uintptr(syncFD), "cursor-key-directory")
	err = errors.Join(syncDir.Sync(), syncDir.Close(), verifyCursorKeyLock(root, lock), ctx.Err())
	if err != nil {
		return [32]byte{}, errors.Join(ErrCursorKeyUncertain, err)
	}
	return key, nil
}

func cursorKeyOwner(file *os.File, directory bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return errors.Join(ErrCursorKeyUnsafe, err)
	}
	want := uint32(unix.S_IFREG)
	if directory {
		want = unix.S_IFDIR
	}
	if stat.Mode&unix.S_IFMT != want || stat.Uid != uint32(unix.Geteuid()) || stat.Mode&0077 != 0 || !directory && stat.Nlink != 1 {
		return fmt.Errorf("%w: directory=%t mode=%#o uid=%d expected_uid=%d links=%d", ErrCursorKeyUnsafe, directory, stat.Mode, stat.Uid, unix.Geteuid(), stat.Nlink)
	}
	return nil
}

func cursorKeyDirectory(root *paths.Root) (*os.File, error) {
	if err := root.Verify(); err != nil {
		return nil, errors.Join(ErrCursorKeyUnsafe, err)
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return nil, errors.Join(ErrCursorKeyUnsafe, err)
	}
	if err = cursorKeyOwner(dir, true); err != nil {
		return nil, errors.Join(err, dir.Close())
	}
	return dir, nil
}

func verifyCursorKeyLock(root *paths.Root, lock *os.File) error {
	if err := cursorKeyOwner(lock, false); err != nil {
		return err
	}
	current, err := root.OpenRegular(cursorKeyLock)
	if err != nil {
		return errors.Join(ErrCursorKeyUnsafe, err)
	}
	a, errA := lock.Stat()
	b, errB := current.Stat()
	err = errors.Join(errA, errB, cursorKeyOwner(current, false), current.Close(), root.Verify())
	if err != nil || !os.SameFile(a, b) {
		return errors.Join(ErrCursorKeyUnsafe, err)
	}
	return nil
}

func readCursorKey(root *paths.Root) (key [32]byte, present bool, resultErr error) {
	defer func() {
		if resultErr != nil {
			key, present = [32]byte{}, false
		}
	}()
	file, err := root.OpenRegular(cursorKeyName)
	if errors.Is(err, os.ErrNotExist) {
		return key, false, root.Verify()
	}
	if err != nil {
		return key, false, errors.Join(ErrCursorKeyUnsafe, err)
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err = cursorKeyOwner(file, false); err != nil {
		return key, false, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() != int64(len(key)) {
		return key, false, errors.Join(ErrCursorKeyUnsafe, err)
	}
	first, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil || len(first) != len(key) {
		return key, false, errors.Join(ErrCursorKeyUnsafe, err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return key, false, errors.Join(ErrCursorKeyUnsafe, err)
	}
	second, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil || !bytes.Equal(first, second) {
		return key, false, errors.Join(ErrCursorKeyUnsafe, err)
	}
	current, err := root.OpenRegular(cursorKeyName)
	if err != nil {
		return key, false, errors.Join(ErrCursorKeyUnsafe, err)
	}
	other, statErr := current.Stat()
	err = errors.Join(statErr, cursorKeyOwner(current, false), current.Close(), root.Verify())
	if err != nil || !os.SameFile(info, other) {
		return key, false, errors.Join(ErrCursorKeyUnsafe, err)
	}
	copy(key[:], first)
	if key == ([32]byte{}) {
		return [32]byte{}, false, ErrCursorKeyUnsafe
	}
	return key, true, nil
}
