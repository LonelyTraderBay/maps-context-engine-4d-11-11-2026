//go:build linux

package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func TestStateDomainRejectsOrdinaryUserState(t *testing.T) {
	f := newAuthorityLogFixture(t)
	if d, err := openStateDomain(f.ctx, f.root, []*paths.Root{f.protect}, uint32(unix.Geteuid())); err == nil || d != nil {
		t.Fatal("accepted a shared engine/client UID")
	}
	if d, err := openStateDomain(f.ctx, f.root, []*paths.Root{f.protect}, 12345); err == nil || d != nil {
		t.Fatal("accepted user-controlled state ancestors/namespace")
	}
	file, err := os.OpenFile(filepath.Join(f.state, walAuthorityName), os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openWALHeadAuthority(f.ctx, f.root, []*paths.Root{f.protect}, file, f.identity, f.key); err == nil {
		t.Fatal("accepted a mutable head under user-controlled ancestors")
	}
	_ = file.Close()
	if info, err := os.Stat(filepath.Join(f.state, walAuthorityName)); err != nil || info.Size() != 0 {
		t.Fatal("negative domain check wrote state")
	}
}

func TestStateDomainPlatformFilesystemFixture(t *testing.T) {
	native, other := os.Getenv("MCE4D_PLATFORM_EXT4_FIXTURE"), os.Getenv("MCE4D_PLATFORM_DRVFS_FIXTURE")
	if native == "" && other == "" {
		t.Skip("requires explicit Linux and DrvFS fixture directories")
	}
	if !filepath.IsAbs(native) || !filepath.IsAbs(other) || native == other || os.Geteuid() != 0 {
		t.Fatal("fixture requires two absolute directories and a root-owned Linux control")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name, path string
		ext4       bool
		want       error
	}{
		{"ext4", native, true, nil},
		{"drvfs", other, false, ErrStateUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fs unix.Statfs_t
			if err := unix.Statfs(tc.path, &fs); err != nil {
				t.Fatal(err)
			}
			if (uint64(fs.Type) == uint64(unix.EXT4_SUPER_MAGIC)) != tc.ext4 {
				t.Fatalf("wrong fixture filesystem: %#x", fs.Type)
			}
			root, err := paths.OpenRoot(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := rootManagedAncestors(ctx, root, true); !errors.Is(err, tc.want) {
				t.Fatalf("filesystem %#x: got %v, want %v", fs.Type, err, tc.want)
			}
			t.Logf("kernel filesystem magic %#x", fs.Type)
		})
	}
}

const protectedFixtureStatePath = "/managed/home/.local/state/mce4d"

type protectedKernelFixture struct {
	ctx                    context.Context
	state, target, control *paths.Root
	domain                 *stateDomain
	identity               walAuthorityIdentity
	key                    [32]byte
}

// These helpers run only in the bounded root-owned synthetic chroot prepared
// by the local evidence harness. Normal go test does not create users/mounts,
// change sysctls or require privileged setup. Positive proof is recorded from
// that harness separately; returning here is not a positive integration test.
func openProtectedKernelFixture(t *testing.T) protectedKernelFixture {
	t.Helper()
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		t.Fatal("protected fixture was not explicitly provisioned")
	}
	if filepath.Join(os.Getenv("HOME"), ".local", "state", "mce4d") != protectedFixtureStatePath || os.Getenv("XDG_STATE_HOME") != "" {
		t.Fatal("fixture does not use the S05 default home layout")
	}
	marker, err := os.ReadFile("/fixture.owner")
	if err != nil || string(marker) != "mce4d-protected-kernel-fixture-v1\n" {
		t.Fatal("unrecognized synthetic chroot")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	f := protectedKernelFixture{ctx: ctx, identity: walAuthorityIdentity{Authority: [16]byte{3}, Store: sha256.Sum256([]byte("protected fixture catalog")), Incarnation: [16]byte{4}}, key: sha256.Sum256([]byte("synthetic protected fixture key"))}
	f.state, err = paths.OpenRoot(protectedFixtureStatePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.state.Close() })
	f.target, err = paths.OpenRoot("/target")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.target.Close() })
	f.control, err = paths.OpenRoot("/authority")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.control.Close() })
	f.domain, err = openStateDomain(ctx, f.state, []*paths.Root{f.target}, 1001)
	if err != nil {
		t.Fatalf("protected state domain: %v", err)
	}
	return f
}

