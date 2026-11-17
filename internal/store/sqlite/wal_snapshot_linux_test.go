//go:build linux

package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// The child commits to a real SQLite WAL and waits without closing SQLite;
// its parent terminates it to leave an uncheckpointed, committed fixture.
func TestWALSnapshotWriterChild(t *testing.T) {
	if os.Getenv("MCE4D_WAL_SNAPSHOT_CHILD") != "1" {
		return
	}
	root, protected, err := openWALIndexOracleFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	defer protected.Close()
	state := os.Getenv("MCE4D_MEMFD_ORACLE_STATE")
	pageSize, err := strconv.Atoi(os.Getenv("MCE4D_WAL_SNAPSHOT_PAGE_SIZE"))
	if err != nil || pageSize != 512 && pageSize != 1024 && pageSize != 4096 && pageSize != 65536 {
		t.Fatal("unsupported fixture page size")
	}
	db, err := sql.Open("sqlite", walOracleDSN(filepath.Join(state, "catalog.sqlite"), "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(fmt.Sprintf("PRAGMA page_size=%d", pageSize)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE fixture(n INTEGER NOT NULL); INSERT INTO fixture VALUES(7)"); err != nil {
		t.Fatal(err)
	}
	var busy, frames, checkpointed, pages int
	if err = db.QueryRow("PRAGMA main.wal_checkpoint(NOOP)").Scan(&busy, &frames, &checkpointed); err != nil || busy != 0 || frames < 1 || checkpointed != 0 {
		t.Fatalf("writer commit boundary: busy=%d frames=%d checkpointed=%d err=%v", busy, frames, checkpointed, err)
	}
	if err = db.QueryRow("PRAGMA main.page_count").Scan(&pages); err != nil || pages < 1 {
		t.Fatalf("writer page count: %d %v", pages, err)
	}
	if os.Getenv("MCE4D_WAL_SNAPSHOT_UNCOMMITTED") == "1" {
		// Force dirty pages into WAL and terminate before the transaction commits.
		if _, err = db.Exec("PRAGMA cache_size=10; BEGIN IMMEDIATE; WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<30000) INSERT INTO fixture SELECT x+7 FROM n"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = fmt.Fprintf(os.Stdout, "COMMITTED %d %d\n", frames, pages); err != nil {
		t.Fatal(err)
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

type walSnapshotFixture struct {
	ctx       context.Context
	root      *paths.Root
	protected *paths.Root
	lease     *runtime.Lifecycle
	state     string
	target    string
	pair      WALSnapshotPair
}

func newWALSnapshotFixture(t *testing.T) walSnapshotFixture {
	return newWALSnapshotFixtureWithPageSize(t, 4096)
}

func newWALSnapshotFixtureWithPageSize(t *testing.T, pageSize int) walSnapshotFixture {
	return newWALSnapshotFixtureWithOptions(t, pageSize, false)
}

func newWALSnapshotFixtureWithOptions(t *testing.T, pageSize int, uncommitted bool) walSnapshotFixture {
	t.Helper()
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	for _, dir := range []string{state, target} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var nonceBytes [16]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(nonceBytes[:])
	if err := os.WriteFile(filepath.Join(state, "wal-memory.owner"), []byte(nonce), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "catalog.sqlite"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWALSnapshotWriterChild$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_WAL_SNAPSHOT_CHILD=1", "MCE4D_MEMFD_ORACLE_STATE="+state, "MCE4D_MEMFD_ORACLE_TARGET="+target, "MCE4D_MEMFD_ORACLE_NONCE="+nonce, "MCE4D_WAL_SNAPSHOT_PAGE_SIZE="+strconv.Itoa(pageSize))
	if uncommitted {
		cmd.Env = append(cmd.Env, "MCE4D_WAL_SNAPSHOT_UNCOMMITTED=1")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = stdin.Close()
	})
	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	fields := strings.Fields(line)
	if readErr != nil || len(fields) != 3 || fields[0] != "COMMITTED" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		waited = true
		t.Fatalf("WAL writer did not commit: %q %v %s", line, readErr, stderr.String())
	}
	frames, frameErr := strconv.ParseUint(fields[1], 10, 32)
	pages, pageErr := strconv.ParseUint(fields[2], 10, 32)
	if frameErr != nil || pageErr != nil || frames == 0 || pages == 0 {
		t.Fatalf("invalid writer boundary: %q", line)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait() // Termination intentionally leaves the SQLite connection unclosed.
	waited = true
	_ = stdin.Close()

	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := paths.OpenRoot(target)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	lease, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.ExclusiveLease, time.Second)
	if err != nil {
		_ = protected.Close()
		_ = root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(); _ = protected.Close(); _ = root.Close() })
	mainBytes, err := os.ReadFile(filepath.Join(state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	walBytes, err := os.ReadFile(filepath.Join(state, "catalog.sqlite-wal"))
	if err != nil {
		t.Fatal(err)
	}
	return walSnapshotFixture{ctx, root, protected, lease, state, target, WALSnapshotPair{
		MainSize: int64(len(mainBytes)), WALSize: int64(len(walBytes)),
		MainSHA256: sha256.Sum256(mainBytes), WALSHA256: sha256.Sum256(walBytes),
		WALCommitFrame: uint32(frames), WALCommitPages: uint32(pages),
	}}
}

// The expected pair is a controlled test oracle captured after the fixture's
// commit. It is not an independent authority for a production state store.
func (f walSnapshotFixture) proof(_ context.Context, pair WALSnapshotPair) error {
	if pair != f.pair {
		return errors.New("fixture pair differs from recorded commit")
	}
	return nil
}

func (f walSnapshotFixture) open(proof func(context.Context, WALSnapshotPair) error) (*WALSnapshot, error) {
	return OpenWALSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 1<<20, allowState, proof)
}

func TestWALSnapshotRecoversCommittedFixture(t *testing.T) {
	f := newWALSnapshotFixture(t)
	beforeMain, _ := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	beforeWAL, _ := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	beforeSHM, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-shm"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.open(f.proof)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snapshot.Close() })
	if err = f.lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("snapshot did not retain lifecycle: %v", err)
	}
	// Force replacement physical connections; each must enter exclusive mode
	// before SQLite accesses the WAL through the read-only VFS.
	snapshot.db.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
			rows, queryErr := q.Query("SELECT n FROM fixture")
			if queryErr != nil {
				return queryErr
			}
			if !rows.Next() {
				return errors.New("committed WAL row missing")
			}
			var value int
			if scanErr := rows.Scan(&value); scanErr != nil || value != 7 {
				return fmt.Errorf("committed WAL value = %d: %v", value, scanErr)
			}
			if rows.Next() {
				return errors.New("unexpected second row")
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("snapshot read %d: %v", i, err)
		}
	}
	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.lease.Close(); err != nil {
		t.Fatalf("snapshot did not release lifecycle: %v", err)
	}
	for name, before := range map[string][]byte{"catalog.sqlite": beforeMain, "catalog.sqlite-wal": beforeWAL, "catalog.sqlite-shm": beforeSHM} {
		after, readErr := os.ReadFile(filepath.Join(f.state, name))
		if readErr != nil || !bytes.Equal(before, after) {
			t.Fatalf("snapshot modified %s: %v", name, readErr)
		}
	}
	if err = snapshot.Read(f.ctx, func(*SnapshotQuery) error { return nil }); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("closed snapshot accepted read: %v", err)
	}
}

func TestWALSnapshotRejectsChangedCommittedPair(t *testing.T) {
	f := newWALSnapshotFixture(t)
	snapshot, err := f.open(f.proof)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	path := filepath.Join(f.state, "catalog.sqlite-wal")
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 33 {
		t.Fatalf("fixture WAL: %v", err)
	}
	data[len(data)-1] ^= 1
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	err = snapshot.Read(f.ctx, func(*SnapshotQuery) error { called = true; return nil })
	if !errors.Is(err, ErrStateChanged) || called {
		t.Fatalf("changed WAL accepted: called=%t err=%v", called, err)
	}
}

func TestWALSnapshotDetectsDamagedCommittedFrame(t *testing.T) {
	f := newWALSnapshotFixture(t)
	path := filepath.Join(f.state, "catalog.sqlite-wal")
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 33 {
		t.Fatalf("fixture WAL: %v", err)
	}
	data[len(data)-1] ^= 1
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	// Even if an observer records the current damaged bytes, the boundary
	// obtained independently from the writer must not move backwards.
	f.pair.WALSHA256 = sha256.Sum256(data)
	if snapshot, err := f.open(f.proof); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("damaged committed frame accepted: %v", err)
	}
	if err := f.lease.Close(); err != nil {
		t.Fatalf("failed open retained lifecycle: %v", err)
	}
}

