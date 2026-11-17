//go:build linux

package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type fixtureWALReceipt struct {
	Pair   WALSnapshotPair
	Prefix walCommitPrefixReceipt
}

func fixturePrefixReceipt(t *testing.T, main, wal []byte, frame, pages uint32) walCommitPrefixReceipt {
	t.Helper()
	if len(wal) < 32 || frame == 0 || pages == 0 {
		t.Fatal("invalid fixture receipt")
	}
	pageSize := binary.BigEndian.Uint32(wal[8:12])
	if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 {
		t.Fatal("invalid fixture WAL page size")
	}
	end := 32 + uint64(frame)*(uint64(pageSize)+24)
	if end > uint64(len(wal)) {
		t.Fatal("fixture WAL lacks the acknowledged frame")
	}
	return walCommitPrefixReceipt{
		MainSize: int64(len(main)), MainSHA256: sha256.Sum256(main),
		WALPrefixSize: int64(end), WALPrefixSHA256: sha256.Sum256(wal[:int(end)]),
		WALCommitFrame: frame, WALCommitPages: pages,
	}
}

// The writer sends its observed pair to a separate fixture process. That
// process fsyncs a receipt outside the state directory before acknowledging
// the operation. This is a test protocol, not a production commit authority.
func TestWALReceiptWriterChild(t *testing.T) {
	if os.Getenv("MCE4D_WAL_RECEIPT_CHILD") != "1" {
		return
	}
	root, protected, err := openWALIndexOracleFixture()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	defer protected.Close()
	state := os.Getenv("MCE4D_MEMFD_ORACLE_STATE")
	db, err := sql.Open("sqlite", walOracleDSN(filepath.Join(state, "catalog.sqlite"), "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err = db.Exec("PRAGMA wal_autocheckpoint=0; INSERT INTO fixture VALUES(8)"); err != nil {
		t.Fatal(err)
	}
	var busy, frames, checkpointed, pages int
	if err = db.QueryRow("PRAGMA main.wal_checkpoint(NOOP)").Scan(&busy, &frames, &checkpointed); err != nil || busy != 0 || frames < 1 || checkpointed != 0 {
		t.Fatalf("commit boundary: %d %d %d %v", busy, frames, checkpointed, err)
	}
	if err = db.QueryRow("PRAGMA main.page_count").Scan(&pages); err != nil || pages < 1 {
		t.Fatalf("commit page count: %d %v", pages, err)
	}
	// Keep a read transaction open so another process can independently probe
	// the main database's SQLite lock while the receipt process reads it.
	if _, err = db.Exec("BEGIN"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM fixture").Scan(&count); err != nil || count != 2 {
		t.Fatalf("committed rows before receipt: %d %v", count, err)
	}
	// Opening and closing a second ordinary descriptor in this process can
	// release SQLite's POSIX locks. The parent captures the bytes instead.
	if _, err = fmt.Fprintf(os.Stdout, "COMMITTED %d %d\n", frames, pages); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || line != "receipt-synced\n" {
		t.Fatalf("receipt acknowledgement unavailable: %v", err)
	}
	if _, err = fmt.Fprintln(os.Stdout, "ACKNOWLEDGED"); err != nil {
		t.Fatal(err)
	}
	line, err = bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || line != "spill\n" {
		t.Fatalf("spill request unavailable: %v", err)
	}
	// Spill a later, uncommitted transaction after the acknowledged receipt.
	// Killing the child leaves those frames for SQLite recovery to ignore.
	if _, err = db.Exec("ROLLBACK; PRAGMA cache_size=10; BEGIN IMMEDIATE; WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<30000) INSERT INTO fixture SELECT x+100 FROM n"); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(os.Stdout, "SPILLED"); err != nil {
		t.Fatal(err)
	}
	// Normal SQLite close would checkpoint away the uncheckpointed fixture.
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestWALReceiptDetectsRollbackAfterWriterCrash(t *testing.T) {
	f := newWALSnapshotFixture(t)
	oldMain, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	oldWAL, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(f.state, "wal-memory.owner"))
	if err != nil {
		t.Fatal(err)
	}
	// No snapshot or SQLite handle is open while changing the fixture lease.
	if err = f.lease.Close(); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(f.ctx, testBinary, "-test.run=^TestWALReceiptWriterChild$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_WAL_RECEIPT_CHILD=1", "MCE4D_MEMFD_ORACLE_STATE="+f.state,
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
	line, err := reader.ReadString('\n')
	fields := strings.Fields(line)
	if err != nil || len(fields) != 3 || fields[0] != "COMMITTED" {
		t.Fatalf("writer did not report the next commit: %q %v", line, err)
	}
	frames, frameErr := strconv.ParseUint(fields[1], 10, 32)
	pages, pageErr := strconv.ParseUint(fields[2], 10, 32)
	if frameErr != nil || pageErr != nil || frames <= uint64(f.pair.WALCommitFrame) || pages == 0 {
		t.Fatalf("invalid writer boundary: %q", line)
	}
	probeMainLock := func() {
		t.Helper()
		probe := exec.CommandContext(f.ctx, testBinary, "-test.run=^TestStateObservationLockProbeChild$", "-test.timeout=5s")
		probe.Env = append(os.Environ(), "MCE4D_STATE_LOCK_PROBE=1", "MCE4D_STATE_LOCK_NAME=catalog.sqlite",
			"MCE4D_MEMFD_ORACLE_STATE="+f.state, "MCE4D_MEMFD_ORACLE_TARGET="+f.target, "MCE4D_MEMFD_ORACLE_NONCE="+string(marker))
		out, probeErr := probe.CombinedOutput()
		if probeErr != nil || strings.TrimSpace(string(out)) != "BUSY\nPASS" {
			t.Fatalf("writer main lock not held: %q %v", out, probeErr)
		}
	}
	probeMainLock()
	// The writer is waiting for the receipt, and this separate process has no
	// SQLite handle for these files; closing its read descriptors does not drop
	// the writer's POSIX locks. No other writer exists in this fixture.
	mainBytes, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	walBytes, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	if err != nil {
		t.Fatal(err)
	}
	probeMainLock()
	receipt := WALSnapshotPair{
		MainSize: int64(len(mainBytes)), WALSize: int64(len(walBytes)),
		MainSHA256: sha256.Sum256(mainBytes), WALSHA256: sha256.Sum256(walBytes),
		WALCommitFrame: uint32(frames), WALCommitPages: uint32(pages),
	}
	fixtureReceipt := fixtureWALReceipt{Pair: receipt, Prefix: fixturePrefixReceipt(t, mainBytes, walBytes, receipt.WALCommitFrame, receipt.WALCommitPages)}
	oldReceipt := fixturePrefixReceipt(t, oldMain, oldWAL, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	if !bytes.Equal(oldMain, mainBytes) || oldReceipt.WALPrefixSize > int64(len(walBytes)) || sha256.Sum256(walBytes[:int(oldReceipt.WALPrefixSize)]) != oldReceipt.WALPrefixSHA256 {
		t.Fatal("fixture did not retain the first acknowledged WAL prefix")
	}
	encoded, err := json.Marshal(fixtureReceipt)
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	// This fixture file is outside the state root. fsync establishes the test's
	// ack ordering, not an independent hardware failure domain or a product
	// receipt service.
	receiptDir := t.TempDir()
	receiptPath := filepath.Join(receiptDir, "receipt.json")
	file, err := os.OpenFile(receiptPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(encoded); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(receiptDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(dir.Sync(), dir.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(stdin, "receipt-synced"); err != nil {
		t.Fatal(err)
	}
	ack, err := reader.ReadString('\n')
	if err != nil || ack != "ACKNOWLEDGED\n" {
		t.Fatalf("writer did not acknowledge durable receipt: %q %v", ack, err)
	}
	if _, err = fmt.Fprintln(stdin, "spill"); err != nil {
		t.Fatal(err)
	}
	spill, err := reader.ReadString('\n')
	if err != nil || spill != "SPILLED\n" {
		t.Fatalf("writer did not spill its uncommitted transaction: %q %v", spill, err)
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
	proof := func(ctx context.Context, actual WALSnapshotPair) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, readErr := os.ReadFile(receiptPath)
		var recorded fixtureWALReceipt
		if readErr != nil || json.Unmarshal(data, &recorded) != nil || actual != recorded.Pair {
			return errors.New("fixture receipt does not match WAL pair")
		}
		return nil
	}
	spilledWAL, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	if err != nil || len(spilledWAL) <= len(walBytes) || len(walBytes) < 32 {
		t.Fatalf("uncommitted WAL frames were not retained: %v", err)
	}
	data, err := os.ReadFile(receiptPath)
	var recorded fixtureWALReceipt
	if err != nil || json.Unmarshal(data, &recorded) != nil {
		t.Fatalf("fixture receipt unavailable: %v", err)
	}
	if err = verifyWALCommitPrefix(f.ctx, mainBytes, spilledWAL, recorded.Prefix); err != nil {
		t.Fatalf("acknowledged prefix rejected after uncommitted spill: %v", err)
	}
	if err = verifyWALCommitPrefix(f.ctx, mainBytes, walBytes, oldReceipt); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("later committed frame accepted using older receipt: %v", err)
	}
	if err = verifyWALCommitPrefix(f.ctx, mainBytes, spilledWAL[:int(recorded.Prefix.WALPrefixSize)-1], recorded.Prefix); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("truncated acknowledged prefix accepted: %v", err)
	}
	corrupt := append([]byte(nil), spilledWAL...)
	corrupt[int(recorded.Prefix.WALPrefixSize)-1] ^= 1
	if err = verifyWALCommitPrefix(f.ctx, mainBytes, corrupt, recorded.Prefix); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("corrupted acknowledged prefix accepted: %v", err)
	}
	changedMain := append([]byte(nil), mainBytes...)
	changedMain[len(changedMain)-1] ^= 1
	if err = verifyWALCommitPrefix(f.ctx, changedMain, spilledWAL, recorded.Prefix); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("changed main database accepted: %v", err)
	}
	wrongPages := recorded.Prefix
	wrongPages.WALCommitPages++
	if err = verifyWALCommitPrefix(f.ctx, mainBytes, spilledWAL, wrongPages); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("wrong acknowledged page count accepted: %v", err)
	}
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	if err = verifyWALCommitPrefix(cancelled, mainBytes, spilledWAL, recorded.Prefix); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled receipt check returned %v", err)
	}
	if conservative, openErr := f.open(proof); conservative != nil || !errors.Is(openErr, ErrStateUnsafe) {
		if conservative != nil {
			_ = conservative.Close()
		}
		t.Fatalf("exact receipt unexpectedly accepted changed WAL bytes: %v", openErr)
	}
	// A fixture-only proof of the complete spilled pair lets SQLite show that
	// its uncommitted tail is harmless. It is not an independent authority.
	spilledPair := receipt
	spilledPair.WALSize = int64(len(spilledWAL))
	spilledPair.WALSHA256 = sha256.Sum256(spilledWAL)
	spilledSnapshot, err := f.open(func(_ context.Context, actual WALSnapshotPair) error {
		if actual != spilledPair {
			return errors.New("fixture spilled pair mismatch")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("SQLite rejected the valid uncommitted tail: %v", err)
	}
	err = spilledSnapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		rows, queryErr := q.Query("SELECT n FROM fixture ORDER BY n")
		if queryErr != nil {
			return queryErr
		}
		for _, want := range []int{7, 8} {
			var got int
			if !rows.Next() || rows.Scan(&got) != nil || got != want {
				return fmt.Errorf("uncommitted tail changed recovered value: %d", got)
			}
		}
		if rows.Next() {
			return errors.New("uncommitted tail exposed an additional row")
		}
		return rows.Err()
	})
	if closeErr := spilledSnapshot.Close(); err != nil || closeErr != nil {
		t.Fatalf("spilled snapshot: %v %v", err, closeErr)
	}
	// Restore the exact acknowledged fixture pair before checking its receipt.
	if err = os.WriteFile(filepath.Join(f.state, "catalog.sqlite-wal"), walBytes, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.open(proof)
	if err != nil {
		t.Fatalf("acknowledged commit rejected: %v", err)
	}
	err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		rows, queryErr := q.Query("SELECT n FROM fixture ORDER BY n")
		if queryErr != nil {
			return queryErr
		}
		for _, want := range []int{7, 8} {
			var got int
			if !rows.Next() || rows.Scan(&got) != nil || got != want {
				return fmt.Errorf("expected committed value %d, got %d", want, got)
			}
		}
		if rows.Next() {
			return errors.New("unexpected extra row")
		}
		return rows.Err()
	})
	if closeErr := snapshot.Close(); err != nil || closeErr != nil {
		t.Fatalf("acknowledged snapshot: %v %v", err, closeErr)
	}
	// Rewind both files to the earlier consistent pair. The independent
	// receipt remains at the latest acknowledged pair outside the state root.
	if err = os.WriteFile(filepath.Join(f.state, "catalog.sqlite"), oldMain, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.state, "catalog.sqlite-wal"), oldWAL, 0600); err != nil {
		t.Fatal(err)
	}
	if stale, openErr := f.open(proof); stale != nil || !errors.Is(openErr, ErrStateUnsafe) {
		if stale != nil {
			_ = stale.Close()
		}
		t.Fatalf("rolled-back pair accepted against latest receipt: %v", openErr)
	}
	// Show why SQLite integrity alone is not the oracle: a copy of the old
	// pair recovers successfully but contains only the first committed row.
	controlDir := t.TempDir()
	controlPath := filepath.Join(controlDir, "control.sqlite")
	if err = os.WriteFile(controlPath, oldMain, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(controlPath+"-wal", oldWAL, 0600); err != nil {
		t.Fatal(err)
	}
	control, err := sql.Open("sqlite", walOracleDSN(controlPath, "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	var check string
	var count, value int
	if err = control.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Fatalf("old fixture integrity: %q %v", check, err)
	}
	if err = control.QueryRow("SELECT COUNT(*), MAX(n) FROM fixture").Scan(&count, &value); err != nil || count != 1 || value != 7 {
		t.Fatalf("old fixture contents: count=%d max=%d err=%v", count, value, err)
	}
}
