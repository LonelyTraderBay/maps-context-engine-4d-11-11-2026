//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"encoding/binary"
)

// walCommittedBoundary follows the documented SQLite WAL recovery scan.
// A stale or incomplete tail is allowed: recovery stops at its first invalid
// frame and exposes the last preceding commit. A trusted, independent proof
// must say which commit is expected; this scan cannot detect a lost final WAL.
// See https://www.sqlite.org/fileformat.html#walformat and
// https://www.sqlite.org/walformat.html#recovery.
func walCommittedBoundary(ctx context.Context, main, wal []byte) (uint32, uint32, error) {
	if ctx == nil || len(main) < 100 || len(wal) < 32 {
		return 0, 0, ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if string(main[:16]) != "SQLite format 3\x00" || main[18] != 2 || main[19] != 2 {
		return 0, 0, ErrStateUnsafe
	}
	mainPageSize := uint32(binary.BigEndian.Uint16(main[16:18]))
	if mainPageSize == 1 {
		mainPageSize = 65536
	}
	magic, version, pageSize := binary.BigEndian.Uint32(wal[:4]), binary.BigEndian.Uint32(wal[4:8]), binary.BigEndian.Uint32(wal[8:12])
	if (magic != 0x377f0682 && magic != 0x377f0683) || version != 3007000 || pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 || pageSize != mainPageSize {
		return 0, 0, ErrStateUnsafe
	}
	var order binary.ByteOrder = binary.LittleEndian
	if magic == 0x377f0683 {
		order = binary.BigEndian
	}
	sum1, sum2 := walChecksum(order, wal[:24], 0, 0)
	if sum1 != binary.BigEndian.Uint32(wal[24:28]) || sum2 != binary.BigEndian.Uint32(wal[28:32]) {
		return 0, 0, ErrStateUnsafe
	}
	frameSize := int(pageSize) + 24
	var lastFrame, lastPages uint32
	for offset, frameNo := 32, uint32(1); len(wal)-offset >= frameSize; offset, frameNo = offset+frameSize, frameNo+1 {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		frame := wal[offset : offset+frameSize]
		if binary.BigEndian.Uint32(frame[:4]) == 0 || !bytes.Equal(frame[8:16], wal[16:24]) {
			break
		}
		sum1, sum2 = walChecksum(order, frame[:8], sum1, sum2)
		sum1, sum2 = walChecksum(order, frame[24:], sum1, sum2)
		if sum1 != binary.BigEndian.Uint32(frame[16:20]) || sum2 != binary.BigEndian.Uint32(frame[20:24]) {
			break
		}
		if pages := binary.BigEndian.Uint32(frame[4:8]); pages != 0 {
			lastFrame, lastPages = frameNo, pages
		}
	}
	return lastFrame, lastPages, ctx.Err()
}

func walChecksum(order binary.ByteOrder, data []byte, sum1, sum2 uint32) (uint32, uint32) {
	for offset := 0; offset < len(data); offset += 8 {
		sum1 += order.Uint32(data[offset:offset+4]) + sum2
		sum2 += order.Uint32(data[offset+4:offset+8]) + sum1
	}
	return sum1, sum2
}
