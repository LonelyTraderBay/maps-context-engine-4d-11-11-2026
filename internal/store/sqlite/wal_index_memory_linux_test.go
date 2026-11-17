//go:build linux

package sqlite

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func allowWALPeer(context.Context, WALIndexPeer) error { return nil }

func TestWALIndexMemorySealsSharingAndLifecycle(t *testing.T) {
	ctx, root, protected, lease, _, target := stateObservationFixture(t)
	first, err := NewWALIndexMemory(ctx, root, []*paths.Root{protected}, lease, "catalog", walIndexPage, allowState)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	peer, err := first.Peer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, allowWALPeer); second != nil || !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("same-process duplicate accepted: %v", err)
	}
	if err = lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("memory handles did not retain lifecycle: %v", err)
	}
	firstMapping, err := first.Map(ctx, 0, walIndexPage)
	if err != nil {
		t.Fatal(err)
	}
	defer firstMapping.Close()
	secondMapping, err := first.Map(ctx, 0, walIndexPage)
	if err != nil {
		t.Fatal(err)
	}
	defer secondMapping.Close()
	firstMap, err := firstMapping.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	secondMap, err := secondMapping.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	copy(firstMap[:8], []byte("wal-test"))
	if string(secondMap[:8]) != "wal-test" {
		t.Fatal("peer did not observe shared memory")
	}
	if err = unix.Ftruncate(int(first.file.Fd()), walIndexPage*2); !errors.Is(err, unix.EPERM) {
		t.Fatalf("sealed memory grew: %v", err)
	}
	if err = unix.Ftruncate(int(first.file.Fd()), 0); !errors.Is(err, unix.EPERM) {
		t.Fatalf("sealed memory shrank: %v", err)
	}
	alias := filepath.Join(target, "alias")
	if err = unix.Linkat(int(first.file.Fd()), "", unix.AT_FDCWD, alias, unix.AT_EMPTY_PATH); err == nil {
		t.Fatal("anonymous memory was linked into protected fixture")
	}
	if _, err = os.Stat(alias); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected protected alias: %v", err)
	}
	for _, bounds := range [][2]int64{{-walIndexPage, walIndexPage}, {0, 0}, {1, walIndexPage}, {0, 1}, {walIndexPage, walIndexPage}, {walIndexMaxBytes, walIndexPage}, {0, walIndexMaxBytes + walIndexPage}} {
		if mapping, mapErr := first.Map(ctx, bounds[0], bounds[1]); mapping != nil || !errors.Is(mapErr, ErrWALIndexUnsafe) {
			t.Fatalf("invalid mapping accepted %v: %v", bounds, mapErr)
		}
	}
	if err = first.Close(); !errors.Is(err, ErrWALIndexInUse) {
		t.Fatalf("memory closed with two live mappings: %v", err)
	}
	if err = firstMapping.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = firstMapping.Bytes(); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("closed mapping returned bytes: %v", err)
	}
	if err = first.Close(); !errors.Is(err, ErrWALIndexInUse) {
		t.Fatalf("memory closed with one live mapping: %v", err)
	}
	if err = lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("mapping did not retain lifecycle: %v", err)
	}
	if err = secondMapping.Close(); err != nil {
		t.Fatal(err)
	}
	user, err := first.Hold(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer user.Close()
	secondUser, err := first.Hold(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer secondUser.Close()
	if err = first.Close(); !errors.Is(err, ErrWALIndexInUse) {
		t.Fatalf("memory closed while a consumer held it: %v", err)
	}
	if err = lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("consumer did not retain lifecycle: %v", err)
	}
	if err = user.Close(); err != nil {
		t.Fatal(err)
	}
	if err = user.Close(); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("consumer released twice: %v", err)
	}
	if err = first.Close(); !errors.Is(err, ErrWALIndexInUse) {
		t.Fatalf("one release closed another consumer's descriptor: %v", err)
	}
	if err = secondUser.Close(); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if mapping, mapErr := first.Map(ctx, 0, walIndexPage); mapping != nil || !errors.Is(mapErr, ErrWALIndexUnsafe) {
		t.Fatalf("closed memory accepted mapping: %v", mapErr)
	}
	if user, holdErr := first.Hold(ctx); user != nil || !errors.Is(holdErr, ErrWALIndexUnsafe) {
		t.Fatalf("closed memory accepted consumer: %v", holdErr)
	}
	if err = lease.Close(); err != nil {
		t.Fatalf("lifecycle not released after handles closed: %v", err)
	}
}