func TestWALSnapshotStopsAfterStateMove(t *testing.T) {
	f := newWALSnapshotFixture(t)
	snapshot, err := f.open(f.proof)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	moved := filepath.Join(f.target, "moved")
	if err = os.Rename(f.state, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if restoreErr := os.Rename(moved, f.state); restoreErr != nil {
			t.Errorf("restore fixture path: %v", restoreErr)
		}
	})
	before, err := os.ReadFile(filepath.Join(moved, "catalog.sqlite-wal"))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = snapshot.Read(f.ctx, func(*SnapshotQuery) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("moved state accepted: called=%t err=%v", called, err)
	}
	after, err := os.ReadFile(filepath.Join(moved, "catalog.sqlite-wal"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read changed moved WAL: %v", err)
	}
}

func TestWALSnapshotReportsStateMoveDuringQuery(t *testing.T) {
	f := newWALSnapshotFixture(t)
	snapshot, err := f.open(f.proof)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	moved := filepath.Join(f.target, "moved")
	movedState := false
	t.Cleanup(func() {
		if movedState {
			if restoreErr := os.Rename(moved, f.state); restoreErr != nil {
				t.Errorf("restore fixture path: %v", restoreErr)
			}
		}
	})
	var value int
	err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		rows, queryErr := q.Query("SELECT n FROM fixture")
		if queryErr != nil {
			return queryErr
		}
		if !rows.Next() {
			return errors.New("committed WAL row missing")
		}
		if scanErr := rows.Scan(&value); scanErr != nil {
			return scanErr
		}
		if moveErr := os.Rename(f.state, moved); moveErr != nil {
			return moveErr
		}
		movedState = true
		return nil
	})
	if err == nil || !movedState || value != 7 {
		t.Fatalf("state move during read was not detected: value=%d moved=%t err=%v", value, movedState, err)
	}
}

