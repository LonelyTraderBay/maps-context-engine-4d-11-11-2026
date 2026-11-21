//go:build linux || windows

package lsp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
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
	lock, err := openResultLock(anchor)
	if err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err = resultOwner(lock, false); err != nil {
		return header, err
	}
	if err = acquireResultLock(ctx, lock); err != nil {
		return header, err
	}
	defer func() { resultErr = errors.Join(resultErr, releaseResultLock(lock)) }()
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
	pending, err := openPendingResult(anchor, name)
	if err != nil {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	defer func() {
		if pending != nil {
			resultErr = errors.Join(resultErr, pending.Close())
		}
	}()
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
	observedInfo, observedRaw, err := inspectPendingResult(dir, pending, name, s.maxResult)
	if err != nil || !os.SameFile(info, observedInfo) || !bytes.Equal(raw, observedRaw) {
		return header, errors.Join(ErrResultStoreUnsafe, err)
	}
	if err := publishPendingResult(anchor, pending, name, result.Header.ResultID+".json"); err != nil {
		return header, err
	}
	closeErr := pending.Close()
	pending = nil
	if err := errors.Join(closeErr, s.check(ctx, dir, lock)); err != nil {
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
	listing, err := openResultListing(dir, anchor)
	if err != nil {
		return 0, 0, errors.Join(ErrResultStoreUnsafe, err)
	}
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
			size, err := inspectResultSize(dir, anchor, name)
			if err != nil || size < 0 || size > resultHardQuota-used {
				return 0, 0, errors.Join(ErrResultStoreUnsafe, err)
			}
			used += size
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