func TestWALIndexMemoryRejectsForgedPeer(t *testing.T) {
	ctx, root, protected, lease, state, target := stateObservationFixture(t)
	remote := startWALIndexCreator(t, ctx, state, target)
	peer := remote.valid
	first, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, allowWALPeer)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*WALIndexPeer)
	}{
		{"pid", func(p *WALIndexPeer) { p.PID = 0 }},
		{"start", func(p *WALIndexPeer) { p.StartTime++ }},
		{"fd", func(p *WALIndexPeer) { p.FD = -1 }},
		{"device", func(p *WALIndexPeer) { p.Device++ }},
		{"inode", func(p *WALIndexPeer) { p.Inode++ }},
		{"size", func(p *WALIndexPeer) { p.Size *= 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forged := peer
			tc.edit(&forged)
			if got, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", forged, allowState, allowWALPeer); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
				t.Fatalf("forged peer accepted: %v", err)
			}
		})
	}
	if got, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, nil); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("missing peer authority accepted: %v", err)
	}
	deny := func(context.Context, WALIndexPeer) error { return errors.New("revoked") }
	if got, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, deny); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("revoked peer accepted: %v", err)
	}
}

func TestWALIndexMemoryRejectsUnsealedAndNonAnonymousFiles(t *testing.T) {
	ctx, root, protected, lease, state, target := stateObservationFixture(t)
	remote := startWALIndexCreator(t, ctx, state, target)
	for name, peer := range map[string]WALIndexPeer{"linked": remote.linked, "unlinked": remote.unlinked, "unsealed": remote.unsealed} {
		if got, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, allowWALPeer); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
			t.Fatalf("%s file accepted: %v", name, err)
		}
	}
}

func TestWALIndexMemoryRejectsDuplicateWithoutDroppingLocks(t *testing.T) {
	ctx, root, protected, lease, state, target := stateObservationFixture(t)
	remote := startWALIndexCreator(t, ctx, state, target)
	memory, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", remote.valid, allowState, allowWALPeer)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	use, err := memory.Hold(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer use.Close()
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(io.SeekStart), Start: 127, Len: 1}
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	defer func() {
		lock.Type = unix.F_UNLCK
		_ = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock)
	}()
	if duplicate, dupErr := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", remote.valid, allowState, allowWALPeer); duplicate != nil || !errors.Is(dupErr, ErrWALIndexUnsafe) {
		t.Fatalf("duplicate local descriptor accepted: %v", dupErr)
	}
	if err = memory.Close(); !errors.Is(err, ErrWALIndexInUse) {
		t.Fatalf("descriptor closed while consumer held a POSIX lock: %v", err)
	}
	if _, err = fmt.Fprintln(remote.stdin, "CHECK"); err != nil {
		t.Fatal(err)
	}
	line, err := remote.stdout.ReadSlice('\n')
	if err != nil || string(line) != "LOCK_BLOCKED\n" {
		t.Fatalf("duplicate attempt released another fd's POSIX lock: %q %v", line, err)
	}
	lock.Type = unix.F_UNLCK
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	if err = remote.stop(); err != nil {
		t.Fatal(err)
	}
	if err = memory.Recheck(ctx); err != nil {
		t.Fatalf("imported handle did not survive its creator: %v", err)
	}
	newPeer, err := memory.Peer(ctx)
	if err != nil || newPeer.PID != os.Getpid() || newPeer.Device != remote.valid.Device || newPeer.Inode != remote.valid.Inode {
		t.Fatalf("surviving holder lost memory identity: %v", err)
	}
	if got, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", remote.valid, allowState, allowWALPeer); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("dead creator descriptor accepted: %v", err)
	}
}

