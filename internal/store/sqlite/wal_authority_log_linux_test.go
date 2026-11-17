//go:build linux

package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

type authorityLogFixture struct {
	t        *testing.T
	ctx      context.Context
	state    string
	target   string
	root     *paths.Root
	protect  *paths.Root
	identity walAuthorityIdentity
	key      [32]byte
	image    walLogicalImage
}

func newAuthorityLogFixture(t *testing.T) *authorityLogFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	f := &authorityLogFixture{t: t, ctx: ctx, state: t.TempDir(), target: t.TempDir(),
		key:      sha256.Sum256([]byte("synthetic receipt key, never a user secret")),
		identity: walAuthorityIdentity{Authority: [16]byte{1}, Store: sha256.Sum256([]byte("catalog fixture")), Incarnation: [16]byte{2}},
		image:    walLogicalImage{PageSize: 4096, PageCount: 2, SHA256: sha256.Sum256([]byte("synthetic initial image"))}}
	if err := os.Chmod(f.state, 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	f.root, err = paths.OpenRoot(f.state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.root.Close() })
	f.protect, err = paths.OpenRoot(f.target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.protect.Close() })
	file, err := os.OpenFile(filepath.Join(f.state, walAuthorityName), os.O_CREATE|os.O_EXCL|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *authorityLogFixture) open(initialize *walLogicalImage, cap int64) (*walAuthorityLog, error) {
	f.t.Helper()
	file, err := os.OpenFile(filepath.Join(f.state, walAuthorityName), os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	l, err := openWALAuthorityLog(f.ctx, f.root, []*paths.Root{f.protect}, file, f.identity, f.key, cap, initialize)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	f.t.Cleanup(func() { _ = l.Close() })
	return l, nil
}

func authorityFixtureCommit() (walLogicalImage, walCommitPrefixReceipt) {
	image := walLogicalImage{PageSize: 4096, PageCount: 3, SHA256: sha256.Sum256([]byte("synthetic committed image"))}
	prefix := walCommitPrefixReceipt{MainSize: 8192, MainSHA256: sha256.Sum256([]byte("synthetic main")),
		WALPrefixSize: 32 + 3*(4096+24), WALPrefixSHA256: sha256.Sum256([]byte("synthetic WAL")), WALCommitFrame: 3, WALCommitPages: 3}
	return image, prefix
}

func TestWALAuthorityLogReceiptReopenAndPending(t *testing.T) {
	f := newAuthorityLogFixture(t)
	l, err := f.open(&f.image, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(nil, walAuthorityMaxBytes); !errors.Is(err, errWALAuthorityBusy) {
		t.Fatalf("second authority owner: %v", err)
	}
	op := sha256.Sum256([]byte("transaction one"))
	image, prefix := authorityFixtureCommit()
	if err := l.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if head, err := l.Head(f.ctx); !errors.Is(err, errWALAuthorityPending) || head != (walAuthorityReceipt{}) {
		t.Fatalf("pending transaction exposed old head: %+v %v", head, err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = f.open(nil, walAuthorityMaxBytes)
	if err != nil || l.pending == nil || l.pending.Operation != op {
		t.Fatalf("pending identity lost on reopen: %v", err)
	}
	if err := l.Commit(f.ctx, sha256.Sum256([]byte("wrong operation")), image, prefix); err == nil {
		t.Fatal("resolved the wrong operation")
	}
	if err := l.Abort(f.ctx, op, image); err == nil {
		t.Fatal("rollback accepted changed image")
	}
	if err := l.Commit(f.ctx, op, image, prefix); err != nil {
		t.Fatal(err)
	}
	if err := l.Commit(f.ctx, op, image, prefix); err == nil {
		t.Fatal("duplicate finalization appended")
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = f.open(nil, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	want := walAuthorityReceipt{Sequence: 1, Operation: op, Image: image, Prefix: prefix}
	if got, err := l.Head(f.ctx); err != nil || got != want {
		t.Fatalf("persisted receipt mismatch: %+v %v", got, err)
	}
	op2 := sha256.Sum256([]byte("transaction two, independently proved rollback"))
	if err := l.Prepare(f.ctx, op2); err != nil {
		t.Fatal(err)
	}
	if err := l.Abort(f.ctx, op2, image); err != nil {
		t.Fatal(err)
	}
	want.Sequence, want.Operation = 2, op2
	if got, err := l.Head(f.ctx); err != nil || got != want {
		t.Fatalf("rollback changed image or reused sequence: %+v %v", got, err)
	}
}

func TestWALAuthorityLogRejectsCorruptionAndIdentity(t *testing.T) {
	f := newAuthorityLogFixture(t)
	l, err := f.open(&f.image, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	op := sha256.Sum256([]byte("corruption fixture"))
	image, prefix := authorityFixtureCommit()
	if err := l.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := l.Commit(f.ctx, op, image, prefix); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.state, walAuthorityName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func([]byte) []byte{
		"partial_record": func(b []byte) []byte { return b[:len(b)-1] },
		"extra_tail":     func(b []byte) []byte { return append(b, 1) },
		"wrong_mac":      func(b []byte) []byte { b[1000] ^= 1; return b },
		"dropped_prepare": func(b []byte) []byte {
			return append(b[:walAuthorityRecordSize:walAuthorityRecordSize], b[2*walAuthorityRecordSize:]...)
		},
		"reordered": func(b []byte) []byte {
			first := append([]byte(nil), b[walAuthorityRecordSize:2*walAuthorityRecordSize]...)
			copy(b[walAuthorityRecordSize:], b[2*walAuthorityRecordSize:])
			copy(b[2*walAuthorityRecordSize:], first)
			return b
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := mutate(append([]byte(nil), raw...))
			if err := os.WriteFile(path, bad, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.open(nil, walAuthorityMaxBytes); !errors.Is(err, errWALAuthorityRecovery) {
				t.Fatalf("accepted corrupt log: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, bad) {
				t.Fatal("recovery altered evidence")
			}
		})
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"key", "authority", "store", "incarnation"} {
		t.Run(field, func(t *testing.T) {
			oldID, oldKey := f.identity, f.key
			switch field {
			case "key":
				f.key[0] ^= 1
			case "authority":
				f.identity.Authority[0] ^= 0x80
			case "store":
				f.identity.Store[0] ^= 1
			case "incarnation":
				f.identity.Incarnation[0] ^= 1
			}
			_, err := f.open(nil, walAuthorityMaxBytes)
			f.identity, f.key = oldID, oldKey
			if !errors.Is(err, errWALAuthorityRecovery) {
				t.Fatalf("accepted unrelated authority: %v", err)
			}
		})
	}
	if _, err := f.open(&f.image, walAuthorityMaxBytes); err == nil {
		t.Fatal("reinitialized existing authority")
	}
}

func TestWALAuthorityLogMoveHardlinkCancelAndCapacity(t *testing.T) {
	f := newAuthorityLogFixture(t)
	if _, err := f.open(nil, walAuthorityMaxBytes); !errors.Is(err, errWALAuthorityRecovery) {
		t.Fatalf("implicitly initialized empty authority: %v", err)
	}
	l, err := f.open(&f.image, 3*walAuthorityRecordSize)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	op := sha256.Sum256([]byte("bounded transaction"))
	if err := l.Prepare(ctx, op); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled operation: %v", err)
	}
	path := filepath.Join(f.state, walAuthorityName)
	link := filepath.Join(f.target, "linked-authority")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Prepare(f.ctx, op); err == nil {
		t.Fatal("appended to already hardlinked authority")
	}
	after, err := os.ReadFile(link)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("hardlink rejection modified target")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := l.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := l.Abort(f.ctx, op, f.image); err != nil {
		t.Fatal(err)
	}
	if err := l.Prepare(f.ctx, op); !errors.Is(err, ErrStateLimit) {
		t.Fatalf("did not reserve resolution space: %v", err)
	}
	// A root moved BEFORE an operation is refused without modifying it. This
	// test does not establish protection against concurrent same-user moves.
	moved := filepath.Join(f.target, "moved-authority")
	if err := os.Rename(f.state, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(moved, f.state) })
	if got, err := l.Head(f.ctx); err == nil || got != (walAuthorityReceipt{}) {
		t.Fatalf("served moved authority: %+v %v", got, err)
	}
}

func TestWALAuthorityLogBindsRecoveryViewAndRechecksHead(t *testing.T) {
	f := newWALSnapshotFixture(t)
	main, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := fixturePrefixReceipt(t, main, wal, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	image, err := digestCommittedWALImage(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), prefix, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	authority := newAuthorityLogFixture(t)
	l, err := authority.open(&authority.image, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	op := sha256.Sum256([]byte("SQLite commit proof fixture"))
	if err := l.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := l.Commit(f.ctx, op, image, prefix); err != nil {
		t.Fatal(err)
	}
	proof, err := l.proveWALCapture(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := proof.VerifyWAL(f.ctx, f.pair); err != nil {
		t.Fatal(err)
	}
	badPair := f.pair
	badPair.WALSHA256[0] ^= 1
	if err := proof.VerifyWAL(f.ctx, badPair); err == nil {
		t.Fatal("proof accepted another WAL pair")
	}
	if _, err := l.proveWALCapture(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal[:len(wal)-1]), int64(len(wal)-1), 64<<20); err == nil {
		t.Fatal("receipt accepted lost committed WAL byte")
	}
	snapshot, err := f.open(proof.VerifyWAL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snapshot.Close() })
	var got int
	if err := snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		rows, err := q.Query("SELECT n FROM fixture")
		if err != nil {
			return err
		}
		if !rows.Next() {
			return errors.New("acknowledged WAL row missing")
		}
		return rows.Scan(&got)
	}); err != nil || got != 7 {
		t.Fatalf("receipt-backed read: %d %v", got, err)
	}
	op2 := sha256.Sum256([]byte("next pending SQLite transaction"))
	if err := l.Prepare(f.ctx, op2); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := snapshot.Read(f.ctx, func(*SnapshotQuery) error { called = true; return nil }); err == nil || called {
		t.Fatalf("old receipt served during pending transaction: called=%v err=%v", called, err)
	}
	if err := l.Abort(f.ctx, op2, image); err != nil {
		t.Fatal(err)
	}
	if err := proof.VerifyWAL(f.ctx, f.pair); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("old authority sequence became valid again: %v", err)
	}
	if _, err := l.proveWALCapture(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), 64<<20); err != nil {
		t.Fatalf("fresh proof of unchanged acknowledged image: %v", err)
	}
}

func TestWALAuthorityLogPartialWriteChild(t *testing.T) {
	if os.Getenv("MCE4D_AUTHORITY_PARTIAL_CHILD") != "1" {
		return
	}
	f := newAuthorityLogFixture(t)
	l, err := f.open(&f.image, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	op := sha256.Sum256([]byte("partial write"))
	if err := l.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	var old unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &old); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: uint64(2*walAuthorityRecordSize + 123), Max: old.Max}); err != nil {
		t.Fatal(err)
	}
	image, prefix := authorityFixtureCommit()
	err = l.Commit(f.ctx, op, image, prefix)
	if resetErr := unix.Setrlimit(unix.RLIMIT_FSIZE, &old); resetErr != nil {
		t.Fatal(resetErr)
	}
	if !errors.Is(err, errWALAuthorityRecovery) || !l.poisoned {
		t.Fatalf("partial append did not poison owner: %v", err)
	}
	if _, err := l.Head(f.ctx); !errors.Is(err, errWALAuthorityRecovery) {
		t.Fatalf("served receipt after ambiguous append: %v", err)
	}
	info, err := os.Stat(filepath.Join(f.state, walAuthorityName))
	if err != nil || info.Size() != 2*walAuthorityRecordSize+123 {
		t.Fatalf("partial write was not exercised: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(nil, walAuthorityMaxBytes); !errors.Is(err, errWALAuthorityRecovery) {
		t.Fatalf("reopen fell back to older receipt: %v", err)
	}
}

func TestWALAuthorityLogPartialWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWALAuthorityLogPartialWriteChild$", "-test.v", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_AUTHORITY_PARTIAL_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("partial-write child: %v %s", err, out)
	}
}

