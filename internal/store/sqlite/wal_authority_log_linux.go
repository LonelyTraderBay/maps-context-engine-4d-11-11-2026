//go:build linux

package sqlite

import (
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

// walAuthorityLog persists receipt ordering on an owner-supplied file. It is
// NOT a secure state opener or an independent proof that this is the latest
// authority. The owner must prove stable, disjoint protected storage, key and
// identity provenance, and the SQL outcome before calling Commit/Abort. A
// whole valid journal rollback cannot be detected from that journal alone.
//
// The file must already exist, O_RDWR|O_APPEND, owner-only, and exclusively
// transferred on success. The caller retains the root for this log's lifetime.
// Path checks detect changes; they do not prevent same-user rename/link races.
// No methods repair/truncate/unlink files. After ambiguous I/O this handle is
// poisoned. A fresh open authenticates the entire log and syncs before use.
// File I/O/Sync are not interruptible by context while a syscall is blocked.
type walAuthorityLog struct {
	mu         sync.Mutex
	root       *paths.Root
	protected  []*paths.Root
	file       *os.File
	identity   walAuthorityIdentity
	key        [32]byte
	maxBytes   int64
	size       int64
	chain      [32]byte
	head       walAuthorityReceipt
	pending    *walAuthorityReceipt
	poisoned   bool
	closed     bool
	domain     *stateDomain
	headSource *walHeadAuthority
}

// openAnchoredWALAuthorityLog consumes file, including on failure. The owner
// keeps the domain roots and head alive until after the log closes. Unlike the
// experimental standalone opener, this path requires a separate kernel-backed
// latest-head authority and a protected state domain before any append.
func openAnchoredWALAuthorityLog(ctx context.Context, domain *stateDomain, file *os.File, head *walHeadAuthority, identity walAuthorityIdentity, key [32]byte, maxBytes int64, initialize *walLogicalImage) (_ *walAuthorityLog, resultErr error) {
	if file == nil {
		return nil, ErrStateUnsafe
	}
	defer func() {
		if resultErr != nil {
			_ = file.Close()
		}
	}()
	if domain == nil || domain.Check(ctx) != nil || head == nil || head.identity != identity || head.key != key {
		return nil, ErrStateUnsafe
	}
	if runtime.CheckCleanupRoot(head.root, append(append([]*paths.Root(nil), domain.protected...), domain.root)) != nil {
		return nil, ErrStateUnsafe
	}
	if err := head.check(ctx, head.position); err != nil {
		return nil, err
	}
	if initialize != nil && head.position != (walJournalPosition{}) {
		return nil, ErrStateUnsafe
	}
	l, err := openWALAuthorityLog(ctx, domain.root, domain.protected, file, identity, key, maxBytes, initialize)
	if err != nil {
		return nil, err
	}
	position := walJournalPosition{Size: l.size, SHA256: l.chain}
	if initialize != nil {
		err = head.advance(ctx, walJournalPosition{}, position)
	} else {
		err = head.check(ctx, position)
	}
	if err != nil {
		_ = l.Close()
		return nil, errors.Join(errWALAuthorityRecovery, err)
	}
	l.domain, l.headSource = domain, head
	return l, nil
}

// openWALAuthorityLog never initializes an empty file. initialize is a separate
// explicit operation for a new, owner-approved authority and initial image.
func openWALAuthorityLog(ctx context.Context, root *paths.Root, protected []*paths.Root, file *os.File, identity walAuthorityIdentity, key [32]byte, maxBytes int64, initialize *walLogicalImage) (_ *walAuthorityLog, resultErr error) {
	if ctx == nil || root == nil || file == nil || len(protected) == 0 || len(protected) > 64 ||
		identity.Authority == [16]byte{} || identity.Store == [32]byte{} || identity.Incarnation == [16]byte{} || key == [32]byte{} ||
		maxBytes < 3*walAuthorityRecordSize || maxBytes > walAuthorityMaxBytes || maxBytes%walAuthorityRecordSize != 0 {
		return nil, ErrStateUnsafe
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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
	l := &walAuthorityLog{root: root, protected: append([]*paths.Root(nil), protected...), file: file, identity: identity, key: key, maxBytes: maxBytes}
	if err := l.check(ctx, false); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() > maxBytes {
		return nil, ErrStateLimit
	}
	l.size = info.Size()
	if initialize != nil {
		if l.size != 0 || !validAuthorityImage(*initialize) {
			return nil, ErrStateUnsafe
		}
		if err := l.append(ctx, walAuthorityRecord{kind: walAuthorityBootstrap, walAuthorityReceipt: walAuthorityReceipt{Image: *initialize}}); err != nil {
			return nil, err
		}
	} else {
		if l.size == 0 || l.size%walAuthorityRecordSize != 0 {
			return nil, errWALAuthorityRecovery
		}
		for offset := int64(0); offset < l.size; offset += walAuthorityRecordSize {
			var raw [walAuthorityRecordSize]byte
			if err := readLogicalAt(ctx, file, raw[:], offset); err != nil {
				return nil, errors.Join(errWALAuthorityRecovery, err)
			}
			r, err := l.decode(raw[:])
			if err != nil || l.apply(r, offset == 0) != nil {
				return nil, errWALAuthorityRecovery
			}
			l.chain = sha256.Sum256(raw[:])
		}
		if err := l.sync(ctx); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *walAuthorityLog) check(ctx context.Context, exactSize bool) error {
	if l == nil || ctx == nil || l.closed || l.file == nil || l.poisoned {
		return errWALAuthorityRecovery
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if o := l.domain; o != nil && o.Check(ctx) != nil {
		return ErrStateUnsafe
	}
	if stateRootOwner(l.root) != nil || runtime.CheckCleanupRoot(l.root, l.protected) != nil || stateOwner(l.file, false) != nil {
		return ErrStateUnsafe
	}
	dir, err := l.root.DirectoryHandle()
	if err != nil {
		return ErrStateUnsafe
	}
	var actual, pinned unix.Stat_t
	err = errors.Join(unix.Fstat(int(l.file.Fd()), &pinned), unix.Fstatat(int(dir.Fd()), walAuthorityName, &actual, unix.AT_SYMLINK_NOFOLLOW), dir.Close())
	if err != nil || actual.Mode&unix.S_IFMT != unix.S_IFREG || actual.Dev != pinned.Dev || actual.Ino != pinned.Ino || actual.Nlink != 1 ||
		pinned.Size < 0 || pinned.Size > l.maxBytes || exactSize && pinned.Size != l.size {
		return ErrStateChanged
	}
	return ctx.Err()
}

func (l *walAuthorityLog) sync(ctx context.Context) error {
	if err := l.check(ctx, true); err != nil {
		return err
	}
	if err := l.file.Sync(); err != nil {
		return err
	}
	// Sync the already-open authority directory as well as the file. This is
	// needed for first publication; the owner still proves durable ancestors.
	if err := syncWALDirectory(l.root); err != nil {
		return err
	}
	return l.check(ctx, true)
}

func syncWALDirectory(root *paths.Root) error {
	dir, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	fd, openErr := unix.Openat(int(dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	err = errors.Join(openErr, dir.Close())
	if fd >= 0 && openErr == nil {
		err = errors.Join(err, unix.Fsync(fd), unix.Close(fd))
	}
	if err != nil {
		return err
	}
	return root.Verify()
}

func (l *walAuthorityLog) apply(r walAuthorityRecord, first bool) error {
	next := walReceiptState{head: l.head, pending: l.pending}
	if err := next.apply(r, first); err != nil {
		return err
	}
	l.head, l.pending = next.head, next.pending
	return nil
}

func (l *walAuthorityLog) append(ctx context.Context, r walAuthorityRecord) error {
	if err := l.check(ctx, true); err != nil {
		return err
	}
	if l.size > l.maxBytes-walAuthorityRecordSize {
		return ErrStateLimit
	}
	position := walJournalPosition{Size: l.size, SHA256: l.chain}
	if l.headSource != nil {
		if err := l.headSource.reserve(ctx, position, 1); err != nil {
			return err
		}
	}
	// Validate on a copy so failed writes never advance the in-memory head.
	next := walAuthorityLog{head: l.head, pending: l.pending}
	if err := next.apply(r, l.size == 0); err != nil {
		return err
	}
	raw := l.encode(r)
	n, err := l.file.Write(raw[:])
	if n != len(raw) || err != nil {
		l.poisoned = true
		if n != len(raw) {
			err = errors.Join(err, io.ErrShortWrite)
		}
		return errors.Join(errWALAuthorityRecovery, err)
	}
	l.size += int64(n)
	if err := l.sync(ctx); err != nil {
		l.poisoned = true
		return errors.Join(errWALAuthorityRecovery, err)
	}
	nextHash := sha256.Sum256(raw[:])
	if l.headSource != nil {
		if err := l.headSource.advance(ctx, position, walJournalPosition{Size: l.size, SHA256: nextHash}); err != nil {
			l.poisoned = true
			return errors.Join(errWALAuthorityRecovery, err)
		}
	}
	l.head, l.pending = next.head, next.pending
	l.chain = nextHash
	return nil
}

func (l *walAuthorityLog) Head(ctx context.Context) (walAuthorityReceipt, error) {
	if l == nil {
		return walAuthorityReceipt{}, errWALAuthorityRecovery
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.check(ctx, true); err != nil {
		return walAuthorityReceipt{}, err
	}
	if l.headSource != nil {
		if err := l.headSource.check(ctx, walJournalPosition{Size: l.size, SHA256: l.chain}); err != nil {
			return walAuthorityReceipt{}, err
		}
	}
	if l.pending != nil {
		return walAuthorityReceipt{}, errWALAuthorityPending
	}
	return l.head, nil
}

func (l *walAuthorityLog) Prepare(ctx context.Context, operation [32]byte) error {
	if l == nil {
		return errWALAuthorityRecovery
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// Reserve both records before permitting SQL to start. No quota failure
	// should strand a prepared transaction without room for its resolution.
	if l.size > l.maxBytes-2*walAuthorityRecordSize {
		return ErrStateLimit
	}
	if l.headSource != nil {
		if err := l.headSource.reserve(ctx, walJournalPosition{Size: l.size, SHA256: l.chain}, 2); err != nil {
			return err
		}
	}
	return l.append(ctx, walAuthorityRecord{kind: walAuthorityPrepare, walAuthorityReceipt: walAuthorityReceipt{
		Sequence: l.head.Sequence + 1, Operation: operation, Image: l.head.Image,
	}})
}

// Commit records an independently verified SQL commit. It does not execute SQL
// or infer success from arbitrary main/WAL files. Call before ACK/checkpoint.
func (l *walAuthorityLog) Commit(ctx context.Context, operation [32]byte, image walLogicalImage, prefix walCommitPrefixReceipt) error {
	if l == nil {
		return errWALAuthorityRecovery
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.append(ctx, walAuthorityRecord{kind: walAuthorityCommit, walAuthorityReceipt: walAuthorityReceipt{
		Sequence: l.head.Sequence + 1, Operation: operation, Image: image, Prefix: prefix,
	}})
}

// CommitQuiesced binds an independently verified checkpointed image after all
// native pools for this operation have closed. The caller proves the SQL
// outcome, full quiescence and absence of relevant WAL/journal. It is not a
// substitute for Commit while a native writer/reader pool remains active.
func (l *walAuthorityLog) CommitQuiesced(ctx context.Context, operation [32]byte, image walLogicalImage) error {
	if l == nil {
		return errWALAuthorityRecovery
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.append(ctx, walAuthorityRecord{kind: walAuthorityCommitQuiesced, walAuthorityReceipt: walAuthorityReceipt{
		Sequence: l.head.Sequence + 1, Operation: operation, Image: image,
	}})
}

// Abort requires owner proof of rollback to exactly the acknowledged image.
// A COMMIT timeout alone is never that proof. Pending state otherwise remains.
func (l *walAuthorityLog) Abort(ctx context.Context, operation [32]byte, unchanged walLogicalImage) error {
	if l == nil {
		return errWALAuthorityRecovery
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if unchanged != l.head.Image {
		return errWALAuthorityRecovery
	}
	r := l.head
	r.Sequence, r.Operation = l.head.Sequence+1, operation
	return l.append(ctx, walAuthorityRecord{kind: walAuthorityAbort, walAuthorityReceipt: r})
}

func (l *walAuthorityLog) Close() error {
	if l == nil {
		return errWALAuthorityRecovery
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrOwnerClosed
	}
	l.closed = true
	clear(l.key[:])
	// close releases flock only after the owned file descriptor is closed.
	return l.file.Close()
}

func (l *walAuthorityLog) encode(r walAuthorityRecord) [walAuthorityRecordSize]byte {
	return encodeWALAuthorityRecord(l.identity, l.key, l.chain, r)
}

func (l *walAuthorityLog) decode(raw []byte) (walAuthorityRecord, error) {
	return decodeWALAuthorityRecord(l.identity, l.key, l.chain, raw)
}