func TestWALIndexMemoryConcurrentPeerImports(t *testing.T) {
	ctx, root, protected, lease, state, target := stateObservationFixture(t)
	remote := startWALIndexCreator(t, ctx, state, target)
	type result struct {
		memory *WALIndexMemory
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			memory, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", remote.valid, allowState, allowWALPeer)
			results <- result{memory, err}
		}()
	}
	close(start)
	accepted, rejected := 0, 0
	var opened []*WALIndexMemory
	for i := 0; i < 2; i++ {
		r := <-results
		if r.memory != nil {
			opened = append(opened, r.memory)
			accepted++
		} else if errors.Is(r.err, ErrWALIndexUnsafe) {
			rejected++
		} else {
			t.Errorf("unexpected import result: %v", r.err)
		}
	}
	for _, memory := range opened {
		if err := memory.Close(); err != nil {
			t.Error(err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent imports: accepted=%d rejected=%d", accepted, rejected)
	}
}

func TestWALIndexMemoryRejectsPeerDescriptorDroppedDuringImport(t *testing.T) {
	ctx, root, protected, lease, state, target := stateObservationFixture(t)
	remote := startWALIndexCreator(t, ctx, state, target)
	calls := 0
	verifyPeer := func(context.Context, WALIndexPeer) error {
		calls++
		if calls != 2 {
			return nil
		}
		if _, err := fmt.Fprintln(remote.stdin, "DROP"); err != nil {
			return err
		}
		line, err := remote.stdout.ReadSlice('\n')
		if err != nil || string(line) != "DROPPED\n" {
			return ErrWALIndexUnsafe
		}
		return nil
	}
	got, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", remote.valid, allowState, verifyPeer)
	if got != nil {
		_ = got.Close()
	}
	if got != nil || !errors.Is(err, ErrWALIndexUnsafe) || calls < 2 {
		t.Fatalf("accepted peer that dropped its descriptor during import: %v (calls=%d)", err, calls)
	}
}

type walIndexCreator struct {
	cmd                               *exec.Cmd
	stdin                             io.WriteCloser
	stdout                            *bufio.Reader
	valid, linked, unlinked, unsealed WALIndexPeer
}

func startWALIndexCreator(t *testing.T, ctx context.Context, state, target string) *walIndexCreator {
	t.Helper()
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
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWALIndexMemoryCreatorProcess$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_MEMFD_ORACLE_CHILD=creator", "MCE4D_MEMFD_ORACLE_STATE="+state, "MCE4D_MEMFD_ORACLE_TARGET="+target, "MCE4D_MEMFD_ORACLE_NONCE="+nonce)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &walIndexCreator{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 256)}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	for _, item := range []struct {
		label string
		peer  *WALIndexPeer
	}{{"VALID", &c.valid}, {"LINKED", &c.linked}, {"UNLINKED", &c.unlinked}, {"UNSEALED", &c.unsealed}} {
		line, readErr := c.stdout.ReadSlice('\n')
		var label string
		n, scanErr := fmt.Sscanf(string(line), "%s %d %d %d %d %d %d", &label, &item.peer.PID, &item.peer.StartTime, &item.peer.FD, &item.peer.Device, &item.peer.Inode, &item.peer.Size)
		if readErr != nil || scanErr != nil || n != 7 || label != item.label {
			t.Fatalf("creator did not supply %s fixture: %q %v", item.label, line, readErr)
		}
	}
	return c
}

func (c *walIndexCreator) stop() error {
	if c.cmd.ProcessState != nil {
		return nil
	}
	_, sendErr := fmt.Fprintln(c.stdin, "STOP")
	return errors.Join(sendErr, c.stdin.Close(), c.cmd.Wait())
}