func TestWALAuthorityLogCrashChild(t *testing.T) {
	if os.Getenv("MCE4D_AUTHORITY_CRASH_CHILD") != "1" {
		return
	}
	f := newAuthorityLogFixture(t)
	l, err := f.open(&f.image, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	op := sha256.Sum256([]byte("killed pending transaction"))
	if err := l.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	phase := os.Getenv("MCE4D_AUTHORITY_CRASH_PHASE")
	if phase == "commit" {
		image, prefix := authorityFixtureCommit()
		if err := l.Commit(f.ctx, op, image, prefix); err != nil {
			t.Fatal(err)
		}
	} else if phase != "prepare" {
		t.Fatal("invalid crash fixture phase")
	}
	// Parent owns cleanup after SIGKILL; only this child's TempDir is reported.
	fmt.Fprintln(os.Stdout, f.state)
	fmt.Fprintln(os.Stdout, f.target)
	fmt.Fprintln(os.Stdout, phase)
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestWALAuthorityLogCrashRecovery(t *testing.T) {
	for _, phase := range []string{"prepare", "commit"} {
		t.Run(phase, func(t *testing.T) { testAuthorityLogCrash(t, phase) })
	}
}

func testAuthorityLogCrash(t *testing.T, phase string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWALAuthorityLogCrashChild$", "-test.timeout=12s")
	sandbox := t.TempDir()
	cmd.Env = append(os.Environ(), "MCE4D_AUTHORITY_CRASH_CHILD=1", "MCE4D_AUTHORITY_CRASH_PHASE="+phase, "TMPDIR="+sandbox)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		_ = input.Close()
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	reader := bufio.NewReader(out)
	state, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("state path unavailable: %v %s", err, &stderr)
	}
	target, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	state, target = state[:len(state)-1], target[:len(target)-1]
	// The parent owns the sandbox, including cleanup after the killed child.
	for _, path := range []string{state, target} {
		if !filepath.IsAbs(path) || filepath.Dir(filepath.Dir(path)) != sandbox || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "TestWALAuthorityLogCrashChild") {
			t.Fatalf("unexpected child TempDir path %q", path)
		}
	}
	if line, err := reader.ReadString('\n'); err != nil || line != phase+"\n" {
		t.Fatalf("durability acknowledgement: %q %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waited = true
	f := newAuthorityLogFixture(t)
	_ = f.root.Close()
	_ = f.protect.Close()
	f.state, f.target = state, target
	f.root, err = paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	f.protect, err = paths.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	l, err := f.open(nil, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	got, err := l.Head(ctx)
	if phase == "prepare" {
		if !errors.Is(err, errWALAuthorityPending) || got != (walAuthorityReceipt{}) {
			t.Fatalf("crash was mistaken for rollback: %+v %v", got, err)
		}
	} else {
		image, prefix := authorityFixtureCommit()
		want := walAuthorityReceipt{Sequence: 1, Operation: sha256.Sum256([]byte("killed pending transaction")), Image: image, Prefix: prefix}
		if err != nil || got != want {
			t.Fatalf("acknowledged receipt lost after crash: %+v %v", got, err)
		}
	}
}
