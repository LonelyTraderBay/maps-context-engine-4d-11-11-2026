//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

const (
	walAuthorityRecordSize     = 512
	walAuthorityMaxBytes       = 16 << 20
	walAuthorityName           = "wal-authority.log"
	walAuthorityBootstrap      = byte(1)
	walAuthorityPrepare        = byte(2)
	walAuthorityCommit         = byte(3)
	walAuthorityAbort          = byte(4)
	walAuthorityCommitQuiesced = byte(5)
)

var (
	errWALAuthorityRecovery = errors.New("WAL authority requires reconciliation")
	errWALAuthorityPending  = errors.New("WAL transaction outcome is pending")
	errWALAuthorityBusy     = errors.New("WAL authority is in use")
)

// This identity and key must come from a trusted owner, never from the journal
// being authenticated. Store binds the database kind and worktree/catalog ID;
// Incarnation and Authority distinguish replacements from the previous store.
type walAuthorityIdentity struct {
	Authority   [16]byte
	Store       [32]byte
	Incarnation [16]byte
}

type walAuthorityReceipt struct {
	Sequence  uint64
	Operation [32]byte
	Image     walLogicalImage
	Prefix    walCommitPrefixReceipt
}

type walAuthorityRecord struct {
	kind byte
	walAuthorityReceipt
}

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

func validAuthorityImage(image walLogicalImage) bool {
	return image.PageSize >= 512 && image.PageSize <= 65536 && image.PageSize&(image.PageSize-1) == 0 && image.PageCount > 0 &&
		uint64(image.PageSize)*uint64(image.PageCount) <= uint64(walLogicalImageMaxBytes) && image.SHA256 != [32]byte{}
}

func validAuthorityPrefix(prefix walCommitPrefixReceipt, image walLogicalImage) bool {
	return prefix.MainSize >= 100 && prefix.MainSize%int64(image.PageSize) == 0 && prefix.WALCommitFrame > 0 &&
		prefix.WALCommitPages == image.PageCount && prefix.WALPrefixSize >= 32 &&
		uint64(prefix.WALPrefixSize) == 32+uint64(prefix.WALCommitFrame)*(uint64(image.PageSize)+24) &&
		prefix.MainSize <= walLogicalImageMaxBytes-prefix.WALPrefixSize && prefix.MainSHA256 != [32]byte{} && prefix.WALPrefixSHA256 != [32]byte{}
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
	if first {
		if r.kind != walAuthorityBootstrap || r.Sequence != 0 || r.Operation != [32]byte{} || r.Prefix != (walCommitPrefixReceipt{}) || !validAuthorityImage(r.Image) {
			return errWALAuthorityRecovery
		}
		l.head = r.walAuthorityReceipt
		return nil
	}
	if r.Sequence == 0 || l.head.Sequence == math.MaxUint64 || r.Sequence != l.head.Sequence+1 || r.Operation == [32]byte{} || !validAuthorityImage(r.Image) {
		return errWALAuthorityRecovery
	}
	switch r.kind {
	case walAuthorityPrepare:
		if l.pending != nil || r.Image != l.head.Image || r.Prefix != (walCommitPrefixReceipt{}) {
			return errWALAuthorityRecovery
		}
		p := r.walAuthorityReceipt
		l.pending = &p
	case walAuthorityCommit, walAuthorityCommitQuiesced, walAuthorityAbort:
		if l.pending == nil || r.Operation != l.pending.Operation || r.Sequence != l.pending.Sequence {
			return errWALAuthorityRecovery
		}
		if r.kind == walAuthorityCommit && !validAuthorityPrefix(r.Prefix, r.Image) ||
			r.kind == walAuthorityCommitQuiesced && r.Prefix != (walCommitPrefixReceipt{}) ||
			r.kind == walAuthorityAbort && (r.Image != l.head.Image || r.Prefix != l.head.Prefix) {
			return errWALAuthorityRecovery
		}
		l.head = r.walAuthorityReceipt
		l.pending = nil
	default:
		return errWALAuthorityRecovery
	}
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

func (l *walAuthorityLog) encode(r walAuthorityRecord) (raw [walAuthorityRecordSize]byte) {
	copy(raw[:8], "MCEWAL1\x00")
	raw[8] = r.kind
	binary.BigEndian.PutUint64(raw[16:24], r.Sequence)
	copy(raw[24:40], l.identity.Authority[:])
	copy(raw[40:72], l.identity.Store[:])
	copy(raw[72:88], l.identity.Incarnation[:])
	copy(raw[88:120], l.chain[:])
	copy(raw[120:152], r.Operation[:])
	binary.BigEndian.PutUint32(raw[152:156], r.Image.PageSize)
	binary.BigEndian.PutUint32(raw[156:160], r.Image.PageCount)
	copy(raw[160:192], r.Image.SHA256[:])
	binary.BigEndian.PutUint64(raw[192:200], uint64(r.Prefix.MainSize))
	copy(raw[200:232], r.Prefix.MainSHA256[:])
	binary.BigEndian.PutUint64(raw[232:240], uint64(r.Prefix.WALPrefixSize))
	copy(raw[240:272], r.Prefix.WALPrefixSHA256[:])
	binary.BigEndian.PutUint32(raw[272:276], r.Prefix.WALCommitFrame)
	binary.BigEndian.PutUint32(raw[276:280], r.Prefix.WALCommitPages)
	mac := hmac.New(sha256.New, l.key[:])
	_, _ = mac.Write(raw[:480])
	copy(raw[480:], mac.Sum(nil))
	return raw
}

func (l *walAuthorityLog) decode(raw []byte) (walAuthorityRecord, error) {
	var r walAuthorityRecord
	if len(raw) != walAuthorityRecordSize || string(raw[:8]) != "MCEWAL1\x00" || !bytes.Equal(raw[9:16], make([]byte, 7)) ||
		!bytes.Equal(raw[280:480], make([]byte, 200)) || !bytes.Equal(raw[24:40], l.identity.Authority[:]) ||
		!bytes.Equal(raw[40:72], l.identity.Store[:]) || !bytes.Equal(raw[72:88], l.identity.Incarnation[:]) || !bytes.Equal(raw[88:120], l.chain[:]) {
		return r, errWALAuthorityRecovery
	}
	mac := hmac.New(sha256.New, l.key[:])
	_, _ = mac.Write(raw[:480])
	if !hmac.Equal(raw[480:], mac.Sum(nil)) {
		return r, errWALAuthorityRecovery
	}
	r.kind, r.Sequence = raw[8], binary.BigEndian.Uint64(raw[16:24])
	copy(r.Operation[:], raw[120:152])
	r.Image.PageSize, r.Image.PageCount = binary.BigEndian.Uint32(raw[152:156]), binary.BigEndian.Uint32(raw[156:160])
	copy(r.Image.SHA256[:], raw[160:192])
	r.Prefix.MainSize = int64(binary.BigEndian.Uint64(raw[192:200]))
	copy(r.Prefix.MainSHA256[:], raw[200:232])
	r.Prefix.WALPrefixSize = int64(binary.BigEndian.Uint64(raw[232:240]))
	copy(r.Prefix.WALPrefixSHA256[:], raw[240:272])
	r.Prefix.WALCommitFrame, r.Prefix.WALCommitPages = binary.BigEndian.Uint32(raw[272:276]), binary.BigEndian.Uint32(raw[276:280])
	return r, nil
}
