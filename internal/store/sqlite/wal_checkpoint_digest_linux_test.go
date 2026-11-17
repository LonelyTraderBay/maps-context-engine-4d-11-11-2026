//go:build linux

package sqlite

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// The receipt belongs to this test's separate temporary directory. The
// fixture does not establish an independently durable product authority.
func writeCheckpointFixtureReceipt(t *testing.T, value checkpointImageDigest) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "receipt.json")
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(directory.Sync(), directory.Close()); err != nil {
		t.Fatal(err)
	}
	return path
}

type checkpointFixtureImages struct {
	Before checkpointImageDigest
	After  checkpointImageDigest
}

func TestCheckpointImageWriterChild(t *testing.T) {
	if os.Getenv("MCE4D_CHECKPOINT_IMAGE_CHILD") != "1" {
		return
	}
	root, protected, err := openWALIndexOracleFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	defer protected.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	path := filepath.Join(os.Getenv("MCE4D_MEMFD_ORACLE_STATE"), "catalog.sqlite")
	db, err := sql.Open("sqlite", walOracleDSN(path, "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	before := captureFixtureCheckpointImage(t, ctx, db)
	if _, err = db.ExecContext(ctx, "INSERT INTO fixture VALUES(8)"); err != nil {
		t.Fatal(err)
	}
	after := captureFixtureCheckpointImage(t, ctx, db)
	if err = json.NewEncoder(os.Stdout).Encode(checkpointFixtureImages{Before: before, After: after}); err != nil {
		t.Fatal(err)
	}
	in := bufio.NewReader(os.Stdin)
	line, err := in.ReadString('\n')
	if err != nil || line != "receipt-synced\n" {
		t.Fatalf("receipt acknowledgement unavailable: %v", err)
	}
	if _, err = fmt.Fprintln(os.Stdout, "ACKNOWLEDGED"); err != nil {
		t.Fatal(err)
	}
	line, err = in.ReadString('\n')
	if err != nil || line != "checkpoint\n" {
		t.Fatalf("checkpoint request unavailable: %v", err)
	}
	var busy, frames, checkpointed int
	if err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil || busy != 0 || frames != 0 || checkpointed != 0 {
		t.Fatalf("fixture checkpoint: %d %d %d %v", busy, frames, checkpointed, err)
	}
	if _, err = fmt.Fprintln(os.Stdout, "CHECKPOINTED"); err != nil {
		t.Fatal(err)
	}
	// The parent kills this process after the checkpoint; normal close would not
	// exercise recovery after abrupt process loss.
	_, _ = in.ReadString('\n')
}

func TestCheckpointConcurrentWriterChild(t *testing.T) {
	if os.Getenv("MCE4D_CHECKPOINT_CONCURRENT_CHILD") != "1" {
		return
	}
	root, protected, err := openWALIndexOracleFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	defer protected.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lease, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	writer, err := runtime.AcquireCatalogMutation(ctx, lease, walOracleJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	path := filepath.Join(os.Getenv("MCE4D_MEMFD_ORACLE_STATE"), "catalog.sqlite")
	db, err := sql.Open("sqlite", walOracleDSN(path, "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0; INSERT INTO fixture VALUES(8)"); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(os.Stdout, "COMMITTED"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || line != "stop\n" {
		t.Fatalf("fixture shutdown: %v", err)
	}
}

func TestCheckpointImageConcurrentWriter(t *testing.T) {
	if os.Getenv("MCE4D_CHECKPOINT_CONCURRENT_SUITE") != "1" {
		testBinary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, testBinary, "-test.run=^TestCheckpointImageConcurrentWriter$", "-test.timeout=18s")
		cmd.Env = append(os.Environ(), "MCE4D_CHECKPOINT_CONCURRENT_SUITE=1", "MODERNC_SQLITE_OFD_LOCK=0")
		if out, runErr := cmd.CombinedOutput(); runErr != nil {
			t.Fatalf("checkpoint concurrent fixture: %v: %s", runErr, out)
		}
		return
	}
	f := newWALSnapshotFixture(t)
	marker, err := os.ReadFile(filepath.Join(f.state, "wal-memory.owner"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.lease.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err := runtime.AcquireLifecycle(f.ctx, f.root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	hold, err := lease.Hold(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	// SQLite paths in this case refer exclusively to the test-owned fixture.
	db, err := sql.Open("sqlite", walOracleDSN(filepath.Join(f.state, "catalog.sqlite"), "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	pinned, err := captureCheckpointImage(f.ctx, conn, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(f.ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count, pages, pageSize int64
	var version string
	if err = tx.QueryRowContext(f.ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(f.ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(f.ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != pinned.SQLiteVersion || pageSize != pinned.PageSize || pages*pageSize != pinned.ImageSize {
		t.Fatal("reader metadata did not match the initial snapshot")
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe := func() {
		t.Helper()
		cmd := exec.CommandContext(f.ctx, testBinary, "-test.run=^TestStateObservationLockProbeChild$", "-test.timeout=5s")
		cmd.Env = append(os.Environ(), "MCE4D_STATE_LOCK_PROBE=1", "MCE4D_STATE_LOCK_NAME=catalog.sqlite",
			"MCE4D_MEMFD_ORACLE_STATE="+f.state, "MCE4D_MEMFD_ORACLE_TARGET="+f.target, "MCE4D_MEMFD_ORACLE_NONCE="+string(marker))
		out, probeErr := cmd.CombinedOutput()
		if probeErr != nil || strings.TrimSpace(string(out)) != "BUSY\nPASS" {
			t.Fatalf("reader main lock not held: %q %v", out, probeErr)
		}
	}
	probe()
	cmd := exec.CommandContext(f.ctx, testBinary, "-test.run=^TestCheckpointConcurrentWriterChild$", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "MCE4D_CHECKPOINT_CONCURRENT_CHILD=1", "MCE4D_MEMFD_ORACLE_STATE="+f.state,
		"MCE4D_MEMFD_ORACLE_TARGET="+f.target, "MCE4D_MEMFD_ORACLE_NONCE="+string(marker))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
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
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "COMMITTED\n" {
		t.Fatalf("concurrent writer did not commit: %q %v", line, err)
	}
	after := checkpointImageDigest{SQLiteVersion: version, PageSize: pageSize, ImageSize: pages * pageSize}
	if err = backupPinnedCheckpointImage(f.ctx, conn, &after, 2<<20); err != nil || after != pinned {
		t.Fatalf("pinned image changed during writer commit: %v", err)
	}
	probe()
	if err = tx.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM fixture").Scan(&count); err != nil || count != 1 {
		t.Fatalf("reader snapshot changed during writer commit: %d %v", count, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	fresh, err := captureCheckpointImage(f.ctx, conn, 2<<20)
	if err != nil || fresh == pinned {
		t.Fatalf("fresh image did not observe writer commit: %v", err)
	}
	if err = conn.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM fixture").Scan(&count); err != nil || count != 2 {
		t.Fatalf("fresh reader did not observe writer commit: %d %v", count, err)
	}
	if _, err = fmt.Fprintln(stdin, "stop"); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	waited = true
}

func TestCheckpointImageReceiptSurvivesWriterCrash(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		name := "before-checkpoint"
		if checkpoint {
			name = "after-checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			runCheckpointImageReceiptCrash(t, checkpoint)
		})
	}
}

func runCheckpointImageReceiptCrash(t *testing.T, checkpoint bool) {
	t.Helper()
	f := newWALSnapshotFixture(t)
	marker, err := os.ReadFile(filepath.Join(f.state, "wal-memory.owner"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.lease.Close(); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(f.ctx, testBinary, "-test.run=^TestCheckpointImageWriterChild$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_CHECKPOINT_IMAGE_CHILD=1", "MCE4D_MEMFD_ORACLE_STATE="+f.state,
		"MCE4D_MEMFD_ORACLE_TARGET="+f.target, "MCE4D_MEMFD_ORACLE_NONCE="+string(marker), "MODERNC_SQLITE_OFD_LOCK=0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
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
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadBytes('\n')
	var images checkpointFixtureImages
	if err != nil || json.Unmarshal(line, &images) != nil || images.Before == images.After || images.After.ImageSize < 1 || images.After.SQLiteVersion == "" {
		t.Fatalf("writer did not produce distinct fixture images: %q %v", line, err)
	}
	receiptPath := writeCheckpointFixtureReceipt(t, images.After)
	if _, err = fmt.Fprintln(stdin, "receipt-synced"); err != nil {
		t.Fatal(err)
	}
	ack, err := reader.ReadString('\n')
	if err != nil || ack != "ACKNOWLEDGED\n" {
		t.Fatalf("writer acknowledged before receipt sync: %q %v", ack, err)
	}
	if checkpoint {
		if _, err = fmt.Fprintln(stdin, "checkpoint"); err != nil {
			t.Fatal(err)
		}
		message, readErr := reader.ReadString('\n')
		if readErr != nil || message != "CHECKPOINTED\n" {
			t.Fatalf("writer did not complete checkpoint: %q %v", message, readErr)
		}
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waited = true
	_ = stdin.Close()
	f.lease, err = runtime.AcquireLifecycle(f.ctx, f.root, "catalog", ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.lease.Close() })
	// The direct SQLite path below is limited to the test-owned fixture. It is
	// deliberately not an authorized product state opener.
	db, err := sql.Open("sqlite", walOracleDSN(filepath.Join(f.state, "catalog.sqlite"), "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if recovered := captureFixtureCheckpointImage(t, f.ctx, db); recovered != readCheckpointFixtureReceipt(t, receiptPath) {
		t.Fatal("checkpointed recovery does not match the acknowledged fixture receipt")
	}
	if images.Before == readCheckpointFixtureReceipt(t, receiptPath) {
		t.Fatal("earlier image unexpectedly matches the later acknowledged receipt")
	}
	var integrity string
	if err = db.QueryRowContext(f.ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("fixture integrity: %q %v", integrity, err)
	}
	rows, err := db.QueryContext(f.ctx, "SELECT n FROM fixture ORDER BY n")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var n int
		if err = rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		values = append(values, fmt.Sprint(n))
	}
	if err = rows.Err(); err != nil || strings.Join(values, ",") != "7,8" {
		t.Fatalf("fixture recovery rows: %v %v", values, err)
	}
}

func readCheckpointFixtureReceipt(t *testing.T, path string) checkpointImageDigest {
	t.Helper()
	data, err := os.ReadFile(path)
	var value checkpointImageDigest
	if err != nil || json.Unmarshal(data, &value) != nil {
		t.Fatalf("fixture receipt unavailable: %v", err)
	}
	return value
}

func captureFixtureCheckpointImage(t *testing.T, ctx context.Context, db *sql.DB) checkpointImageDigest {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	image, captureErr := captureCheckpointImage(ctx, conn, 2<<20)
	if err = errors.Join(captureErr, conn.Close()); err != nil {
		t.Fatalf("capture fixture image: %v", err)
	}
	return image
}

func TestCheckpointImageReceiptSurvivesCheckpoint(t *testing.T) {
	for _, pageSize := range []int{512, 1024, 4096, 65536} {
		t.Run(fmt.Sprint(pageSize), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "fixture.sqlite")
			db, err := sql.Open("sqlite", walOracleDSN(path, "unix", "rwc"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			if _, err = db.ExecContext(ctx, fmt.Sprintf("PRAGMA page_size=%d", pageSize)); err != nil {
				t.Fatal(err)
			}
			var mode string
			if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
				t.Fatalf("WAL setup: %q %v", mode, err)
			}
			if _, err = db.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0; CREATE TABLE fixture(n INTEGER NOT NULL,payload BLOB NOT NULL); INSERT INTO fixture VALUES(7,zeroblob(131072))"); err != nil {
				t.Fatal(err)
			}
			before := captureFixtureCheckpointImage(t, ctx, db)
			if _, err = db.ExecContext(ctx, "INSERT INTO fixture VALUES(8,zeroblob(131072))"); err != nil {
				t.Fatal(err)
			}
			acknowledged := captureFixtureCheckpointImage(t, ctx, db)
			if acknowledged == before || acknowledged.PageSize != int64(pageSize) || acknowledged.SQLiteVersion == "" {
				t.Fatalf("committed fixture did not change its image: before=%v after=%v", before, acknowledged)
			}
			receiptPath := writeCheckpointFixtureReceipt(t, acknowledged)
			for _, checkpoint := range []string{"FULL", "TRUNCATE"} {
				var busy, logFrames, checkpointed int
				if err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint("+checkpoint+")").Scan(&busy, &logFrames, &checkpointed); err != nil || busy != 0 || logFrames != checkpointed {
					t.Fatalf("%s checkpoint: %d %d %d %v", checkpoint, busy, logFrames, checkpointed, err)
				}
				if got := captureFixtureCheckpointImage(t, ctx, db); got != readCheckpointFixtureReceipt(t, receiptPath) {
					t.Fatalf("%s checkpoint changed acknowledged image", checkpoint)
				}
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			limited, limitErr := captureCheckpointImage(ctx, conn, acknowledged.ImageSize-1)
			if !errors.Is(limitErr, ErrStateLimit) || limited != (checkpointImageDigest{}) {
				t.Fatalf("image limit did not fail closed: %v", limitErr)
			}
			cancelled, stop := context.WithCancel(ctx)
			stop()
			cancelledImage, cancelErr := captureCheckpointImage(cancelled, conn, 2<<20)
			if !errors.Is(cancelErr, context.Canceled) || cancelledImage != (checkpointImageDigest{}) {
				t.Fatalf("cancelled capture did not fail closed: %v", cancelErr)
			}
			if err = conn.Close(); err != nil {
				t.Fatal(err)
			}
			if got := captureFixtureCheckpointImage(t, ctx, db); got != acknowledged {
				t.Fatal("limit failure changed the fixture or retained its transaction")
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := sql.Open("sqlite", walOracleDSN(path, "unix", "rw"))
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			reopened.SetMaxOpenConns(1)
			if got := captureFixtureCheckpointImage(t, ctx, reopened); got != readCheckpointFixtureReceipt(t, receiptPath) {
				t.Fatal("reopened database did not match its acknowledged receipt")
			}
			if _, err = reopened.ExecContext(ctx, "DELETE FROM fixture WHERE n=8"); err != nil {
				t.Fatal(err)
			}
			changed := captureFixtureCheckpointImage(t, ctx, reopened)
			if changed.ImageSize != acknowledged.ImageSize || changed.SHA256 == readCheckpointFixtureReceipt(t, receiptPath).SHA256 {
				t.Fatal("receipt missed a same-size database change")
			}
		})
	}
}
