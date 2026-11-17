//go:build linux

package packstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

const quotaLock = "pack-quota.lock"

func fileOwner(file *os.File, directory bool) error {
	var stat unix.Stat_t
	if file == nil || unix.Fstat(int(file.Fd()), &stat) != nil {
		return ErrUnsafe
	}
	kind := uint32(unix.S_IFREG)
	if directory {
		kind = unix.S_IFDIR
	}
	if stat.Mode&unix.S_IFMT != kind || stat.Uid != uint32(unix.Geteuid()) || stat.Mode&0077 != 0 || !directory && stat.Nlink != 1 {
		return ErrUnsafe
	}
	return nil
}

func rootOwner(root *paths.Root) error {
	if root == nil || root.Verify() != nil {
		return ErrUnsafe
	}
	file, err := root.DirectoryHandle()
	if err != nil {
		return ErrUnsafe
	}
	if err = errors.Join(fileOwner(file, true), file.Close(), root.Verify()); err != nil {
		return ErrUnsafe
	}
	return nil
}

func (s *Store) checkBase(ctx context.Context) error {
	if s == nil || ctx == nil || s.lease == nil || s.root == nil {
		return ErrInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalid
	}
	if err := checked(ctx, s.authorize); err != nil {
		return err
	}
	if s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease) != nil || rootOwner(s.root) != nil || runtime.CheckCleanupRoot(s.root, s.protected) != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnsafe
	}
	return checked(ctx, s.authorize)
}

func (s *Store) open(ctx context.Context) (*paths.Root, *paths.Root, *runtime.LifecycleHold, error) {
	if err := s.checkBase(ctx); err != nil {
		return nil, nil, nil, err
	}
	hold, err := s.lease.Hold(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		return nil, nil, nil, ErrUnsafe
	}
	scratch, err := s.root.OpenDirectory("scratch")
	if err != nil {
		_ = hold.Close()
		return nil, nil, nil, ErrUnsafe
	}
	if err := rootOwner(scratch); err != nil {
		_ = scratch.Close()
		_ = hold.Close()
		return nil, nil, nil, err
	}
	dir, err := scratch.OpenDirectory("pack")
	if err != nil {
		if dir != nil {
			_ = dir.Close()
		}
		_ = scratch.Close()
		_ = hold.Close()
		return nil, nil, nil, ErrUnsafe
	}
	if rootOwner(dir) != nil {
		_ = dir.Close()
		_ = scratch.Close()
		_ = hold.Close()
		return nil, nil, nil, ErrUnsafe
	}
	if err = s.checkBase(ctx); err != nil {
		_ = dir.Close()
		_ = scratch.Close()
		_ = hold.Close()
		return nil, nil, nil, err
	}
	return dir, scratch, hold, nil
}

func openQuotaLock(ctx context.Context, dir *paths.Root, create bool) (*os.File, error) {
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return nil, ErrUnsafe
	}
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if create {
		flags |= unix.O_CREAT
	}
	fd, openErr := unix.Openat(int(anchor.Fd()), quotaLock, flags, 0600)
	closeErr := anchor.Close()
	if openErr != nil || closeErr != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return nil, ErrUnsafe
	}
	lock := os.NewFile(uintptr(fd), "pack-quota-lock")
	if fileOwner(lock, false) != nil {
		_ = lock.Close()
		return nil, ErrUnsafe
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = lock.Close()
			return nil, err
		}
		err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = lock.Close()
			return nil, ErrUnsafe
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
			_ = lock.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Store) check(ctx context.Context, scratch, dir *paths.Root, lock *os.File) error {
	if err := s.checkBase(ctx); err != nil {
		return err
	}
	if rootOwner(scratch) != nil || rootOwner(dir) != nil || fileOwner(lock, false) != nil {
		return ErrUnsafe
	}
	current, err := dir.OpenRegular(quotaLock)
	if err != nil {
		return ErrUnsafe
	}
	a, errA := lock.Stat()
	b, errB := current.Stat()
	err = errors.Join(errA, errB, fileOwner(current, false), current.Close(), dir.Verify())
	if err != nil || !os.SameFile(a, b) {
		return ErrUnsafe
	}
	return checked(ctx, s.authorize)
}