func (f protectedKernelFixture) openLog(t *testing.T, initial *walLogicalImage) (*walAuthorityLog, *walHeadAuthority, error) {
	t.Helper()
	file, err := os.OpenFile("/authority/"+walHeadName, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return nil, nil, err
	}
	head, err := openWALHeadAuthority(f.ctx, f.control, []*paths.Root{f.target, f.state}, file, f.identity, f.key)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	journal, err := os.OpenFile(protectedFixtureStatePath+"/"+walAuthorityName, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		_ = head.Close()
		return nil, nil, err
	}
	log, err := openAnchoredWALAuthorityLog(f.ctx, f.domain, journal, head, f.identity, f.key, walAuthorityMaxBytes, initial)
	if err != nil {
		_ = head.Close()
		return nil, nil, err
	}
	t.Cleanup(func() { _ = log.Close(); _ = head.Close() })
	return log, head, nil
}

type protectedCapture struct {
	Image  walLogicalImage
	Prefix walCommitPrefixReceipt
}

func TestProtectedStateCaptureChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	f := openProtectedKernelFixture(t)
	lease, err := runtime.AcquireLifecycle(f.ctx, f.state, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	main, err := os.ReadFile(protectedFixtureStatePath + "/catalog.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(protectedFixtureStatePath + "/catalog.sqlite-wal")
	if err != nil {
		t.Fatal(err)
	}
	frame, pages, err := walCommittedBoundary(f.ctx, main, wal)
	if err != nil {
		t.Fatal(err)
	}
	prefix := fixturePrefixReceipt(t, main, wal, frame, pages)
	image, err := digestCommittedWALImage(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), prefix, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(protectedCapture{Image: image, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, string(raw))
}

func captureProtectedFixture(t *testing.T, ctx context.Context) protectedCapture {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestProtectedStateCaptureChild$", "-test.timeout=12s")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated capture: %v %s", err, out)
	}
	var result protectedCapture
	line, _, ok := bytes.Cut(out, []byte{'\n'})
	if !ok || json.Unmarshal(line, &result) != nil {
		t.Fatalf("invalid capture output: %s", out)
	}
	return result
}

func TestProtectedStateWriterChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	f := openProtectedKernelFixture(t)
	lease, err := runtime.AcquireLifecycle(f.ctx, f.state, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	observation, err := f.domain.Observe(f.ctx, lease, "catalog", "catalog.sqlite", 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite", walOracleDSN(protectedFixtureStatePath+"/catalog.sqlite", "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	defer writer.Close()
	if _, err = writer.ExecContext(f.ctx, "PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA wal_autocheckpoint=0; CREATE TABLE fixture(n INTEGER NOT NULL); INSERT INTO fixture VALUES(7)"); err != nil {
		t.Fatal(err)
	}
	old := captureProtectedFixture(t, f.ctx)
	log, head, err := f.openLog(t, &old.Image)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", walOracleDSN(protectedFixtureStatePath+"/catalog.sqlite", "unix", "ro"))
	if err != nil {
		t.Fatal(err)
	}
	reader.SetMaxOpenConns(1)
	reader.SetMaxIdleConns(1)
	defer reader.Close()
	readTx, err := reader.BeginTx(f.ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	var count int
	if err := readTx.QueryRowContext(f.ctx, "SELECT count(*) FROM fixture").Scan(&count); err != nil || count != 1 {
		t.Fatalf("initial reader: %d %v", count, err)
	}
	op := sha256.Sum256([]byte("protected fixture transaction"))
	if err := log.Prepare(f.ctx, op); err != nil {
		t.Fatal(err)
	}
	tx, err := writer.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	fmt.Fprintln(os.Stdout, "ATTACK_WINDOW")
	in := bufio.NewReader(os.Stdin)
	if line, err := in.ReadString('\n'); err != nil || line != "attacker-started\n" {
		t.Fatalf("attack barrier: %q %v", line, err)
	}
	if _, err = tx.ExecContext(f.ctx, "INSERT INTO fixture VALUES(8)"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MCE4D_KERNEL_CRASH_BEFORE_RECEIPT") == "1" {
		fmt.Fprintln(os.Stdout, "SQL_COMMITTED_PENDING")
		_, _ = in.ReadString('\n')
		t.Fatal("pending crash fixture unexpectedly resumed")
	}
	after := captureProtectedFixture(t, f.ctx)
	if after.Image == old.Image {
		t.Fatal("writer did not change committed image")
	}
	if err = log.Commit(f.ctx, op, after.Image, after.Prefix); err != nil {
		t.Fatal(err)
	}
	if got, err := log.Head(f.ctx); err != nil || got.Sequence != 1 || got.Image != after.Image {
		t.Fatalf("anchored acknowledgement: %+v %v", got, err)
	}
	if err := readTx.QueryRowContext(f.ctx, "SELECT count(*) FROM fixture").Scan(&count); err != nil || count != 1 {
		t.Fatalf("pinned reader changed: %d %v", count, err)
	}
	fmt.Fprintln(os.Stdout, "ACKNOWLEDGED")
	if line, err := in.ReadString('\n'); err != nil || line != "attacker-stopped\n" {
		t.Fatalf("stop barrier: %q %v", line, err)
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := reader.QueryRowContext(f.ctx, "SELECT count(*) FROM fixture").Scan(&count); err != nil || count != 2 {
		t.Fatalf("fresh reader: %d %v", count, err)
	}
	if err := f.domain.Check(f.ctx); err != nil {
		t.Fatal(err)
	}
	observation, err = f.domain.Observe(f.ctx, lease, "catalog", "catalog.sqlite", 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.QueryRowContext(f.ctx, "SELECT count(*) FROM fixture").Scan(&count); err != nil || count != 2 {
		t.Fatalf("identity observation disturbed SQLite locks: %d %v", count, err)
	}
	for _, name := range []string{"catalog.sqlite", "catalog.sqlite-wal"} {
		var st unix.Stat_t
		if unix.Stat(protectedFixtureStatePath+"/"+name, &st) != nil || st.Nlink != 1 || st.Uid != 1000 {
			t.Fatal("state inode escaped protected domain")
		}
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := head.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "PROTECTED_WRITER_COMPLETE")
}

func TestProtectedStateVerifyChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	f := openProtectedKernelFixture(t)
	lease, err := runtime.AcquireLifecycle(f.ctx, f.state, "catalog", ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	log, head, err := f.openLog(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer head.Close()
	defer log.Close()
	got, err := log.Head(f.ctx)
	if err != nil || got.Sequence != 1 {
		t.Fatalf("restart lost authority: %+v %v", got, err)
	}
	main, err := os.ReadFile(protectedFixtureStatePath + "/catalog.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	wal, walErr := os.ReadFile(protectedFixtureStatePath + "/catalog.sqlite-wal")
	if walErr != nil && !errors.Is(walErr, os.ErrNotExist) {
		t.Fatal(walErr)
	}
	if len(wal) != 0 {
		proof, err := log.proveWALCapture(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), 64<<20)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := OpenWALSnapshot(f.ctx, f.state, []*paths.Root{f.target}, lease, "catalog", "catalog.sqlite", 64<<20, f.domain.Check, proof.VerifyWAL)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
			rows, err := q.Query("SELECT n FROM fixture ORDER BY n")
			if err != nil {
				return err
			}
			for rows.Next() {
				var n int
				if err := rows.Scan(&n); err != nil {
					return err
				}
				if n != 7+count {
					return errors.New("unexpected acknowledged row")
				}
				count++
			}
			return rows.Err()
		})
		closeErr := snapshot.Close()
		if err != nil || closeErr != nil || count != 2 {
			t.Fatalf("crash recovery rows: %d %v %v", count, err, closeErr)
		}
		fmt.Fprintln(os.Stdout, "RESTART_WAL_VERIFIED")
	} else {
		proof, err := log.proveCheckpointedCapture(f.ctx, bytes.NewReader(main), int64(len(main)), 64<<20)
		if err != nil || proof.VerifyCheckpoint(f.ctx, got.Image.SHA256) != nil {
			t.Fatalf("restart checkpoint authority: %v", err)
		}
		fmt.Fprintln(os.Stdout, "RESTART_CHECKPOINT_VERIFIED")
	}
}

func TestProtectedStateCheckpointChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	f := openProtectedKernelFixture(t)
	lease, err := runtime.AcquireLifecycle(f.ctx, f.state, "catalog", ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	log, head, err := f.openLog(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer head.Close()
	defer log.Close()
	main, err := os.ReadFile(protectedFixtureStatePath + "/catalog.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(protectedFixtureStatePath + "/catalog.sqlite-wal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.proveWALCapture(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), 64<<20); err != nil {
		t.Fatal(err)
	}
	observation, err := f.domain.Observe(f.ctx, lease, "catalog", "catalog.sqlite", 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", walOracleDSN(protectedFixtureStatePath+"/catalog.sqlite", "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	var busy, frames, done int
	if err := db.QueryRowContext(f.ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &done); err != nil || busy != 0 {
		t.Fatalf("checkpoint: %d %v", busy, err)
	}
	fmt.Fprintln(os.Stdout, "CHECKPOINTED")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	t.Fatal("checkpoint crash fixture unexpectedly resumed")
}

func TestProtectedStatePendingChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	f := openProtectedKernelFixture(t)
	log, _, err := f.openLog(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := log.Head(f.ctx); !errors.Is(err, errWALAuthorityPending) || got != (walAuthorityReceipt{}) {
		t.Fatalf("unknown SQL outcome was served: %+v %v", got, err)
	}
	fmt.Fprintln(os.Stdout, "PENDING_RECOVERY_REQUIRED")
}

func TestProtectedStateRollbackChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	f := openProtectedKernelFixture(t)
	headPath := "/authority/" + walHeadName
	for _, attempt := range []func() error{
		func() error { return os.Rename(protectedFixtureStatePath, "/target/engine-moved-state") },
		func() error { return os.Rename("/managed", "/target/engine-moved-parent") },
		func() error {
			f, e := os.OpenFile(headPath, os.O_WRONLY|os.O_TRUNC, 0)
			if f != nil {
				_ = f.Close()
			}
			return e
		},
		func() error { return os.Remove(headPath) },
		func() error { return os.Rename(headPath, headPath+".old") },
		func() error {
			f, e := os.OpenFile(headPath, os.O_RDWR|os.O_APPEND, 0)
			if e != nil {
				return e
			}
			defer f.Close()
			return f.Truncate(0)
		},
		func() error {
			f, e := os.OpenFile(headPath, os.O_RDWR|os.O_APPEND, 0)
			if e != nil {
				return e
			}
			defer f.Close()
			return unix.IoctlSetPointerInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, 0)
		},
	} {
		if err := attempt(); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("kernel allowed head rollback: %v", err)
		}
	}
	path := protectedFixtureStatePath + "/" + walAuthorityName
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) != 3*walAuthorityRecordSize && len(raw) != 2*walAuthorityRecordSize {
		t.Fatalf("mutable journal: %d %v", len(raw), err)
	}
	// This is a complete valid old journal, not a malformed/truncated record.
	if err := os.WriteFile(path, raw[:walAuthorityRecordSize], 0600); err != nil {
		t.Fatal(err)
	}
	log, _, err := f.openLog(t, nil)
	if err == nil || log != nil {
		t.Fatal("trusted head accepted whole-journal rollback")
	}
	fmt.Fprintln(os.Stdout, "WHOLE_JOURNAL_ROLLBACK_REJECTED")
}

func TestProtectedStateRejectsProvisioningChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	f := openProtectedKernelFixture(t)
	switch os.Getenv("MCE4D_PROTECTED_NEGATIVE_CASE") {
	case "head-flag", "head-parent", "head-tail":
		initial := walLogicalImage{PageSize: 4096, PageCount: 1, SHA256: sha256.Sum256([]byte("synthetic initial image"))}
		if log, _, err := f.openLog(t, &initial); err == nil || log != nil {
			t.Fatal("unprotected head was accepted")
		}
	case "main-link", "wal-link":
		lease, err := runtime.AcquireLifecycle(f.ctx, f.state, "catalog", ports.SharedLease, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Close()
		if observation, err := f.domain.Observe(f.ctx, lease, "catalog", "catalog.sqlite", 64<<20); err == nil || observation != nil {
			t.Fatal("preexisting aliased state was accepted")
		}
	default:
		t.Fatal("unknown negative provisioning fixture")
	}
	fmt.Fprintln(os.Stdout, "PROVISIONING_REJECTED")
}

func TestProtectedStateAttackerChild(t *testing.T) {
	if os.Getenv("MCE4D_PROTECTED_KERNEL_FIXTURE") != "synthetic-v1" {
		return
	}
	if unix.Geteuid() != 1001 {
		t.Fatal("wrong synthetic attacker UID")
	}
	done := make(chan struct{})
	go func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n'); close(done) }()
	fmt.Fprintln(os.Stdout, "ATTACKER_READY")
	deadline := time.Now().Add(10 * time.Second)
	count := 0
	for time.Now().Before(deadline) {
		for _, attempt := range []func() error{
			func() error { return os.Rename(protectedFixtureStatePath, "/target/moved-state") },
			func() error { return os.Rename("/managed", "/target/moved-parent") },
			func() error { return os.Link(protectedFixtureStatePath+"/catalog.sqlite", "/target/main-link") },
			func() error { return os.Link(protectedFixtureStatePath+"/catalog.sqlite-wal", "/target/wal-link") },
			func() error { return os.Link(protectedFixtureStatePath+"/catalog.sqlite-shm", "/target/shm-link") },
		} {
			if err := attempt(); !errors.Is(err, os.ErrPermission) {
				t.Fatalf("state attack outcome: %v", err)
			}
			count++
		}
		select {
		case <-done:
			fmt.Fprintf(os.Stdout, "ATTACKS_REJECTED %d\n", count)
			return
		default:
		}
	}
	t.Fatal("attack fixture did not receive a stop signal")
}
