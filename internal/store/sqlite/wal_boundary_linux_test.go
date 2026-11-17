//go:build linux

package sqlite

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func readCommittedFixtureRow(f walSnapshotFixture) error {
	snapshot, err := f.open(f.proof)
	if err != nil {
		return err
	}
	defer snapshot.Close()
	return snapshot.Read(f.ctx, func(query *SnapshotQuery) error {
		rows, err := query.Query("SELECT n FROM fixture")
		if err != nil {
			return err
		}
		if !rows.Next() {
			return errors.New("committed row missing")
		}
		var value int
		if err := rows.Scan(&value); err != nil || value != 7 {
			return fmt.Errorf("committed value %d: %v", value, err)
		}
		if rows.Next() {
			return errors.New("unexpected extra row")
		}
		return rows.Err()
	})
}

func TestWALBoundarySQLitePageSizes(t *testing.T) {
	for _, pageSize := range []int{512, 1024, 4096, 65536} {
		t.Run(fmt.Sprint(pageSize), func(t *testing.T) {
			f := newWALSnapshotFixtureWithPageSize(t, pageSize)
			main, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
			if err != nil || len(main) < 18 {
				t.Fatalf("fixture main database: %v", err)
			}
			actualPageSize := int(binary.BigEndian.Uint16(main[16:18]))
			if actualPageSize == 1 {
				actualPageSize = 65536
			}
			if actualPageSize != pageSize {
				t.Fatalf("fixture page size %d, want %d", actualPageSize, pageSize)
			}
			if err := readCommittedFixtureRow(f); err != nil {
				t.Fatalf("SQLite boundary or row differs at page size %d: %v", pageSize, err)
			}
		})
	}
}

func TestWALBoundaryIgnoresIncompleteAndStaleTail(t *testing.T) {
	for _, kind := range []string{"partial", "stale"} {
		t.Run(kind, func(t *testing.T) {
			f := newWALSnapshotFixture(t)
			path := filepath.Join(f.state, "catalog.sqlite-wal")
			wal, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
			if len(wal) < 32+pageSize+24 {
				t.Fatal("fixture WAL has no complete frame")
			}
			if kind == "partial" {
				wal = append(wal, []byte{1, 2, 3, 4, 5}...)
			} else {
				stale := append([]byte(nil), wal[32:32+pageSize+24]...)
				stale[8] ^= 1 // Different checkpoint salt: an old frame.
				wal = append(wal, stale...)
			}
			if err := os.WriteFile(path, wal, 0600); err != nil {
				t.Fatal(err)
			}
			// The frame/page boundary remains the writer's earlier observation;
			// the exact expected fixture bytes include the harmless tail.
			f.pair.WALSize = int64(len(wal))
			f.pair.WALSHA256 = sha256.Sum256(wal)
			if err := readCommittedFixtureRow(f); err != nil {
				t.Fatalf("SQLite rejected %s tail: %v", kind, err)
			}
		})
	}
}

func TestWALBoundaryIgnoresUncommittedTransaction(t *testing.T) {
	f := newWALSnapshotFixtureWithOptions(t, 4096, true)
	wal, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
	if err != nil || len(wal) < 32 {
		t.Fatalf("fixture WAL: %v", err)
	}
	pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
	framesOnDisk := (len(wal) - 32) / (pageSize + 24)
	if framesOnDisk <= int(f.pair.WALCommitFrame) {
		t.Fatalf("fixture did not spill uncommitted frames: disk=%d committed=%d", framesOnDisk, f.pair.WALCommitFrame)
	}
	if err := readCommittedFixtureRow(f); err != nil {
		t.Fatalf("uncommitted transaction affected recovery: %v", err)
	}
}

func TestWALBoundaryBigEndianChecksum(t *testing.T) {
	f := newWALSnapshotFixture(t)
	path := filepath.Join(f.state, "catalog.sqlite-wal")
	wal, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
	if len(wal) < 32+pageSize+24 || (len(wal)-32)%(pageSize+24) != 0 {
		t.Fatal("fixture WAL has incomplete frames")
	}
	// Re-encode the same fixture with the other SQLite checksum byte order.
	// SQLite's recovery is the independent oracle that must still read row 7.
	binary.BigEndian.PutUint32(wal[:4], 0x377f0683)
	var sum1, sum2 uint32
	update := func(data []byte) {
		for i := 0; i < len(data); i += 8 {
			sum1 = sum1 + binary.BigEndian.Uint32(data[i:i+4]) + sum2
			sum2 = sum2 + binary.BigEndian.Uint32(data[i+4:i+8]) + sum1
		}
	}
	update(wal[:24])
	binary.BigEndian.PutUint32(wal[24:28], sum1)
	binary.BigEndian.PutUint32(wal[28:32], sum2)
	for offset := 32; offset < len(wal); offset += pageSize + 24 {
		frame := wal[offset : offset+pageSize+24]
		update(frame[:8])
		update(frame[24:])
		binary.BigEndian.PutUint32(frame[16:20], sum1)
		binary.BigEndian.PutUint32(frame[20:24], sum2)
	}
	if err := os.WriteFile(path, wal, 0600); err != nil {
		t.Fatal(err)
	}
	f.pair.WALSHA256 = sha256.Sum256(wal)
	if err := readCommittedFixtureRow(f); err != nil {
		t.Fatalf("SQLite rejected big-endian WAL: %v", err)
	}
}