func directoryUsage(ctx context.Context, dir *paths.Root, quota int64, maxCount int) (total int64, totalCount int, resultErr error) {
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return 0, 0, ErrUnsafe
	}
	fd, openErr := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	closeErr := anchor.Close()
	if openErr != nil || closeErr != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return 0, 0, ErrUnsafe
	}
	listing := os.NewFile(uintptr(fd), "pack-directory-list")
	defer func() {
		if listing.Close() != nil {
			total, totalCount, resultErr = 0, 0, ErrUnsafe
		}
	}()
	var used int64
	count, seen := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		entries, readErr := listing.ReadDir(64)
		for _, entry := range entries {
			seen++
			if seen > maxCount+1 {
				return 0, 0, ErrCapacity
			}
			name := entry.Name()
			if name != quotaLock {
				suffix := ""
				if strings.HasSuffix(name, ".pack") {
					suffix = ".pack"
				} else if strings.HasSuffix(name, ".pending") {
					suffix = ".pending"
				}
				if suffix == "" || !validID(strings.TrimSuffix(name, suffix)) {
					return 0, 0, ErrUnsafe
				}
				count++
			}
			var stat unix.Stat_t
			if unix.Fstatat(int(listing.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uint32(unix.Geteuid()) || stat.Mode&0077 != 0 || stat.Nlink != 1 || stat.Size < 0 {
				return 0, 0, ErrUnsafe
			}
			if stat.Size > quota-used {
				return 0, 0, ErrCapacity
			}
			used += stat.Size
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return 0, 0, ErrUnsafe
		}
	}
	if dir.Verify() != nil {
		return 0, 0, ErrUnsafe
	}
	return used, count, nil
}

func readOwned(dir *paths.Root, id string, max int64) (result []byte, resultErr error) {
	file, err := dir.OpenRegular(id + ".pack")
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil || fileOwner(file, false) != nil {
		if file != nil {
			_ = file.Close()
		}
		return nil, ErrUnsafe
	}
	defer func() {
		if file.Close() != nil {
			result, resultErr = nil, ErrUnsafe
		}
	}()
	info, err := file.Stat()
	if err != nil || info.Size() < 1 || info.Size() > max {
		return nil, ErrUnsafe
	}
	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, ErrUnsafe
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrUnsafe
	}
	again, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || !bytes.Equal(raw, again) {
		return nil, ErrUnsafe
	}
	current, err := dir.OpenRegular(id + ".pack")
	if err != nil {
		return nil, ErrUnsafe
	}
	other, statErr := current.Stat()
	err = errors.Join(statErr, fileOwner(current, false), current.Close(), dir.Verify())
	if err != nil || !os.SameFile(info, other) {
		return nil, ErrUnsafe
	}
	return raw, nil
}

func verify(ctx context.Context, verifier VerifyBytes, raw []byte) error {
	if verifier == nil {
		return ErrInvalid
	}
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := verifier(ctx, bytes.Clone(raw))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, ErrMissing) || errors.Is(err, contextpack.ErrPackExpired) || errors.Is(err, contextpack.ErrCursorExpired) {
		return ErrMissing
	}
	if err != nil {
		return ErrUnsafe
	}
	return nil
}