func TestWALIndexMemoryCreatorProcess(t *testing.T) {
	if os.Getenv("MCE4D_MEMFD_ORACLE_CHILD") != "creator" {
		return
	}
	if err := runWALIndexCreatorProcess(); err != nil {
		fmt.Fprintln(os.Stdout, "ERROR")
	}
}

func runWALIndexCreatorProcess() error {
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
	memory, err := NewWALIndexMemory(ctx, root, []*paths.Root{protected}, lease, "catalog", walIndexPage, allowState)
	if err != nil {
		return err
	}
	defer memory.Close()
	peer, err := memory.Peer(ctx)
	if err != nil {
		return err
	}
	state := os.Getenv("MCE4D_MEMFD_ORACLE_STATE")
	linked, err := os.OpenFile(filepath.Join(state, "linked-fixture"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer linked.Close()
	if err = linked.Truncate(walIndexPage); err != nil {
		return err
	}
	unlinked, err := os.OpenFile(filepath.Join(state, "unlinked-fixture"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer unlinked.Close()
	if err = unlinked.Truncate(walIndexPage); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(state, "unlinked-fixture")); err != nil {
		return err
	}
	unsealed, err := unix.MemfdCreate("unsealed-fixture", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	defer unix.Close(unsealed)
	if err = unix.Fchmod(unsealed, 0600); err != nil {
		return err
	}
	if err = unix.Ftruncate(unsealed, walIndexPage); err != nil {
		return err
	}
	for _, item := range []struct {
		name string
		fd   int
	}{{"VALID", int(memory.file.Fd())}, {"LINKED", int(linked.Fd())}, {"UNLINKED", int(unlinked.Fd())}, {"UNSEALED", unsealed}} {
		var st unix.Stat_t
		if err = unix.Fstat(item.fd, &st); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s %d %d %d %d %d %d\n", item.name, peer.PID, peer.StartTime, item.fd, uint64(st.Dev), st.Ino, st.Size)
	}
	in := bufio.NewReaderSize(os.Stdin, 256)
	for {
		line, readErr := in.ReadSlice('\n')
		if readErr != nil {
			return readErr
		}
		switch string(line) {
		case "STOP\n":
			return nil
		case "DROP\n":
			if err = memory.Close(); err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "DROPPED")
		case "CHECK\n":
			lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(io.SeekStart), Start: 127, Len: 1}
			err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock)
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) {
				fmt.Fprintln(os.Stdout, "LOCK_BLOCKED")
				continue
			}
			if err != nil {
				return err
			}
			lock.Type = unix.F_UNLCK
			_ = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock)
			fmt.Fprintln(os.Stdout, "LOCK_ACQUIRED")
		default:
			return ErrWALIndexUnsafe
		}
	}
}

