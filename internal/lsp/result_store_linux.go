//go:build linux

package lsp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

const resultQuotaLock = "rename-quota.lock"

// Create only persists the complete caller-validated result. It returns no
// signed token; the caller may issue one only after this method succeeds.
func (s *ResultStore) Create(ctx context.Context, scope model.ScopeVector, principal string, payload RenameResultPayload, guard ResultGuard) (header RenameResultHeader, resultErr error) {
	defer func() {
		if resultErr != nil {
			header = RenameResultHeader{}
		}
	}()
	if s == nil || ctx == nil || guard == nil || s.lease == nil {
		return header, ErrResultInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return header, ErrResultInvalid
	}
	if err := s.checkAuthority(ctx); err != nil {
		return header, err
	}
	if err := s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease); err != nil {
		return header, err
	}
	result, raw, err := s.prepare(scope, principal, payload)
	if err != nil {
		return header, err
	}
	if err = resultGuard(ctx, guard, result); err != nil {
		return header, err
	}
	dir, hold, err := s.open(ctx)
	if err != nil {
		return header, err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	fd, err := unix.Openat(int(anchor.Fd()), resultQuotaLock, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	lock := os.NewFile(uintptr(fd), "rename-quota-lock")
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err = resultOwner(lock, false); err != nil {
		return header, err
	}
	if err = acquireResultLock(ctx, lock); err != nil {
		return header, err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Flock(int(lock.Fd()), unix.LOCK_UN)) }()
	if err = s.check(ctx, dir, lock); err != nil {
		return header, err
	}
	used, count, err := resultDirectoryUsage(ctx, dir, anchor)
	if err != nil {
		return header, err
	}
	if count >= s.maxCount || int64(len(raw)) > s.quota-used {
		return header, ErrResultQuota
	}
	// Keep the shared cache growth gate through the pending write, atomic
	// publication and readback. Pending files remain charged on any failure.
	checkBudget := func() error { return s.checkAuthority(ctx) }
	if s.reserve != nil {
		reservation, reserveErr := s.reserve(ctx, int64(len(raw)))
		if reserveErr != nil || reservation == nil {
			if reservation != nil {
				_ = reservation.Close()
			}
			return header, errors.Join(ErrResultQuota, reserveErr)
		}
		defer func() { resultErr = errors.Join(resultErr, reservation.Close()) }()
		checkBudget = func() error { return errors.Join(s.checkAuthority(ctx), reservation.Check(ctx)) }
	}
	if err = checkBudget(); err != nil {
		return header, err
	}
	name := result.Header.ResultID + ".pending"
	fd, err = unix.Openat(int(anchor.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	pending := os.NewFile(uintptr(fd), "rename-result-pending")
	defer func() { resultErr = errors.Join(resultErr, pending.Close()) }()
	if err = resultOwner(pending, false); err != nil {
		return header, err
	}
	info, err := pending.Stat()
	if err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	n, writeErr := pending.Write(raw)
	if writeErr != nil || n != len(raw) {
		return header, errors.Join(ErrResultStoreUnsafe, writeErr, io.ErrShortWrite)
	}
	if err = pending.Sync(); err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	if err = checkBudget(); err != nil {
		return header, err
	}
	if err = resultGuard(ctx, guard, result); err != nil {
		return header, err
	}
	if err = s.check(ctx, dir, lock); err != nil {
		return header, err
	}
	observed, err := dir.OpenRegular(name)
	if err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	observedInfo, statErr := observed.Stat()
	observedRaw, readErr := io.ReadAll(io.LimitReader(observed, s.maxResult+1))
	closeErr := observed.Close()
	if statErr != nil || readErr != nil || closeErr != nil || !os.SameFile(info, observedInfo) || !bytes.Equal(raw, observedRaw) {
		return header, errors.Join(ErrResultStoreUnsafe, statErr, readErr, closeErr)
	}
	if err = unix.Renameat2(int(anchor.Fd()), name, int(anchor.Fd()), result.Header.ResultID+".json", unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
			return header, errors.Join(ErrResultUnsupported, err)
		}
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	syncFD, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return header, errors.Join(ErrResultUncertain, err)
	}
	syncDir := os.NewFile(uintptr(syncFD), "rename-result-directory")
	err = errors.Join(syncDir.Sync(), syncDir.Close(), s.check(ctx, dir, lock))
	if err != nil {
		return header, errors.Join(ErrResultUncertain, err)
	}
	installedRaw, err := readResultFile(dir, result.Header.ResultID, s.maxResult)
	if err != nil || !bytes.Equal(installedRaw, raw) {
		return header, errors.Join(ErrResultUncertain, err)
	}
	installedFile, err := dir.OpenRegular(result.Header.ResultID + ".json")
	if err != nil {
		return header, errors.Join(ErrResultUncertain, err)
	}
	installedInfo, statErr := installedFile.Stat()
	closeErr = installedFile.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(info, installedInfo) {
		return header, errors.Join(ErrResultUncertain, statErr, closeErr)
	}
	installed, err := s.decode(installedRaw, result.Header.ResultID, principal, scope)
	if err != nil {
		return header, errors.Join(ErrResultUncertain, err)
	}
	if err = resultGuard(ctx, guard, installed); err != nil {
		return header, err
	}
	if err = s.check(ctx, dir, lock); err != nil {
		return header, err
	}
	if err = s.validateResult(installed, principal, scope); err != nil {
		return header, err
	}
	if err = checkBudget(); err != nil {
		return header, errors.Join(ErrResultUncertain, err)
	}
	return installed.Header, nil
}

// Read never recomputes a missing result or moves to a newer snapshot. A
// missing, evicted or expired spool is returned as ErrResultExpired.
func (s *ResultStore) Read(ctx context.Context, id, principal string, scope model.ScopeVector, guard ResultGuard) (result RenameStoredResult, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = RenameStoredResult{}
		}
	}()
	if s == nil || ctx == nil || !resultUUID(id) || !resultHash(principal) || guard == nil {
		return result, ErrResultInvalid
	}
	dir, hold, err := s.open(ctx)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	raw, err := readResultFile(dir, id, s.maxResult)
	if err != nil {
		return result, err
	}
	result, err = s.decode(raw, id, principal, scope)
	if err != nil {
		return RenameStoredResult{}, err
	}
	if err = resultGuard(ctx, guard, result); err != nil {
		return RenameStoredResult{}, err
	}
	if err = s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease); err != nil {
		return RenameStoredResult{}, err
	}
	again, err := readResultFile(dir, id, s.maxResult)
	if err != nil || !bytes.Equal(raw, again) {
		return RenameStoredResult{}, errors.Join(ErrResultStoreUnsafe, err)
	}
	if err = s.validateResult(result, principal, scope); err != nil {
		return RenameStoredResult{}, err
	}
	if err = resultGuard(ctx, guard, result); err != nil {
		return RenameStoredResult{}, err
	}
	if err = s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease); err != nil {
		return RenameStoredResult{}, err
	}
	if err = resultRootOwner(dir); err != nil {
		return RenameStoredResult{}, err
	}
	if err = s.validateResult(result, principal, scope); err != nil {
		return RenameStoredResult{}, err
	}
	if err = s.checkAuthority(ctx); err != nil {
		return RenameStoredResult{}, err
	}
	return result, nil
}