// Create publishes a single verified, immutable result. A write error after
// rename reports ErrUncertain; callers must not emit a token on any error.
// Pending files are retained and charged to quota until authorized GC.
func (s *Store) Create(ctx context.Context, id string, raw []byte, verifier VerifyBytes) (resultErr error) {
	if s == nil || ctx == nil || !validID(id) || len(raw) < 1 || int64(len(raw)) > s.maxResult || verifier == nil {
		return ErrInvalid
	}
	raw = bytes.Clone(raw)
	published := false
	defer func() {
		if published && resultErr != nil {
			resultErr = errors.Join(resultErr, ErrUncertain)
		}
	}()
	if err := verify(ctx, verifier, raw); err != nil {
		return err
	}
	dir, scratch, hold, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close(), scratch.Close(), hold.Close()) }()
	lock, err := openQuotaLock(ctx, dir, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Flock(int(lock.Fd()), unix.LOCK_UN), lock.Close()) }()
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return err
	}
	used, count, err := directoryUsage(ctx, dir, s.quota, s.maxCount)
	if err != nil {
		return err
	}
	if count >= s.maxCount || int64(len(raw)) > s.quota-used {
		return ErrCapacity
	}
	existing, openErr := dir.OpenRegular(id + ".pack")
	if openErr == nil {
		_ = existing.Close()
		return ErrUnsafe
	}
	if !errors.Is(openErr, os.ErrNotExist) {
		return ErrUnsafe
	}
	reservation, err := s.reserve(ctx, int64(len(raw)))
	if err != nil || reservation == nil {
		if reservation != nil {
			_ = reservation.Close()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return ErrCapacity
	}
	defer func() {
		if reservation.Close() != nil {
			resultErr = errors.Join(resultErr, ErrUnsafe)
		}
	}()
	if err = checked(ctx, reservation.Check); err != nil {
		return err
	}
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return ErrUnsafe
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	fd, err := unix.Openat(int(anchor.Fd()), id+".pending", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return ErrUnsafe
	}
	pending := os.NewFile(uintptr(fd), "pack-pending")
	defer func() { resultErr = errors.Join(resultErr, pending.Close()) }()
	if fileOwner(pending, false) != nil {
		return ErrUnsafe
	}
	info, err := pending.Stat()
	if err != nil {
		return ErrUnsafe
	}
	if n, writeErr := pending.Write(raw); writeErr != nil || n != len(raw) {
		return ErrUnsafe
	}
	if pending.Sync() != nil {
		return ErrUnsafe
	}
	if err = verify(ctx, verifier, raw); err != nil {
		return err
	}
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return err
	}
	if err = checked(ctx, reservation.Check); err != nil {
		return err
	}
	observed, err := dir.OpenRegular(id + ".pending")
	if err != nil {
		return ErrUnsafe
	}
	observedInfo, statErr := observed.Stat()
	observedRaw, readErr := io.ReadAll(io.LimitReader(observed, s.maxResult+1))
	err = errors.Join(statErr, readErr, fileOwner(observed, false), observed.Close())
	if err != nil || !os.SameFile(info, observedInfo) || !bytes.Equal(raw, observedRaw) {
		return ErrUnsafe
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = unix.Renameat2(int(anchor.Fd()), id+".pending", int(anchor.Fd()), id+".pack", unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
			return ErrUnsupported
		}
		return ErrUnsafe
	}
	published = true
	syncFD, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return ErrUnsafe
	}
	syncDir := os.NewFile(uintptr(syncFD), "pack-directory-sync")
	if errors.Join(syncDir.Sync(), syncDir.Close()) != nil {
		return ErrUnsafe
	}
	installed, err := readOwned(dir, id, s.maxResult)
	if err != nil || !bytes.Equal(installed, raw) {
		return ErrUnsafe
	}
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return err
	}
	if err = checked(ctx, reservation.Check); err != nil {
		return err
	}
	if err = verify(ctx, verifier, installed); err != nil {
		return err
	}
	installedFile, err := dir.OpenRegular(id + ".pack")
	if err != nil {
		return ErrUnsafe
	}
	installedInfo, statErr := installedFile.Stat()
	err = errors.Join(statErr, fileOwner(installedFile, false), installedFile.Close())
	if err != nil || !os.SameFile(info, installedInfo) {
		return ErrUnsafe
	}
	return s.check(ctx, scratch, dir, lock)
}