func TestWALIndexMemoryRechecksAuthorityRootAndLimits(t *testing.T) {
	ctx, root, protected, lease, state, _ := stateObservationFixture(t)
	authorized := true
	authorize := func(context.Context) error {
		if !authorized {
			return errors.New("revoked")
		}
		return nil
	}
	for _, size := range []int64{0, walIndexPage - 1, walIndexPage + 1, walIndexMaxBytes + walIndexPage} {
		if got, err := NewWALIndexMemory(ctx, root, []*paths.Root{protected}, lease, "catalog", size, authorize); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
			t.Fatalf("invalid size %d accepted: %v", size, err)
		}
	}
	if got, err := NewWALIndexMemory(ctx, root, []*paths.Root{root}, lease, "catalog", walIndexPage, authorize); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("protected root accepted: %v", err)
	}
	if got, err := NewWALIndexMemory(ctx, protected, []*paths.Root{root}, lease, "catalog", walIndexPage, authorize); got != nil || !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("lease bound to another root accepted: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := NewWALIndexMemory(canceled, root, []*paths.Root{protected}, lease, "catalog", walIndexPage, authorize); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
	memory, err := NewWALIndexMemory(ctx, root, []*paths.Root{protected}, lease, "catalog", walIndexPage, authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	mapping, err := memory.Map(ctx, 0, walIndexPage)
	if err != nil {
		t.Fatal(err)
	}
	defer mapping.Close()
	use, err := memory.Hold(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer use.Close()
	if got, mapErr := memory.Map(canceled, 0, walIndexPage); got != nil || !errors.Is(mapErr, context.Canceled) {
		t.Fatalf("canceled mapping accepted: %v", mapErr)
	}
	if user, holdErr := memory.Hold(canceled); user != nil || !errors.Is(holdErr, context.Canceled) {
		t.Fatalf("canceled consumer accepted: %v", holdErr)
	}
	authorized = false
	if err = memory.Recheck(ctx); !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("authority revocation not detected: %v", err)
	}
	if mapping, mapErr := memory.Map(ctx, 0, walIndexPage); mapping != nil || !errors.Is(mapErr, ErrWALIndexUnsafe) {
		t.Fatalf("revoked authority accepted mapping: %v", mapErr)
	}
	if user, holdErr := memory.Hold(ctx); user != nil || !errors.Is(holdErr, ErrWALIndexUnsafe) {
		t.Fatalf("revoked authority accepted consumer: %v", holdErr)
	}
	if err = mapping.Close(); err != nil {
		t.Fatalf("revoked authority prevented cleanup: %v", err)
	}
	if err = use.Close(); err != nil {
		t.Fatalf("revoked authority prevented consumer cleanup: %v", err)
	}
	authorized = true
	if err = os.Rename(state, state+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err = memory.Recheck(ctx); !errors.Is(err, ErrWALIndexUnsafe) {
		t.Fatalf("state root move not detected: %v", err)
	}
}

func TestWALIndexMemoryCrossProcessPeer(t *testing.T) {
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	for _, dir := range []string{state, target} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(random[:])
	if err := os.WriteFile(filepath.Join(state, "wal-memory.owner"), []byte(nonce), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	protected, err := paths.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	defer protected.Close()
	lease, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	memory, err := NewWALIndexMemory(ctx, root, []*paths.Root{protected}, lease, "catalog", walIndexPage, allowState)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	peer, err := memory.Peer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := memory.Map(ctx, 0, walIndexPage)
	if err != nil {
		t.Fatal(err)
	}
	defer mapping.Close()
	data, err := mapping.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	copy(data[:8], "memfd-ok")
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(io.SeekStart), Start: 127, Len: 1}
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	if duplicate, dupErr := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, allowWALPeer); duplicate != nil || !errors.Is(dupErr, ErrWALIndexUnsafe) {
		t.Fatalf("same-process duplicate accepted while lock held: %v", dupErr)
	}
	defer func() {
		lock.Type = unix.F_UNLCK
		_ = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock)
	}()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWALIndexMemoryPeerProcess$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_MEMFD_ORACLE_CHILD=1", "MCE4D_MEMFD_ORACLE_STATE="+state, "MCE4D_MEMFD_ORACLE_TARGET="+target, "MCE4D_MEMFD_ORACLE_NONCE="+nonce)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	if _, err = fmt.Fprintf(stdin, "%d %d %d %d %d %d\n", peer.PID, peer.StartTime, peer.FD, peer.Device, peer.Inode, peer.Size); err != nil {
		t.Fatal(err)
	}
	output := bufio.NewReaderSize(stdout, 256)
	line, err := output.ReadString('\n')
	if err != nil || line != "LOCK_BLOCKED\n" {
		t.Fatalf("peer did not share the expected lock: %q %v", line, err)
	}
	lock.Type = unix.F_UNLCK
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(stdin, "RELEASED"); err != nil {
		t.Fatal(err)
	}
	line, err = output.ReadString('\n')
	if err != nil || line != "SHARED_OK\n" || string(data[:8]) != "child-ok" {
		t.Fatalf("peer memory transfer failed: %q %v", line, err)
	}
	if err = stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("peer process failed: %v", err)
	}
}

