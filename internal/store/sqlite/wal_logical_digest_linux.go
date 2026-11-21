//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
)

// digestCommittedWALImage hashes the exact logical pages at the independently
// specified commit. The inputs must be immutable for the entire call and
// belong to one authorized, quiescent capture. The caller must independently
// authenticate the latest receipt, store incarnation and lifecycle lease.
// An incomplete/uncommitted tail is ignored; a newer commit is rejected. This
// helper never opens or modifies files and does not establish crash durability.
// On POSIX, closing an ordinary descriptor for an SQLite inode can release the
// process's locks; the owner must isolate such reads or exclude live SQLite
// handles. ReadAt has no context parameter and a blocked call cannot be interrupted.
func digestCommittedWALImage(ctx context.Context, main io.ReaderAt, mainSize int64, wal io.ReaderAt, walSize int64, receipt walCommitPrefixReceipt, maxBytes int64) (walLogicalImage, error) {
	var empty walLogicalImage
	if ctx == nil || main == nil || wal == nil || maxBytes < 512 || maxBytes > walLogicalImageMaxBytes || mainSize < 100 || walSize < 32 || receipt.WALCommitFrame == 0 || receipt.WALCommitPages == 0 {
		return empty, ErrStateUnsafe
	}
	if mainSize > maxBytes || walSize > maxBytes-mainSize {
		return empty, ErrStateLimit
	}
	if receipt.MainSize != mainSize {
		return empty, ErrStateChanged
	}
	mainHeader := make([]byte, 100)
	if err := readLogicalAt(ctx, main, mainHeader, 0); err != nil {
		return empty, err
	}
	pageSize, err := logicalMainPageSize(mainHeader)
	if err != nil || mainSize%int64(pageSize) != 0 {
		return empty, ErrStateUnsafe
	}
	imageSize := uint64(receipt.WALCommitPages) * uint64(pageSize)
	if imageSize > uint64(maxBytes) || imageSize == 0 {
		return empty, ErrStateLimit
	}
	frameSize := int64(pageSize) + 24
	prefixSize := uint64(32) + uint64(receipt.WALCommitFrame)*uint64(frameSize)
	if prefixSize > uint64(walSize) || receipt.WALPrefixSize < 0 || uint64(receipt.WALPrefixSize) != prefixSize {
		return empty, ErrStateChanged
	}
	mainDigest, err := hashLogicalReader(ctx, main, mainSize)
	if err != nil {
		return empty, err
	}
	if mainDigest != receipt.MainSHA256 {
		return empty, ErrStateChanged
	}
	header := make([]byte, 32)
	if err := readLogicalAt(ctx, wal, header, 0); err != nil {
		return empty, err
	}
	magic := binary.BigEndian.Uint32(header[:4])
	if (magic != 0x377f0682 && magic != 0x377f0683) || binary.BigEndian.Uint32(header[4:8]) != 3007000 || binary.BigEndian.Uint32(header[8:12]) != pageSize {
		return empty, ErrStateUnsafe
	}
	var order binary.ByteOrder = binary.LittleEndian
	if magic == 0x377f0683 {
		order = binary.BigEndian
	}
	sum1, sum2 := walChecksum(order, header[:24], 0, 0)
	if sum1 != binary.BigEndian.Uint32(header[24:28]) || sum2 != binary.BigEndian.Uint32(header[28:32]) {
		return empty, ErrStateUnsafe
	}
	// At most 2 GiB / 512 pages * 4 bytes = 16 MiB of frame indexes.
	lastForPage := make([]uint32, int(receipt.WALCommitPages))
	frame := make([]byte, int(frameSize))
	prefixHash := sha256.New()
	_, _ = prefixHash.Write(header)
	frames := (walSize - 32) / frameSize
	var lastCommit, lastPages uint32
	for i := int64(1); i <= frames; i++ {
		if err := readLogicalAt(ctx, wal, frame, 32+(i-1)*frameSize); err != nil {
			return empty, err
		}
		page := binary.BigEndian.Uint32(frame[:4])
		if page == 0 || !bytes.Equal(frame[8:16], header[16:24]) {
			break
		}
		sum1, sum2 = walChecksum(order, frame[:8], sum1, sum2)
		sum1, sum2 = walChecksum(order, frame[24:], sum1, sum2)
		if sum1 != binary.BigEndian.Uint32(frame[16:20]) || sum2 != binary.BigEndian.Uint32(frame[20:24]) {
			break
		}
		if i <= int64(receipt.WALCommitFrame) {
			_, _ = prefixHash.Write(frame)
			if page <= receipt.WALCommitPages {
				lastForPage[page-1] = uint32(i)
			}
		}
		if pages := binary.BigEndian.Uint32(frame[4:8]); pages != 0 {
			lastCommit, lastPages = uint32(i), pages
		}
	}
	if lastCommit != receipt.WALCommitFrame || lastPages != receipt.WALCommitPages || !bytes.Equal(prefixHash.Sum(nil), receipt.WALPrefixSHA256[:]) {
		return empty, ErrStateChanged
	}
	digest := sha256.New()
	page := make([]byte, int(pageSize))
	for i, frameNo := range lastForPage {
		var source io.ReaderAt = main
		offset := int64(i) * int64(pageSize)
		if frameNo != 0 {
			source = wal
			offset = 32 + (int64(frameNo)-1)*frameSize + 24
		} else if offset > mainSize-int64(pageSize) {
			return empty, ErrStateChanged
		}
		if err := readLogicalAt(ctx, source, page, offset); err != nil {
			return empty, err
		}
		_, _ = digest.Write(page)
	}
	var result walLogicalImage
	result.PageSize, result.PageCount = pageSize, receipt.WALCommitPages
	copy(result.SHA256[:], digest.Sum(nil))
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
