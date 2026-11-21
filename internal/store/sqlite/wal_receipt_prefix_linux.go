//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
)

// verifyWALCommitPrefix checks an acknowledged commit against one immutable
// main/WAL capture. A valid uncommitted or incomplete WAL tail may follow the
// receipt. A later committed frame is not covered by the receipt and is
// rejected for reconciliation. Checkpoints and changed main bytes are also
// rejected; neither outcome is a determination that a transaction rolled
// back. The caller must enforce quiescence while capturing, verify receipt
// authority/incarnation/freshness independently, and run SQLite integrity and
// policy checks before returning any result. This is not a product opener.
func verifyWALCommitPrefix(ctx context.Context, main, wal []byte, receipt walCommitPrefixReceipt) error {
	if ctx == nil || len(main) < 100 || len(wal) < 32 || receipt.WALCommitFrame == 0 || receipt.WALCommitPages == 0 {
		return ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if int64(len(wal)) > checkpointedSnapshotMaxBytes || int64(len(main)) > checkpointedSnapshotMaxBytes-int64(len(wal)) {
		return ErrStateLimit
	}
	pageSize := binary.BigEndian.Uint32(wal[8:12])
	if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 {
		return ErrStateUnsafe
	}
	prefixSize := uint64(32) + uint64(receipt.WALCommitFrame)*(uint64(pageSize)+24)
	if prefixSize > uint64(len(wal)) || receipt.MainSize != int64(len(main)) || receipt.WALPrefixSize != int64(prefixSize) {
		return ErrStateChanged
	}
	if sha256.Sum256(main) != receipt.MainSHA256 || sha256.Sum256(wal[:int(prefixSize)]) != receipt.WALPrefixSHA256 {
		return ErrStateChanged
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	frame, pages, err := walCommittedBoundary(ctx, main, wal[:int(prefixSize)])
	if err != nil {
		return err
	}
	if frame != receipt.WALCommitFrame || pages != receipt.WALCommitPages {
		return ErrStateChanged
	}
	frame, pages, err = walCommittedBoundary(ctx, main, wal)
	if err != nil {
		return err
	}
	if frame != receipt.WALCommitFrame || pages != receipt.WALCommitPages {
		return ErrStateChanged
	}
	return ctx.Err()
}
