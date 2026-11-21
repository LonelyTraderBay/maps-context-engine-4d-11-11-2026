//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

const linuxAppendFlag = 0x20 // FS_APPEND_FL, Linux UAPI

// walHeadAuthority is a separate, administrator-provisioned append-only file.
// Its name and all ancestor directories are root-managed; the unprivileged
// engine can append but cannot truncate, overwrite, unlink, replace or clear
// FS_APPEND_FL. This supplies a latest head outside the mutable WAL journal.
//
// The trusted owner supplies identity/key and the complete protected set,
// including the mutable journal root. No provisioning, chmod/chown, sysctl or
// flag changes occur here. Root/disk snapshot rollback is outside this trust
// boundary. A torn entry blocks serving until explicit operator recovery;
// there is deliberately no fallback to the previous valid entry.
type walHeadAuthority struct {
	mu        sync.Mutex
	root      *paths.Root
	protected []*paths.Root
	file      *os.File
	identity  walAuthorityIdentity
	key       [32]byte
	size      int64
	chain     [32]byte
	position  walJournalPosition
	poisoned  bool
	closed    bool
}

// On success ownership of file transfers; root remains caller-owned.
func openWALHeadAuthority(ctx context.Context, root *paths.Root, protected []*paths.Root, file *os.File, identity walAuthorityIdentity, key [32]byte) (_ *walHeadAuthority, resultErr error) {
	if file == nil || identity.Authority == [16]byte{} || identity.Store == [32]byte{} || identity.Incarnation == [16]byte{} || key == [32]byte{} {
		return nil, ErrStateUnsafe
	}
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDWR || flags&unix.O_APPEND == 0 {
		return nil, ErrStateUnsafe
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(errWALAuthorityBusy, err)
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, unix.Flock(int(file.Fd()), unix.LOCK_UN))
		}
	}()
	h := &walHeadAuthority{root: root, protected: append([]*paths.Root(nil), protected...), file: file, identity: identity, key: key}
	info, err := file.Stat()
	if err != nil || info.Size() < 0 || info.Size() > walHeadMaxBytes || info.Size()%walHeadRecordSize != 0 {
		return nil, errWALAuthorityRecovery
	}
	h.size = info.Size()
	if err := h.checkBase(ctx, h.size); err != nil {
		return nil, err
	}
	for offset := int64(0); offset < h.size; offset += walHeadRecordSize {
		var raw [walHeadRecordSize]byte
		if err := readLogicalAt(ctx, file, raw[:], offset); err != nil {
			return nil, errors.Join(errWALAuthorityRecovery, err)
		}
		next := walJournalPosition{Size: h.position.Size + walAuthorityRecordSize}
		copy(next.SHA256[:], raw[88:120])
		if next.SHA256 == [32]byte{} || !bytes.Equal(h.encode(next)[:], raw[:]) {
			return nil, errWALAuthorityRecovery
		}
		h.position = next
		h.chain = sha256.Sum256(raw[:])
	}
	if err := file.Sync(); err != nil {
		return nil, errors.Join(errWALAuthorityRecovery, err)
	}
	if err := syncWALDirectory(root); err != nil {
		return nil, errors.Join(errWALAuthorityRecovery, err)
	}
	if err := h.checkBase(ctx, h.size); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *walHeadAuthority) checkBase(ctx context.Context, expectedSize int64) error {
	if err := unprivilegedStateProcess(ctx); err != nil {
		return err
	}
	if h == nil || h.closed || h.poisoned || h.file == nil || stateOwner(h.file, false) != nil ||
		rootManagedAncestors(ctx, h.root, true) != nil || runtime.CheckCleanupRoot(h.root, h.protected) != nil {
		return ErrStateUnsafe
	}
	flags, err := unix.IoctlGetInt(int(h.file.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || flags&linuxAppendFlag == 0 {
		return ErrStateUnsafe
	}
	dir, err := h.root.DirectoryHandle()
	if err != nil {
		return ErrStateUnsafe
	}
	var actual, pinned unix.Stat_t
	err = errors.Join(unix.Fstat(int(h.file.Fd()), &pinned), unix.Fstatat(int(dir.Fd()), walHeadName, &actual, unix.AT_SYMLINK_NOFOLLOW), dir.Close())
	if err != nil || actual.Mode&unix.S_IFMT != unix.S_IFREG || actual.Dev != pinned.Dev || actual.Ino != pinned.Ino ||
		pinned.Size != expectedSize || expectedSize < 0 || expectedSize > walHeadMaxBytes || actual.Nlink != 1 {
		return ErrStateChanged
	}
	return ctx.Err()
}

func (h *walHeadAuthority) check(ctx context.Context, expected walJournalPosition) error {
	if h == nil {
		return ErrStateUnsafe
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.checkLocked(ctx, expected)
}

func (h *walHeadAuthority) checkLocked(ctx context.Context, expected walJournalPosition) error {
	if err := h.checkBase(ctx, h.size); err != nil {
		return err
	}
	if expected != h.position {
		return ErrStateChanged
	}
	if h.size != 0 {
		var last [walHeadRecordSize]byte
		if err := readLogicalAt(ctx, h.file, last[:], h.size-walHeadRecordSize); err != nil {
			return err
		}
		if sha256.Sum256(last[:]) != h.chain {
			return errWALAuthorityRecovery
		}
	}
	return ctx.Err()
}

func (h *walHeadAuthority) reserve(ctx context.Context, expected walJournalPosition, records int64) error {
	if h == nil || records < 1 || records > 2 {
		return ErrStateUnsafe
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.checkLocked(ctx, expected); err != nil {
		return err
	}
	if h.size > walHeadMaxBytes-records*walHeadRecordSize {
		return ErrStateLimit
	}
	return nil
}

func (h *walHeadAuthority) advance(ctx context.Context, expected, next walJournalPosition) error {
	if h == nil {
		return ErrStateUnsafe
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.checkLocked(ctx, expected); err != nil {
		return err
	}
	if h.size > walHeadMaxBytes-walHeadRecordSize || next.Size != expected.Size+walAuthorityRecordSize || next.Size > walAuthorityMaxBytes || next.SHA256 == [32]byte{} {
		return ErrStateLimit
	}
	raw := h.encode(next)
	n, err := h.file.Write(raw)
	if n != len(raw) {
		err = errors.Join(err, io.ErrShortWrite)
	}
	if err == nil {
		err = h.file.Sync()
	}
	if err == nil {
		err = h.checkBase(ctx, h.size+walHeadRecordSize)
	}
	if err != nil {
		h.poisoned = true
		return errors.Join(errWALAuthorityRecovery, err)
	}
	h.size += walHeadRecordSize
	h.position, h.chain = next, sha256.Sum256(raw)
	return nil
}

func (h *walHeadAuthority) encode(next walJournalPosition) []byte {
	return encodeWALHeadRecord(h.identity, h.key, h.chain, next)
}

func (h *walHeadAuthority) Close() error {
	if h == nil {
		return ErrStateUnsafe
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrOwnerClosed
	}
	h.closed = true
	clear(h.key[:])
	return h.file.Close()
}