// Read holds the quota lock while checking, reading twice and validating the
// exact bytes. The caller must recheck authority before emitting any output.
func (s *Store) Read(ctx context.Context, id string, verifier VerifyBytes) (result []byte, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	if s == nil || ctx == nil || !validID(id) || verifier == nil {
		return nil, ErrInvalid
	}
	dir, scratch, hold, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close(), scratch.Close(), hold.Close()) }()
	lock, err := openQuotaLock(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Flock(int(lock.Fd()), unix.LOCK_UN), lock.Close()) }()
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return nil, err
	}
	raw, err := readOwned(dir, id, s.maxResult)
	if err != nil {
		return nil, err
	}
	if err = verify(ctx, verifier, raw); err != nil {
		return nil, err
	}
	again, err := readOwned(dir, id, s.maxResult)
	if err != nil || !bytes.Equal(raw, again) {
		return nil, ErrUnsafe
	}
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return nil, err
	}
	if err = verify(ctx, verifier, raw); err != nil {
		return nil, err
	}
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return nil, err
	}
	return raw, nil
}

func packIDs(ctx context.Context, dir *paths.Root, maxCount int) (ids []string, resultErr error) {
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return nil, ErrUnsafe
	}
	fd, openErr := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	closeErr := anchor.Close()
	if openErr != nil || closeErr != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return nil, ErrUnsafe
	}
	listing := os.NewFile(uintptr(fd), "pack-gc-list")
	defer func() {
		if listing.Close() != nil {
			ids, resultErr = nil, ErrUnsafe
		}
	}()
	seen := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, err := listing.ReadDir(64)
		for _, entry := range entries {
			seen++
			if seen > maxCount+1 {
				return nil, ErrCapacity
			}
			name := entry.Name()
			if name == quotaLock {
				continue
			}
			if strings.HasSuffix(name, ".pending") && validID(strings.TrimSuffix(name, ".pending")) {
				// An interrupted write has no complete authenticated header.
				// Leave it untouched until an owner can prove its provenance.
				continue
			}
			if !strings.HasSuffix(name, ".pack") || !validID(strings.TrimSuffix(name, ".pack")) {
				return nil, ErrUnsafe
			}
			ids = append(ids, strings.TrimSuffix(name, ".pack"))
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrUnsafe
		}
	}
	if dir.Verify() != nil {
		return nil, ErrUnsafe
	}
	sort.Strings(ids)
	return ids, nil
}

func checkExpiry(ctx context.Context, proof ExpiryProof, id string, raw []byte) (bool, error) {
	if ctx == nil || proof == nil {
		return false, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	expired, err := proof(ctx, id, bytes.Clone(raw))
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return false, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false, context.DeadlineExceeded
	}
	if err != nil {
		return false, ErrUnsafe
	}
	return expired, nil
}

