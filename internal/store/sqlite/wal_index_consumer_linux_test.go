//go:build linux

package sqlite

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
	"modernc.org/libc"
	_ "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// This subprocess-only oracle uses process-global, unsafe driver hooks on a
// test-owned database. It is not a safe state opener or a product VFS.
func TestWALIndexConsumerHoldKeepsSQLiteWriteLock(t *testing.T) {
	ctx, root, protected, lease, state, target := stateObservationFixture(t)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(random[:])
	if err := os.WriteFile(filepath.Join(state, "wal-memory.owner"), []byte(nonce), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWALIndexSQLiteHoldChild$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_WAL_HOLD_CHILD=1", "MCE4D_MEMFD_ORACLE_STATE="+state, "MCE4D_MEMFD_ORACLE_TARGET="+target, "MCE4D_MEMFD_ORACLE_NONCE="+nonce)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = in.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	stream := bufio.NewReaderSize(out, 512)
	line, err := stream.ReadString('\n')
	var peer WALIndexPeer
	n, scanErr := fmt.Sscanf(line, "READY %d %d %d %d %d %d", &peer.PID, &peer.StartTime, &peer.FD, &peer.Device, &peer.Inode, &peer.Size)
	if err != nil || scanErr != nil || n != 6 {
		t.Fatalf("SQLite oracle did not start: %q %v", line, err)
	}
	memory, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, allowWALPeer)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(io.SeekStart), Start: 120, Len: 1}
	checkBlocked := func() {
		t.Helper()
		err := unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock)
		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
			if err == nil {
				lock.Type = unix.F_UNLCK
				_ = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock)
			}
			t.Fatalf("SQLite writer lock was not held: %v", err)
		}
	}
	checkBlocked()
	if _, err = fmt.Fprintln(in, "TRY_CLOSE"); err != nil {
		t.Fatal(err)
	}
	line, err = stream.ReadString('\n')
	if err != nil || line != "CLOSE_REFUSED\n" {
		t.Fatalf("consumer did not retain descriptor: %q %v", line, err)
	}
	checkBlocked()
	if _, err = fmt.Fprintln(in, "FINISH"); err != nil {
		t.Fatal(err)
	}
	line, err = stream.ReadString('\n')
	if err != nil || line != "CLOSED\n" {
		t.Fatalf("SQLite consumer did not close: %q %v", line, err)
	}
	lock.Type = unix.F_WRLCK
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatalf("writer lock remained after SQLite close: %v", err)
	}
	lock.Type = unix.F_UNLCK
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	if err = in.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestWALIndexSQLiteHoldChild(t *testing.T) {
	if os.Getenv("MCE4D_WAL_HOLD_CHILD") != "1" {
		return
	}
	if err := runWALIndexSQLiteHoldChild(); err != nil {
		t.Fatal(err)
	}
}

func runWALIndexSQLiteHoldChild() error {
	root, protected, err := openWALIndexOracleFixture()
	if err != nil {
		return err
	}
	defer root.Close()
	defer protected.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lease, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		return err
	}
	defer lease.Close()
	memory, err := NewWALIndexMemory(ctx, root, []*paths.Root{protected}, lease, "catalog", walIndexMaxBytes, allowState)
	if err != nil {
		return err
	}
	defer memory.Close()
	use, err := memory.Hold(ctx)
	if err != nil {
		return err
	}
	defer use.Close()
	state := os.Getenv("MCE4D_MEMFD_ORACLE_STATE")
	path := filepath.Join(state, "wal-hold-fixture.sqlite")
	initial, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	_, initErr := initial.ExecContext(ctx, "PRAGMA journal_mode=WAL; CREATE TABLE fixture(n INTEGER)")
	if err = errors.Join(initErr, initial.Close()); err != nil {
		return err
	}
	if err = installWALHoldHooks(path, memory.file); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	peer, err := memory.Peer(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("READY %d %d %d %d %d %d\n", peer.PID, peer.StartTime, peer.FD, peer.Device, peer.Inode, peer.Size)
	in := bufio.NewReaderSize(os.Stdin, 64)
	line, err := in.ReadString('\n')
	if err != nil || line != "TRY_CLOSE\n" {
		return ErrWALIndexUnsafe
	}
	if err = memory.Close(); !errors.Is(err, ErrWALIndexInUse) {
		return errors.Join(ErrWALIndexUnsafe, err)
	}
	fmt.Println("CLOSE_REFUSED")
	line, err = in.ReadString('\n')
	if err != nil || line != "FINISH\n" {
		return ErrWALIndexUnsafe
	}
	if _, err = db.ExecContext(ctx, "ROLLBACK"); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	if err = use.Close(); err != nil {
		return err
	}
	if err = memory.Close(); err != nil {
		return err
	}
	fmt.Println("CLOSED")
	return nil
}

