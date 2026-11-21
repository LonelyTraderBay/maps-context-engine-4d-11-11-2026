//go:build linux || windows

package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

const walLogicalImageMaxBytes int64 = 2 << 30

// walLogicalImage is only a bounded image observation. A trusted independent
// authority must bind it to the latest acknowledged store/incarnation and
// sequence. It is not a receipt or permission to read or write state.
type walLogicalImage struct {
	PageSize  uint32
	PageCount uint32
	SHA256    [sha256.Size]byte
}

// walCommitPrefixReceipt is an experimental value supplied by an independent
// authority after a commit. The authority must authenticate the latest receipt
// for the exact store and incarnation; this value does not establish that
// authority or prove that an acknowledged receipt has not itself been lost.
type walCommitPrefixReceipt struct {
	MainSize        int64
	MainSHA256      [sha256.Size]byte
	WALPrefixSize   int64
	WALPrefixSHA256 [sha256.Size]byte
	WALCommitFrame  uint32
	WALCommitPages  uint32
}

// digestCheckpointedMain observes a complete post-checkpoint main image. Its
// authority, lifecycle and absence of relevant WAL/journal are caller proofs.
func digestCheckpointedMain(ctx context.Context, main io.ReaderAt, size int64, pages uint32, maxBytes int64) (walLogicalImage, error) {
	var empty walLogicalImage
	if ctx == nil || main == nil || size < 100 || pages == 0 || maxBytes < 512 || maxBytes > walLogicalImageMaxBytes {
		return empty, ErrStateUnsafe
	}
	if size > maxBytes {
		return empty, ErrStateLimit
	}
	header := make([]byte, 100)
	if err := readLogicalAt(ctx, main, header, 0); err != nil {
		return empty, err
	}
	pageSize, err := logicalMainPageSize(header)
	if err != nil {
		return empty, err
	}
	if uint64(pages)*uint64(pageSize) != uint64(size) {
		return empty, ErrStateChanged
	}
	digest, err := hashLogicalReader(ctx, main, size)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return walLogicalImage{PageSize: pageSize, PageCount: pages, SHA256: digest}, nil
}

func logicalMainPageSize(header []byte) (uint32, error) {
	if len(header) < 100 || string(header[:16]) != "SQLite format 3\x00" || header[18] != 2 || header[19] != 2 {
		return 0, ErrStateUnsafe
	}
	pageSize := uint32(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 {
		return 0, ErrStateUnsafe
	}
	return pageSize, nil
}

func readLogicalAt(ctx context.Context, source io.ReaderAt, output []byte, offset int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := source.ReadAt(output, offset)
	if n != len(output) || err != nil && err != io.EOF {
		return errors.Join(ErrStateChanged, err)
	}
	return ctx.Err()
}

func hashLogicalReader(ctx context.Context, source io.ReaderAt, size int64) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	digest := sha256.New()
	buffer := make([]byte, 65536)
	for offset := int64(0); offset < size; {
		length := min(int64(len(buffer)), size-offset)
		if err := readLogicalAt(ctx, source, buffer[:int(length)], offset); err != nil {
			return result, err
		}
		_, _ = digest.Write(buffer[:int(length)])
		offset += length
	}
	copy(result[:], digest.Sum(nil))
	if err := ctx.Err(); err != nil {
		return [sha256.Size]byte{}, err
	}
	return result, nil
}