func TestWALSnapshotRejectsLateHardlinks(t *testing.T) {
	for _, name := range []string{"catalog.sqlite", "catalog.sqlite-wal"} {
		t.Run(name, func(t *testing.T) {
			f := newWALSnapshotFixture(t)
			snapshot, err := f.open(f.proof)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			original := filepath.Join(f.state, name)
			before, err := os.ReadFile(original)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(f.target, "alias")
			called := false
			err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
				rows, queryErr := q.Query("SELECT n FROM fixture")
				if queryErr != nil {
					return queryErr
				}
				if !rows.Next() {
					return errors.New("committed WAL row missing")
				}
				if linkErr := os.Link(original, alias); linkErr != nil {
					return linkErr
				}
				called = true
				return nil
			})
			if err == nil || !called {
				t.Fatalf("late hardlink not detected: called=%t err=%v", called, err)
			}
			after, err := os.ReadFile(alias)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("snapshot changed fixture alias: %v", err)
			}
		})
	}
}

func TestWALSnapshotRequiresRecordedPair(t *testing.T) {
	f := newWALSnapshotFixture(t)
	if snapshot, err := f.open(nil); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("missing proof accepted: %v", err)
	}
	if snapshot, err := f.open(func(context.Context, WALSnapshotPair) error { return errors.New("proof unavailable") }); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("unavailable proof accepted: %v", err)
	}
	if err := f.lease.Close(); err != nil {
		t.Fatalf("failed open retained lifecycle: %v", err)
	}
}

func TestWALSnapshotLimitsAndCancellation(t *testing.T) {
	f := newWALSnapshotFixture(t)
	if snapshot, err := OpenWALSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 64, allowState, f.proof); snapshot != nil || !errors.Is(err, ErrStateLimit) {
		t.Fatalf("oversized fixture accepted: %v", err)
	}
	snapshot, err := f.open(f.proof)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	called := false
	if err = snapshot.Read(ctx, func(*SnapshotQuery) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancelled read accepted: called=%t err=%v", called, err)
	}
	if err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		rows, queryErr := q.Query("SELECT n FROM fixture")
		if queryErr != nil {
			return queryErr
		}
		if !rows.Next() {
			return errors.New("row missing after cancellation")
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("snapshot unusable after cancellation: %v", err)
	}
}
