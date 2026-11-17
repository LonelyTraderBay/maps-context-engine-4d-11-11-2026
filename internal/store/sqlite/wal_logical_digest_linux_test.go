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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestWALLogicalImageMatchesSQLiteCheckpoint(t *testing.T) {
	for _, pageSize := range []int{512, 1024, 4096, 65536} {
		t.Run(fmt.Sprint(pageSize), func(t *testing.T) {
			checkLogicalImageAgainstCheckpoint(t, pageSize, false)
		})
	}
}

func TestWALLogicalImageIgnoresUncommittedTail(t *testing.T) {
	checkLogicalImageAgainstCheckpoint(t, 4096, true)
}

func TestWALLogicalImageBigEndianChecksum(t *testing.T) {
	f := newWALSnapshotFixture(t)
	main, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.state, "catalog.sqlite-wal")
	wal, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixturePrefixReceipt(t, main, wal, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	want, err := digestCommittedWALImage(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), receipt, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
	step := pageSize + 24
	if (len(wal)-32)%step != 0 {
		t.Fatal("fixture has an incomplete frame")
	}
	binary.BigEndian.PutUint32(wal[:4], 0x377f0683)
	var one, two uint32
	update := func(data []byte) {
		for i := 0; i < len(data); i += 8 {
			one = one + binary.BigEndian.Uint32(data[i:i+4]) + two
			two = two + binary.BigEndian.Uint32(data[i+4:i+8]) + one
		}
	}
	update(wal[:24])
	binary.BigEndian.PutUint32(wal[24:28], one)
	binary.BigEndian.PutUint32(wal[28:32], two)
	for offset := 32; offset < len(wal); offset += step {
		frame := wal[offset : offset+step]
		update(frame[:8])
		update(frame[24:])
		binary.BigEndian.PutUint32(frame[16:20], one)
		binary.BigEndian.PutUint32(frame[20:24], two)
	}
	if err := os.WriteFile(path, wal, 0600); err != nil {
		t.Fatal(err)
	}
	f.pair.WALSHA256 = sha256.Sum256(wal)
	if err := readCommittedFixtureRow(f); err != nil {
		t.Fatalf("SQLite rejected fixture checksum: %v", err)
	}
	receipt = fixturePrefixReceipt(t, main, wal, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	got, err := digestCommittedWALImage(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), receipt, 64<<20)
	if err != nil || got != want {
		t.Fatalf("big-endian committed image: %v", err)
	}
}

