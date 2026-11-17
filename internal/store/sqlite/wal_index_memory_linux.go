//go:build linux

package sqlite

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

const walIndexPage int64 = 32 << 10
const walIndexMaxBytes int64 = 64 << 20
const walIndexSeals = unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL

type walIndexFileKey struct{ device, inode uint64 }

var walIndexLocal = struct {
	sync.Mutex
	active map[walIndexFileKey]struct{}
}{active: make(map[walIndexFileKey]struct{})}

// NewWALIndexMemory creates a size-sealed anonymous file and keeps a shared
// lifecycle attachment. It never creates or opens a database or sidecar.
func NewWALIndexMemory(ctx context.Context, root *paths.Root, protected []*paths.Root, lease *runtime.Lifecycle, scope string, size int64, authorize func(context.Context) error) (_ *WALIndexMemory, resultErr error) {
	m, err := newWALIndexOwner(ctx, root, protected, lease, scope, size, authorize)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, m.Close())
		}
	}()
	fd, err := unix.MemfdCreate("mce4d-wal-index", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	m.file = os.NewFile(uintptr(fd), "anonymous-wal-index")
	if m.file == nil {
		_ = unix.Close(fd)
		return nil, ErrWALIndexUnsafe
	}
	if err = unix.Fchmod(fd, 0600); err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	if err = unix.Ftruncate(fd, size); err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	if _, err = unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, walIndexSeals); err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	identity, err := validateWALIndexFile(m.file, size)
	if err != nil {
		return nil, err
	}
	m.identity = identity
	if err = registerWALIndex(m, identity); err != nil {
		return nil, err
	}
	if err = m.recheck(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// OpenWALIndexPeer imports an anonymous file descriptor from an independently
// authorized peer. Only the final procfs fd link is followed: it is first
// pinned with O_PATH and verified before being reopened as writable memory.
// This does not discover peers or prove there is no competing WAL index.
func OpenWALIndexPeer(ctx context.Context, root *paths.Root, protected []*paths.Root, lease *runtime.Lifecycle, scope string, peer WALIndexPeer, authorize func(context.Context) error, authorizePeer func(context.Context, WALIndexPeer) error) (_ *WALIndexMemory, resultErr error) {
	if authorizePeer == nil || peer.PID < 1 || peer.PID == os.Getpid() || peer.FD < 0 || peer.StartTime == 0 || peer.Inode == 0 {
		return nil, ErrWALIndexUnsafe
	}
	m, err := newWALIndexOwner(ctx, root, protected, lease, scope, peer.Size, authorize)
	if err != nil {
		return nil, err
	}
	m.peer = &peer
	m.authorizePeer = authorizePeer
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, m.Close())
		}
	}()
	if err = m.checkAuthority(ctx); err != nil {
		return nil, err
	}
	pidfd, err := unix.PidfdOpen(peer.PID, 0)
	if err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	defer unix.Close(pidfd)
	if err = requireLivePID(pidfd); err != nil {
		return nil, err
	}
	proc, err := verifiedProc()
	if err != nil {
		return nil, err
	}
	defer unix.Close(proc)
	pidDir, err := openProcPID(proc, peer.PID)
	if err != nil {
		return nil, err
	}
	defer unix.Close(pidDir)
	start, err := procStartTime(pidDir, peer.PID)
	if err != nil || start != peer.StartTime {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	fdDir, err := openProcDirectory(pidDir, "fd")
	if err != nil {
		return nil, err
	}
	defer unix.Close(fdDir)
	// O_PATH pins the target without opening a device or touching its content.
	pathFD, err := unix.Openat(fdDir, strconv.Itoa(peer.FD), unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	defer unix.Close(pathFD)
	if err = validateWALIndexPath(pathFD, peer); err != nil {
		return nil, err
	}
	selfDir, err := openProcPID(proc, os.Getpid())
	if err != nil {
		return nil, err
	}
	defer unix.Close(selfDir)
	selfFDDir, err := openProcDirectory(selfDir, "fd")
	if err != nil {
		return nil, err
	}
	defer unix.Close(selfFDDir)
	identity, err := openRegisteredWALIndex(m, selfFDDir, pathFD, peer)
	if err != nil {
		return nil, err
	}
	m.identity = identity
	start, err = procStartTime(pidDir, peer.PID)
	if err != nil || start != peer.StartTime || requireLivePID(pidfd) != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	if err = m.recheck(ctx); err != nil {
		return nil, err
	}
	// The peer may have closed or replaced its fd while authority was checked.
	// Reopen it only as O_PATH and compare the pinned object again before
	// returning. A cooperative registry is still needed to coordinate handover
	// after this check; this observation cannot prevent a later close.
	peerFD, err := unix.Openat(fdDir, strconv.Itoa(peer.FD), unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	err = errors.Join(validateWALIndexPath(peerFD, peer), unix.Close(peerFD))
	if err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	start, err = procStartTime(pidDir, peer.PID)
	if err != nil || start != peer.StartTime || requireLivePID(pidfd) != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	return m, nil
}

func walIndexKey(identity WALIndexPeer) walIndexFileKey {
	return walIndexFileKey{device: identity.Device, inode: identity.Inode}
}

func registerWALIndex(m *WALIndexMemory, identity WALIndexPeer) error {
	walIndexLocal.Lock()
	defer walIndexLocal.Unlock()
	key := walIndexKey(identity)
	if _, exists := walIndexLocal.active[key]; exists {
		return ErrWALIndexUnsafe
	}
	walIndexLocal.active[key] = struct{}{}
	m.registered = true
	return nil
}

// Linux POSIX locks are process-associated: closing any ordinary fd for an
// inode can release locks held through another fd in the same process. Never
// create a second local managed fd for an already-registered WAL index.
func openRegisteredWALIndex(m *WALIndexMemory, fdDir, pathFD int, peer WALIndexPeer) (WALIndexPeer, error) {
	walIndexLocal.Lock()
	defer walIndexLocal.Unlock()
	key := walIndexKey(peer)
	if _, exists := walIndexLocal.active[key]; exists {
		return WALIndexPeer{}, ErrWALIndexUnsafe
	}
	fd, err := unix.Openat(fdDir, strconv.Itoa(pathFD), unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return WALIndexPeer{}, errors.Join(ErrWALIndexUnsafe, err)
	}
	file := os.NewFile(uintptr(fd), "anonymous-wal-index-peer")
	if file == nil {
		_ = unix.Close(fd)
		return WALIndexPeer{}, ErrWALIndexUnsafe
	}
	identity, err := validateWALIndexFile(file, peer.Size)
	if err != nil || identity.Device != peer.Device || identity.Inode != peer.Inode {
		return WALIndexPeer{}, errors.Join(ErrWALIndexUnsafe, err, file.Close())
	}
	walIndexLocal.active[key] = struct{}{}
	m.registered, m.file = true, file
	return identity, nil
}

func newWALIndexOwner(ctx context.Context, root *paths.Root, protected []*paths.Root, lease *runtime.Lifecycle, scope string, size int64, authorize func(context.Context) error) (*WALIndexMemory, error) {
	if ctx == nil || root == nil || lease == nil || authorize == nil || size < walIndexPage || size > walIndexMaxBytes || size%walIndexPage != 0 || len(protected) < 1 || len(protected) > 64 {
		return nil, ErrWALIndexUnsafe
	}
	for _, item := range protected {
		if item == nil {
			return nil, ErrWALIndexUnsafe
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	m := &WALIndexMemory{root: root, protected: append([]*paths.Root(nil), protected...), lease: lease, hold: hold, scope: scope, authorize: authorize}
	if err = m.checkAuthority(ctx); err != nil {
		return nil, errors.Join(err, hold.Close())
	}
	return m, nil
}

func (m *WALIndexMemory) checkAuthority(ctx context.Context) error {
	if m == nil || ctx == nil || m.root == nil || m.lease == nil || m.hold == nil || m.authorize == nil || m.closed {
		return ErrWALIndexUnsafe
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrWALIndexUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.authorize(ctx) != nil || m.lease.CheckRoot(ctx, m.root, m.scope, ports.SharedLease) != nil || stateRootOwner(m.root) != nil || runtime.CheckCleanupRoot(m.root, m.protected) != nil || m.peer != nil && (m.authorizePeer == nil || m.authorizePeer(ctx, *m.peer) != nil) || m.authorize(ctx) != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrWALIndexUnsafe
	}
	return ctx.Err()
}

func validateWALIndexPath(fd int, expected WALIndexPeer) error {
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatfs(fd, &fs) != nil || uint64(fs.Type) != uint64(unix.TMPFS_MAGIC) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != uint32(unix.Geteuid()) || st.Nlink != 0 || st.Size != expected.Size || uint64(st.Dev) != expected.Device || st.Ino != expected.Inode {
		return ErrWALIndexUnsafe
	}
	return nil
}

func validateWALIndexFile(file *os.File, size int64) (WALIndexPeer, error) {
	if file == nil {
		return WALIndexPeer{}, ErrWALIndexUnsafe
	}
	var st unix.Stat_t
	fd := int(file.Fd())
	if unix.Fstat(fd, &st) != nil {
		return WALIndexPeer{}, ErrWALIndexUnsafe
	}
	identity := WALIndexPeer{Device: uint64(st.Dev), Inode: st.Ino, Size: size}
	if identity.Inode == 0 || validateWALIndexPath(fd, identity) != nil {
		return WALIndexPeer{}, ErrWALIndexUnsafe
	}
	seals, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil || seals != walIndexSeals {
		return WALIndexPeer{}, errors.Join(ErrWALIndexUnsafe, err)
	}
	return identity, nil
}

func (m *WALIndexMemory) recheck(ctx context.Context) error {
	if m.closeErr != nil {
		return errors.Join(ErrWALIndexUnsafe, m.closeErr)
	}
	if err := m.checkAuthority(ctx); err != nil {
		return err
	}
	actual, err := validateWALIndexFile(m.file, m.identity.Size)
	if err != nil || actual.Device != m.identity.Device || actual.Inode != m.identity.Inode {
		return errors.Join(ErrWALIndexUnsafe, err)
	}
	return m.checkAuthority(ctx)
}

// Recheck observes current authority, lifecycle and fd identity. It does not
// prevent an uncooperative same-user actor from mutating mapped bytes.
func (m *WALIndexMemory) Recheck(ctx context.Context) error {
	if m == nil {
		return ErrWALIndexUnsafe
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recheck(ctx)
}

// Peer returns this process's pinned descriptor identity for an authorized
// registry. Callers must not expose it as a capability to untrusted requests.
func (m *WALIndexMemory) Peer(ctx context.Context) (WALIndexPeer, error) {
	if m == nil {
		return WALIndexPeer{}, ErrWALIndexUnsafe
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.recheck(ctx); err != nil {
		return WALIndexPeer{}, err
	}
	proc, err := verifiedProc()
	if err != nil {
		return WALIndexPeer{}, err
	}
	defer unix.Close(proc)
	dir, err := openProcPID(proc, os.Getpid())
	if err != nil {
		return WALIndexPeer{}, err
	}
	defer unix.Close(dir)
	start, err := procStartTime(dir, os.Getpid())
	if err != nil || start == 0 {
		return WALIndexPeer{}, errors.Join(ErrWALIndexUnsafe, err)
	}
	peer := m.identity
	peer.PID, peer.StartTime, peer.FD = os.Getpid(), start, int(m.file.Fd())
	if err = m.recheck(ctx); err != nil {
		return WALIndexPeer{}, err
	}
	return peer, nil
}

// Hold attaches a consumer to the descriptor lifetime. It is not a proof that
// the consumer has actually closed every fd or mapping before releasing it.
func (m *WALIndexMemory) Hold(ctx context.Context) (*WALIndexUse, error) {
	if m == nil {
		return nil, ErrWALIndexUnsafe
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.recheck(ctx); err != nil {
		return nil, err
	}
	m.users++
	return &WALIndexUse{owner: m}, nil
}

// Close releases an attachment even if authority has since been revoked.
func (use *WALIndexUse) Close() error {
	if use == nil {
		return ErrWALIndexUnsafe
	}
	use.mu.Lock()
	defer use.mu.Unlock()
	if use.owner == nil {
		return ErrOwnerClosed
	}
	use.owner.mu.Lock()
	use.owner.users--
	use.owner.mu.Unlock()
	use.owner = nil
	return nil
}

// Map attaches a bounded region while retaining the memory object's lifecycle.
// The caller is responsible for preventing use of the slice after unmapping.
func (m *WALIndexMemory) Map(ctx context.Context, offset, length int64) (*WALIndexMapping, error) {
	if m == nil {
		return nil, ErrWALIndexUnsafe
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.recheck(ctx); err != nil {
		return nil, err
	}
	if offset < 0 || length <= 0 || offset%walIndexPage != 0 || length%walIndexPage != 0 || length > m.identity.Size || offset > m.identity.Size-length {
		return nil, ErrWALIndexUnsafe
	}
	data, err := unix.Mmap(int(m.file.Fd()), offset, int(length), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, errors.Join(ErrWALIndexUnsafe, err)
	}
	if err = m.recheck(ctx); err != nil {
		if unmapErr := unix.Munmap(data); unmapErr != nil {
			m.mappings++
			m.closeErr = errors.Join(ErrWALIndexUnsafe, unmapErr)
			return nil, errors.Join(err, m.closeErr)
		}
		return nil, err
	}
	m.mappings++
	return &WALIndexMapping{owner: m, data: data}, nil
}

// Bytes is valid only until Close; a caller must synchronize its own use.
func (mapping *WALIndexMapping) Bytes() ([]byte, error) {
	if mapping == nil {
		return nil, ErrWALIndexUnsafe
	}
	mapping.mu.Lock()
	defer mapping.mu.Unlock()
	if mapping.closed || mapping.owner == nil {
		return nil, ErrOwnerClosed
	}
	return mapping.data, nil
}

// Close unmaps even after authority revocation and releases the attachment
// only after the kernel reports successful unmapping.
func (mapping *WALIndexMapping) Close() error {
	if mapping == nil {
		return ErrWALIndexUnsafe
	}
	mapping.mu.Lock()
	defer mapping.mu.Unlock()
	if mapping.closed || mapping.owner == nil {
		return ErrOwnerClosed
	}
	owner := mapping.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if err := unix.Munmap(mapping.data); err != nil {
		return errors.Join(ErrWALIndexUnsafe, err)
	}
	owner.mappings--
	mapping.data = nil
	mapping.owner = nil
	mapping.closed = true
	return nil
}

// Close drops the descriptor before its lifecycle attachment. A close failure
// retains the attachment because safe release cannot be established.
func (m *WALIndexMemory) Close() error {
	if m == nil {
		return ErrWALIndexUnsafe
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrOwnerClosed
	}
	if m.closeErr != nil {
		return m.closeErr
	}
	if m.mappings != 0 || m.users != 0 {
		return ErrWALIndexInUse
	}
	if m.file != nil {
		if m.registered {
			walIndexLocal.Lock()
			m.closeErr = m.file.Close()
			if m.closeErr == nil {
				delete(walIndexLocal.active, walIndexKey(m.identity))
				m.registered = false
			}
			walIndexLocal.Unlock()
		} else {
			m.closeErr = m.file.Close()
		}
		if m.closeErr != nil {
			return m.closeErr
		}
		m.file = nil
	}
	if m.hold != nil {
		if m.closeErr = m.hold.Close(); m.closeErr != nil {
			return m.closeErr
		}
		m.hold = nil
	}
	m.closed = true
	return nil
}

func verifiedProc() (int, error) {
	fd, err := unix.Open("/proc", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, errors.Join(ErrWALIndexUnsafe, err)
	}
	var fs unix.Statfs_t
	buf := make([]byte, 32)
	n, readErr := unix.Readlinkat(fd, "self", buf)
	if unix.Fstatfs(fd, &fs) != nil || uint64(fs.Type) != uint64(unix.PROC_SUPER_MAGIC) || readErr != nil || string(buf[:n]) != strconv.Itoa(os.Getpid()) {
		_ = unix.Close(fd)
		return -1, ErrWALIndexUnsafe
	}
	return fd, nil
}

func openProcPID(proc, pid int) (int, error) {
	if pid < 1 {
		return -1, ErrWALIndexUnsafe
	}
	fd, err := openProcDirectory(proc, strconv.Itoa(pid))
	if err != nil {
		return -1, err
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(unix.Geteuid()) {
		_ = unix.Close(fd)
		return -1, ErrWALIndexUnsafe
	}
	return fd, nil
}

func openProcDirectory(parent int, name string) (int, error) {
	fd, err := unix.Openat(parent, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, errors.Join(ErrWALIndexUnsafe, err)
	}
	var fs unix.Statfs_t
	if unix.Fstatfs(fd, &fs) != nil || uint64(fs.Type) != uint64(unix.PROC_SUPER_MAGIC) {
		_ = unix.Close(fd)
		return -1, ErrWALIndexUnsafe
	}
	return fd, nil
}

func procStartTime(pidDir, pid int) (uint64, error) {
	fd, err := unix.Openat(pidDir, "stat", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return 0, errors.Join(ErrWALIndexUnsafe, err)
	}
	file := os.NewFile(uintptr(fd), "proc-stat")
	if file == nil {
		_ = unix.Close(fd)
		return 0, ErrWALIndexUnsafe
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 8193))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 8192 || !strings.HasPrefix(string(data), strconv.Itoa(pid)+" (") {
		return 0, errors.Join(ErrWALIndexUnsafe, readErr, closeErr)
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, ErrWALIndexUnsafe
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return 0, ErrWALIndexUnsafe
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return 0, errors.Join(ErrWALIndexUnsafe, err)
	}
	return start, nil
}

func requireLivePID(pidfd int) error {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 0)
	if err != nil || n != 0 || fds[0].Revents != 0 {
		return errors.Join(ErrWALIndexUnsafe, err)
	}
	return nil
}