func (s *ResultStore) open(ctx context.Context) (*paths.Root, *runtime.LifecycleHold, error) {
	if s == nil || ctx == nil || s.root == nil || s.lease == nil {
		return nil, nil, ErrResultInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, nil, ErrResultInvalid
	}
	if err := s.checkAuthority(ctx); err != nil {
		return nil, nil, err
	}
	if err := s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease); err != nil {
		return nil, nil, err
	}
	hold, err := s.lease.Hold(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err = resultRootOwner(s.root); err != nil {
		return nil, nil, errors.Join(err, hold.Close())
	}
	scratch, err := s.root.OpenDirectory("scratch")
	if err != nil {
		return nil, nil, errors.Join(ErrResultStoreUnsafe, err, hold.Close())
	}
	if err = resultRootOwner(scratch); err != nil {
		return nil, nil, errors.Join(err, scratch.Close(), hold.Close())
	}
	dir, err := scratch.OpenDirectory("rename")
	closeErr := scratch.Close()
	if err != nil || closeErr != nil {
		return nil, nil, errors.Join(ErrResultStoreUnsafe, err, closeErr, hold.Close())
	}
	if err = resultRootOwner(dir); err != nil {
		return nil, nil, errors.Join(err, dir.Close(), hold.Close())
	}
	if err = s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease); err != nil {
		return nil, nil, errors.Join(err, dir.Close(), hold.Close())
	}
	return dir, hold, nil
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