// GCExpired removes only complete artifacts individually proven owned and
// expired by a trusted callback. The caller must authorize GC and keep budget
// accounting locked until the resulting disk usage has been reconciled. The
// writer must independently prove it is bound to this exact state root.
// Unknown and incomplete files are never removed by this method.
func (s *Store) GCExpired(ctx context.Context, writer GCWriter, job string, proof ExpiryProof) (removed int, resultErr error) {
	if s == nil || ctx == nil || writer == nil || !validID(job) || proof == nil {
		return 0, ErrInvalid
	}
	defer func() {
		if removed > 0 && resultErr != nil {
			resultErr = errors.Join(resultErr, ErrUncertain)
		}
	}()
	if err := s.checkGCWriterRoot(ctx, writer, job); err != nil {
		return 0, err
	}
	writerHold, err := writer.HoldWriter(ctx, s.worktree, job)
	if err != nil || writerHold == nil {
		if writerHold != nil {
			_ = writerHold.Close()
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return 0, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, context.DeadlineExceeded
		}
		return 0, ErrUnsafe
	}
	defer func() { resultErr = errors.Join(resultErr, writerHold.Close()) }()
	if err := s.checkGCWriterRoot(ctx, writer, job); err != nil {
		return 0, err
	}
	dir, scratch, hold, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close(), scratch.Close(), hold.Close()) }()
	lock, err := openQuotaLock(ctx, dir, false)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Flock(int(lock.Fd()), unix.LOCK_UN), lock.Close()) }()
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return 0, err
	}
	// Existing files may exceed newly reduced limits. GC uses the immutable
	// hard ceilings so that authenticated expired results can be reclaimed.
	if _, _, err = directoryUsage(ctx, dir, 32*1024*1024, 32); err != nil {
		return 0, err
	}
	reservation, err := s.reserve(ctx, 0)
	if err != nil || reservation == nil {
		if reservation != nil {
			_ = reservation.Close()
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return 0, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, context.DeadlineExceeded
		}
		return 0, ErrCapacity
	}
	defer func() {
		if reservation.Close() != nil {
			resultErr = errors.Join(resultErr, ErrUnsafe)
		}
	}()
	ids, err := packIDs(ctx, dir, 32)
	if err != nil {
		return 0, err
	}
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return 0, ErrUnsafe
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	for _, id := range ids {
		if err = s.check(ctx, scratch, dir, lock); err != nil {
			return removed, err
		}
		if err = s.checkGCWriterRoot(ctx, writer, job); err != nil {
			return removed, err
		}
		if err = checked(ctx, reservation.Check); err != nil {
			return removed, err
		}
		raw, err := readOwned(dir, id, 32*1024*1024)
		if err != nil {
			return removed, ErrUnsafe
		}
		expired, err := checkExpiry(ctx, proof, id, raw)
		if err != nil {
			return removed, err
		}
		if !expired {
			continue
		}
		file, err := dir.OpenRegular(id + ".pack")
		if err != nil || fileOwner(file, false) != nil {
			if file != nil {
				_ = file.Close()
			}
			return removed, ErrUnsafe
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		again, readErr := readOwned(dir, id, 32*1024*1024)
		if statErr != nil || closeErr != nil || readErr != nil || !bytes.Equal(raw, again) {
			return removed, ErrUnsafe
		}
		expired, err = checkExpiry(ctx, proof, id, again)
		if err != nil {
			return removed, err
		}
		if !expired {
			return removed, ErrUnsafe
		}
		if err = s.check(ctx, scratch, dir, lock); err != nil {
			return removed, err
		}
		if err = s.checkGCWriterRoot(ctx, writer, job); err != nil {
			return removed, err
		}
		if err = checked(ctx, reservation.Check); err != nil {
			return removed, err
		}
		current, err := dir.OpenRegular(id + ".pack")
		if err != nil {
			return removed, ErrUnsafe
		}
		currentInfo, statErr := current.Stat()
		err = errors.Join(statErr, fileOwner(current, false), current.Close())
		if err != nil || !os.SameFile(info, currentInfo) {
			return removed, ErrUnsafe
		}
		if err = ctx.Err(); err != nil {
			return removed, err
		}
		if err = unix.Unlinkat(int(anchor.Fd()), id+".pack", 0); err != nil {
			return removed, ErrUnsafe
		}
		removed++
	}
	if removed > 0 {
		fd, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return removed, ErrUnsafe
		}
		syncDir := os.NewFile(uintptr(fd), "pack-gc-sync")
		if errors.Join(syncDir.Sync(), syncDir.Close()) != nil {
			return removed, ErrUnsafe
		}
	}
	if err = s.check(ctx, scratch, dir, lock); err != nil {
		return removed, err
	}
	if err = s.checkGCWriterRoot(ctx, writer, job); err != nil {
		return removed, err
	}
	if err = checked(ctx, reservation.Check); err != nil {
		return removed, err
	}
	return removed, nil
}

func (s *Store) checkGCWriterRoot(ctx context.Context, writer GCWriter, job string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bound, ok := writer.(interface {
		CheckWriterRoot(context.Context, *paths.Root, string, string) error
	})
	if !ok {
		return ErrUnsafe
	}
	return checked(ctx, func(ctx context.Context) error {
		if err := writer.CheckWriter(ctx, s.worktree, job); err != nil {
			return err
		}
		return bound.CheckWriterRoot(ctx, s.root, s.worktree, job)
	})
}