var walHoldSHMName, walHoldFDPath string
var walHoldDevice, walHoldInode uint64
var walHoldOpenOriginal func(*libc.TLS, uintptr, int32, int32) int32
var walHoldTruncateOriginal func(*libc.TLS, int32, libc.Toff_t) int32
var walHoldOpenFunction = walHoldOpen
var walHoldTruncateFunction = walHoldTruncate

func walHoldErrno(tls *libc.TLS, err syscall.Errno) int32 {
	code := int32(err)
	libc.Xmemcpy(tls, libc.X__errno_location(tls), uintptr(unsafe.Pointer(&code)), 4)
	goruntime.KeepAlive(&code)
	return -1
}

func walHoldOpen(tls *libc.TLS, name uintptr, flags, mode int32) int32 {
	if libc.GoString(name) != walHoldSHMName {
		return walHoldOpenOriginal(tls, name, flags, mode)
	}
	fd, err := unix.Open(walHoldFDPath, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return walHoldErrno(tls, syscall.EACCES)
	}
	return int32(fd)
}

func walHoldTruncate(tls *libc.TLS, fd int32, length libc.Toff_t) int32 {
	var st unix.Stat_t
	if unix.Fstat(int(fd), &st) == nil && uint64(st.Dev) == walHoldDevice && st.Ino == walHoldInode {
		if int64(length) != 3 || unix.Fallocate(int(fd), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, 0, walIndexMaxBytes) != nil {
			return walHoldErrno(tls, syscall.EIO)
		}
		return 0
	}
	return walHoldTruncateOriginal(tls, fd, length)
}

func installWALHoldHooks(path string, file *os.File) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	walHoldDevice, walHoldInode = uint64(st.Dev), st.Ino
	walHoldSHMName = path + "-shm"
	walHoldFDPath = fmt.Sprintf("/proc/self/fd/%d", file.Fd())
	tls := libc.NewTLS() // Fixture process exits after the SQLite handles close.
	vfs := sqlite3.Xsqlite3_vfs_find(tls, 0)
	if vfs == 0 {
		return ErrWALIndexUnsafe
	}
	var copyVFS sqlite3.Tsqlite3_vfs
	libc.Xmemcpy(tls, uintptr(unsafe.Pointer(&copyVFS)), vfs, uint64(unsafe.Sizeof(copyVFS)))
	if copyVFS.FiVersion < 3 || copyVFS.FxSetSystemCall == 0 || copyVFS.FxGetSystemCall == 0 {
		return ErrWALIndexUnsafe
	}
	setter := *(*func(*libc.TLS, uintptr, uintptr, uintptr) int32)(unsafe.Pointer(&copyVFS.FxSetSystemCall))
	getter := *(*func(*libc.TLS, uintptr, uintptr) uintptr)(unsafe.Pointer(&copyVFS.FxGetSystemCall))
	openName, err := libc.CString("open")
	if err != nil {
		return err
	}
	truncateName, err := libc.CString("ftruncate")
	if err != nil {
		return err
	}
	openAddress, truncateAddress := getter(tls, vfs, openName), getter(tls, vfs, truncateName)
	if openAddress == 0 || truncateAddress == 0 {
		return ErrWALIndexUnsafe
	}
	walHoldOpenOriginal = *(*func(*libc.TLS, uintptr, int32, int32) int32)(unsafe.Pointer(&openAddress))
	walHoldTruncateOriginal = *(*func(*libc.TLS, int32, libc.Toff_t) int32)(unsafe.Pointer(&truncateAddress))
	openHook := *(*uintptr)(unsafe.Pointer(&walHoldOpenFunction))
	truncateHook := *(*uintptr)(unsafe.Pointer(&walHoldTruncateFunction))
	if setter(tls, vfs, openName, openHook) != sqlite3.SQLITE_OK || setter(tls, vfs, truncateName, truncateHook) != sqlite3.SQLITE_OK {
		return ErrWALIndexUnsafe
	}
	return nil
}