func resultRootOwner(root *paths.Root) error {
	if err := root.Verify(); err != nil {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	return errors.Join(resultOwner(dir, true), dir.Close(), root.Verify())
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

func (s *ResultStore) check(ctx context.Context, dir *paths.Root, lock *os.File) error {
	if err := s.checkAuthority(ctx); err != nil {
		return err
	}
	if err := s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease); err != nil {
		return err
	}
	if err := resultRootOwner(dir); err != nil {
		return err
	}
	current, err := dir.OpenRegular(resultQuotaLock)
	if err != nil {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	a, errA := lock.Stat()
	b, errB := current.Stat()
	err = errors.Join(errA, errB, resultOwner(lock, false), resultOwner(current, false), current.Close(), dir.Verify(), s.root.Verify())
	if err != nil || !os.SameFile(a, b) {
		return errors.Join(ErrResultStoreUnsafe, err)
	}
	return ctx.Err()
}

func resultDirectoryUsage(ctx context.Context, dir *paths.Root, anchor *os.File) (used int64, results int, resultErr error) {
	fd, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return 0, 0, errors.Join(ErrResultStoreUnsafe, err)
	}
	listing := os.NewFile(uintptr(fd), "rename-result-list")
	defer func() { resultErr = errors.Join(resultErr, listing.Close()) }()
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		entries, readErr := listing.ReadDir(128)
		for _, entry := range entries {
			count++
			if count > 10000 {
				return 0, 0, ErrResultQuota
			}
			name := entry.Name()
			if name != resultQuotaLock {
				results++
				ext := ""
				if strings.HasSuffix(name, ".json") {
					ext = ".json"
				} else if strings.HasSuffix(name, ".pending") {
					ext = ".pending"
				}
				if ext == "" || !resultUUID(strings.TrimSuffix(name, ext)) {
					return 0, 0, ErrResultStoreUnsafe
				}
			}
			var st unix.Stat_t
			if err := unix.Fstatat(int(anchor.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return 0, 0, errors.Join(ErrResultStoreUnsafe, err)
			}
			if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || st.Nlink != 1 || st.Size < 0 || st.Size > resultHardQuota-used {
				return 0, 0, ErrResultStoreUnsafe
			}
			used += st.Size
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return 0, 0, errors.Join(ErrResultStoreUnsafe, readErr)
		}
	}
	if err = dir.Verify(); err != nil {
		return 0, 0, errors.Join(ErrResultStoreUnsafe, err)
	}
	return used, results, nil
}

func readResultFile(dir *paths.Root, id string, maxBytes int64) (result []byte, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	file, err := dir.OpenRegular(id + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrResultExpired
	}
	if err != nil {
		return nil, errors.Join(ErrResultStoreUnsafe, err)
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err = resultOwner(file, false); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() < 1 || info.Size() > maxBytes {
		return nil, errors.Join(ErrResultStoreUnsafe, err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, errors.Join(ErrResultStoreUnsafe, err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Join(ErrResultStoreUnsafe, err)
	}
	again, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || !bytes.Equal(raw, again) {
		return nil, errors.Join(ErrResultStoreUnsafe, err)
	}
	current, err := dir.OpenRegular(id + ".json")
	if err != nil {
		return nil, errors.Join(ErrResultStoreUnsafe, err)
	}
	other, statErr := current.Stat()
	err = errors.Join(statErr, resultOwner(current, false), current.Close(), dir.Verify())
	if err != nil || !os.SameFile(info, other) {
		return nil, errors.Join(ErrResultStoreUnsafe, err)
	}
	return raw, nil
}