func checkLogicalImageAgainstCheckpoint(t *testing.T, pageSize int, uncommitted bool) {
	t.Helper()
	f := newWALSnapshotFixtureWithOptions(t, pageSize, uncommitted)
	mainPath, walPath := filepath.Join(f.state, "catalog.sqlite"), filepath.Join(f.state, "catalog.sqlite-wal")
	main, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixturePrefixReceipt(t, main, wal, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	mainFile, err := os.Open(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	walFile, err := os.Open(walPath)
	if err != nil {
		_ = mainFile.Close()
		t.Fatal(err)
	}
	before, err := digestCommittedWALImage(f.ctx, mainFile, int64(len(main)), walFile, int64(len(wal)), receipt, 64<<20)
	closeErr := errors.Join(mainFile.Close(), walFile.Close())
	if err != nil || closeErr != nil {
		t.Fatalf("committed image: %v, close: %v", err, closeErr)
	}
	// The writer process is gone and these ordinary file descriptors are closed
	// before SQLite opens the fixture, so this test does not drop its POSIX locks.
	db, err := sql.Open("sqlite", walOracleDSN(mainPath, "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	var busy, frames, checkpointed int
	err = db.QueryRowContext(f.ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed)
	if err != nil || busy != 0 {
		_ = db.Close()
		t.Fatalf("checkpoint: busy=%d frames=%d checkpointed=%d err=%v", busy, frames, checkpointed, err)
	}
	var count, value int
	if err := db.QueryRowContext(f.ctx, "SELECT count(*),min(n) FROM fixture").Scan(&count, &value); err != nil || count != 1 || value != 7 {
		_ = db.Close()
		t.Fatalf("SQLite recovered a different committed result: count=%d value=%d err=%v", count, value, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	mainFile, err = os.Open(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := mainFile.Stat()
	if err != nil {
		_ = mainFile.Close()
		t.Fatal(err)
	}
	after, err := digestCheckpointedMain(f.ctx, mainFile, info.Size(), f.pair.WALCommitPages, 64<<20)
	closeErr = mainFile.Close()
	if err != nil || closeErr != nil || before != after || before.PageSize != uint32(pageSize) {
		t.Fatalf("logical image changed across checkpoint: before=%+v after=%+v err=%v close=%v", before, after, err, closeErr)
	}
}

func TestWALLogicalImageRejectsWrongReceiptAndCorruption(t *testing.T) {
	f := newWALSnapshotFixture(t)
	main, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixturePrefixReceipt(t, main, wal, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	read := func(ctx context.Context, main, wal []byte, receipt walCommitPrefixReceipt, cap int64) (walLogicalImage, error) {
		return digestCommittedWALImage(ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), receipt, cap)
	}
	want, err := read(f.ctx, main, wal, receipt, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	tail := append(bytes.Clone(wal), 1, 2, 3)
	if got, err := read(f.ctx, main, tail, receipt, 64<<20); err != nil || got != want {
		t.Fatalf("incomplete tail changed committed image: %v", err)
	}
	changed := bytes.Clone(wal)
	changed[int(receipt.WALPrefixSize)-1] ^= 1
	if _, err := read(f.ctx, main, changed, receipt, 64<<20); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("corrupted acknowledged frame accepted: %v", err)
	}
	if _, err := read(f.ctx, main, wal[:len(wal)-1], receipt, 64<<20); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("truncated acknowledged frame accepted: %v", err)
	}
	changedMain := bytes.Clone(main)
	changedMain[len(changedMain)-1] ^= 1
	if _, err := read(f.ctx, changedMain, wal, receipt, 64<<20); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("changed main accepted: %v", err)
	}
	step := int(binary.BigEndian.Uint32(wal[8:12])) + 24
	var olderFrame, olderPages uint32
	for offset, number := 32, uint32(1); offset+step <= len(wal) && number < receipt.WALCommitFrame; offset, number = offset+step, number+1 {
		if pages := binary.BigEndian.Uint32(wal[offset+4 : offset+8]); pages != 0 {
			olderFrame, olderPages = number, pages
		}
	}
	if olderFrame == 0 {
		t.Fatal("fixture lacks an earlier commit")
	}
	older := fixturePrefixReceipt(t, main, wal, olderFrame, olderPages)
	if _, err := read(f.ctx, main, wal, older, 64<<20); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("later committed frame accepted using older receipt: %v", err)
	}
	if _, err := read(f.ctx, main, wal, receipt, int64(len(main)+len(wal)-1)); !errors.Is(err, ErrStateLimit) {
		t.Fatalf("size bound ignored: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := read(canceled, main, wal, receipt, 64<<20); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled digest: %v", err)
	}
}

// paddedLogicalReader supplies a large synthetic main file without allocating
// it; its first bytes are an existing fixture and its suffix consists of zeroes.
type paddedLogicalReader struct {
	first      []byte
	size       int64
	maxRequest int
	reads      int
}

func (r *paddedLogicalReader) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset >= r.size {
		return 0, io.EOF
	}
	r.reads++
	if len(p) > r.maxRequest {
		r.maxRequest = len(p)
	}
	count := int(min(int64(len(p)), r.size-offset))
	clear(p[:count])
	if offset < int64(len(r.first)) {
		copy(p[:count], r.first[int(offset):])
	}
	if count != len(p) {
		return count, io.EOF
	}
	return count, nil
}

func TestWALLogicalImageUsesBoundedReads(t *testing.T) {
	f := newWALSnapshotFixture(t)
	main, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixturePrefixReceipt(t, main, wal, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	want, err := digestCommittedWALImage(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), receipt, 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	const syntheticSize int64 = 64 << 20
	if int64(len(main)) >= syntheticSize {
		t.Fatal("fixture unexpectedly large")
	}
	padded := &paddedLogicalReader{first: main, size: syntheticSize}
	hash := sha256.New()
	_, _ = hash.Write(main)
	zeroes := make([]byte, 65536)
	for left := syntheticSize - int64(len(main)); left > 0; {
		n := min(left, int64(len(zeroes)))
		_, _ = hash.Write(zeroes[:int(n)])
		left -= n
	}
	receipt.MainSize = syntheticSize
	copy(receipt.MainSHA256[:], hash.Sum(nil))
	got, err := digestCommittedWALImage(f.ctx, padded, syntheticSize, bytes.NewReader(wal), int64(len(wal)), receipt, 128<<20)
	if err != nil || got != want || padded.maxRequest > 65536 || padded.reads < 1000 {
		t.Fatalf("bounded streaming result: reads=%d max=%d err=%v", padded.reads, padded.maxRequest, err)
	}
	if _, err := digestCommittedWALImage(f.ctx, padded, syntheticSize, bytes.NewReader(wal), int64(len(wal)), receipt, walLogicalImageMaxBytes+1); !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("maximum bound was not enforced: %v", err)
	}
}

// The log is synced before the child ACKs. Identity/key provenance and storage
// containment are fixture assumptions; this is not a product state owner.
func TestWALLogicalImageReceiptSurvivesCheckpointCrash(t *testing.T) {
	f := newWALSnapshotFixture(t)
	mainPath, walPath := filepath.Join(f.state, "catalog.sqlite"), filepath.Join(f.state, "catalog.sqlite-wal")
	oldMain, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	oldWAL, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	oldReceipt := fixturePrefixReceipt(t, oldMain, oldWAL, f.pair.WALCommitFrame, f.pair.WALCommitPages)
	oldImage, err := digestCommittedWALImage(f.ctx, bytes.NewReader(oldMain), int64(len(oldMain)), bytes.NewReader(oldWAL), int64(len(oldWAL)), oldReceipt, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	authority := newAuthorityLogFixture(t)
	log, err := authority.open(&oldImage, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	operation := sha256.Sum256([]byte("synthetic SQLite checkpoint transaction"))
	if err := log.Prepare(f.ctx, operation); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(f.state, "wal-memory.owner"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.lease.Close(); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(f.ctx, binary, "-test.run=^TestCheckpointImageWriterChild$", "-test.timeout=12s")
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
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
	if err != nil || json.Unmarshal(line, &images) != nil || images.Before == images.After {
		t.Fatalf("fixture writer did not commit: %v", err)
	}
	// The writer is another process and waits here, so these ordinary reads do
	// not close descriptors in the SQLite process or alter its POSIX locks.
	main, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	wal, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	frame, pages, err := walCommittedBoundary(f.ctx, main, wal)
	if err != nil || frame == 0 || pages == 0 {
		t.Fatalf("fixture commit boundary: %v", err)
	}
	prefix := fixturePrefixReceipt(t, main, wal, frame, pages)
	image, err := digestCommittedWALImage(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)), prefix, 64<<20)
	if err != nil || image == oldImage {
		t.Fatalf("new committed image unavailable: %v", err)
	}
	if err := log.Commit(f.ctx, operation, image, prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(stdin, "receipt-synced"); err != nil {
		t.Fatal(err)
	}
	if ack, err := reader.ReadString('\n'); err != nil || ack != "ACKNOWLEDGED\n" {
		t.Fatalf("fixture acknowledgement: %q %v", ack, err)
	}
	if _, err := fmt.Fprintln(stdin, "checkpoint"); err != nil {
		t.Fatal(err)
	}
	if ack, err := reader.ReadString('\n'); err != nil || ack != "CHECKPOINTED\n" {
		t.Fatalf("fixture checkpoint: %q %v", ack, err)
	}
	if err := cmd.Process.Kill(); err != nil {
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
	file, err := os.Open(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	actual, digestErr := digestCheckpointedMain(f.ctx, file, info.Size(), pages, 64<<20)
	closeErr := file.Close()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	log, err = authority.open(nil, walAuthorityMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	acknowledged, err := log.Head(f.ctx)
	if err != nil || acknowledged.Sequence != 1 || acknowledged.Operation != operation || acknowledged.Prefix != prefix ||
		digestErr != nil || closeErr != nil || actual != acknowledged.Image || oldImage == acknowledged.Image {
		t.Fatalf("checkpoint/crash receipt mismatch: digest=%v close=%v authority=%v", digestErr, closeErr, err)
	}
	checkpointed, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := log.proveCheckpointedCapture(f.ctx, bytes.NewReader(checkpointed), int64(len(checkpointed)), 64<<20)
	if err != nil || proof.VerifyCheckpoint(f.ctx, actual.SHA256) != nil {
		t.Fatalf("checkpointed authority proof: %v", err)
	}
	if err := proof.VerifyCheckpoint(f.ctx, oldImage.SHA256); err == nil {
		t.Fatal("authority accepted pre-transaction checkpoint image")
	}
	db, err := sql.Open("sqlite", walOracleDSN(mainPath, "unix", "rw"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	var integrity string
	var count, lowest, highest int
	checkErr := db.QueryRowContext(f.ctx, "PRAGMA integrity_check").Scan(&integrity)
	if checkErr == nil {
		checkErr = db.QueryRowContext(f.ctx, "SELECT count(*),min(n),max(n) FROM fixture").Scan(&count, &lowest, &highest)
	}
	closeErr = db.Close()
	if checkErr != nil || closeErr != nil || integrity != "ok" || count != 2 || lowest != 7 || highest != 8 {
		t.Fatalf("SQLite recovery mismatch: count=%d min=%d max=%d err=%v close=%v", count, lowest, highest, checkErr, closeErr)
	}
}