// The child only opens a temp fixture carrying a random parent-owned marker.
func TestWALIndexMemoryPeerProcess(t *testing.T) {
	if os.Getenv("MCE4D_MEMFD_ORACLE_CHILD") != "1" {
		return
	}
	if err := runWALIndexMemoryPeerProcess(); err != nil {
		fmt.Fprintln(os.Stdout, "ERROR")
	}
}

func runWALIndexMemoryPeerProcess() error {
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
	in := bufio.NewReaderSize(os.Stdin, 256)
	raw, err := in.ReadSlice('\n')
	if err != nil || len(raw) > 255 {
		return ErrWALIndexUnsafe
	}
	line := string(raw)
	var peer WALIndexPeer
	n, err := fmt.Sscanf(line, "%d %d %d %d %d %d", &peer.PID, &peer.StartTime, &peer.FD, &peer.Device, &peer.Inode, &peer.Size)
	if err != nil || n != 6 {
		return ErrWALIndexUnsafe
	}
	memory, err := OpenWALIndexPeer(ctx, root, []*paths.Root{protected}, lease, "catalog", peer, allowState, allowWALPeer)
	if err != nil {
		return err
	}
	defer memory.Close()
	mapping, err := memory.Map(ctx, 0, walIndexPage)
	if err != nil {
		return err
	}
	defer mapping.Close()
	data, err := mapping.Bytes()
	if err != nil {
		return err
	}
	if string(data[:8]) != "memfd-ok" {
		return ErrWALIndexUnsafe
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(io.SeekStart), Start: 127, Len: 1}
	err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock)
	if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
		return ErrWALIndexUnsafe
	}
	fmt.Fprintln(os.Stdout, "LOCK_BLOCKED")
	raw, err = in.ReadSlice('\n')
	if err != nil || string(raw) != "RELEASED\n" {
		return ErrWALIndexUnsafe
	}
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		return err
	}
	copy(data[:8], "child-ok")
	lock.Type = unix.F_UNLCK
	if err = unix.FcntlFlock(memory.file.Fd(), unix.F_SETLK, &lock); err != nil {
		return err
	}
	if err = memory.Recheck(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "SHARED_OK")
	return nil
}

func openWALIndexOracleFixture() (*paths.Root, *paths.Root, error) {
	state := os.Getenv("MCE4D_MEMFD_ORACLE_STATE")
	target := os.Getenv("MCE4D_MEMFD_ORACLE_TARGET")
	nonce := os.Getenv("MCE4D_MEMFD_ORACLE_NONCE")
	decoded, err := hex.DecodeString(nonce)
	stateRel, stateErr := filepath.Rel(os.TempDir(), state)
	targetRel, targetErr := filepath.Rel(os.TempDir(), target)
	if err != nil || len(decoded) != 16 || stateErr != nil || targetErr != nil || !filepath.IsAbs(state) || !filepath.IsAbs(target) || filepath.Base(state) != "state" || filepath.Base(target) != "target" || filepath.Dir(state) != filepath.Dir(target) || stateRel == ".." || targetRel == ".." || strings.HasPrefix(stateRel, ".."+string(filepath.Separator)) || strings.HasPrefix(targetRel, ".."+string(filepath.Separator)) {
		return nil, nil, ErrWALIndexUnsafe
	}
	root, err := paths.OpenRoot(state)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*paths.Root, *paths.Root, error) {
		return nil, nil, errors.Join(err, root.Close())
	}
	marker, err := root.OpenRegular("wal-memory.owner")
	if err != nil {
		return fail(err)
	}
	if stateOwner(marker, false) != nil {
		_ = marker.Close()
		return fail(ErrWALIndexUnsafe)
	}
	contents, readErr := io.ReadAll(io.LimitReader(marker, 33))
	closeErr := marker.Close()
	if readErr != nil || closeErr != nil || string(contents) != nonce {
		return fail(ErrWALIndexUnsafe)
	}
	protected, err := paths.OpenRoot(target)
	if err != nil {
		return fail(err)
	}
	return root, protected, nil
}
