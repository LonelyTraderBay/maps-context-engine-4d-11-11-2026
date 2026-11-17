//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// captureNativeWALImage runs ONLY in a dedicated capture process, while its
// trusted parent holds the short authority gate. It uses a shared DB lifecycle;
// the gate excludes writes/checkpoints/closes, not concurrent pinned readers.
// Its result is an observation: the parent still authenticates the latest head.
func captureNativeWALImage(ctx context.Context, domain *stateDomain, lease *runtime.Lifecycle, scope, name string) (out nativeWALCapture, resultErr error) {
	if domain == nil || lease == nil || domain.Check(ctx) != nil {
		return out, ErrStateUnsafe
	}
	if err := lease.CheckRoot(ctx, domain.root, scope, ports.SharedLease); err != nil {
		return out, err
	}
	observation, err := domain.Observe(ctx, lease, scope, name, walLogicalImageMaxBytes)
	if err != nil {
		return out, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, observation.Close())
		if resultErr != nil {
			out = nativeWALCapture{}
		}
	}()
	if observation.files[3].file != nil {
		return out, errWALAuthorityRecovery
	}
	main, mainSize, err := openWALSnapshotSource(domain.root, observation.files[0])
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, main.Close()) }()
	var header [100]byte
	if err := readLogicalAt(ctx, main, header[:], 0); err != nil {
		return out, err
	}
	var wal io.ReaderAt
	var walSize int64
	if observation.files[1].file != nil {
		f, size, err := openWALSnapshotSource(domain.root, observation.files[1])
		if err != nil {
			return out, err
		}
		defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
		wal, walSize = f, size
	}
	if walSize > 0 {
		out.Prefix, err = observeNativeWALPrefix(ctx, main, mainSize, wal, walSize)
		if err != nil {
			return out, err
		}
	}
	if out.Prefix.WALCommitFrame == 0 {
		out.Image, err = digestCheckpointedMain(ctx, main, mainSize, binary.BigEndian.Uint32(header[28:32]), walLogicalImageMaxBytes)
	} else {
		out.Image, err = digestCommittedWALImage(ctx, main, mainSize, wal, walSize, out.Prefix, walLogicalImageMaxBytes)
	}
	if err != nil {
		return out, err
	}
	return out, observation.Recheck(ctx)
}

// Scan the documented SQLite WAL checksums with one frame buffer. No full DB
// or WAL allocation is needed. A stale/incomplete tail stops the scan; only a
// preceding complete commit contributes to the returned prefix. This cannot
// prove that a final commit is missing: the independent parent head does that.
func observeNativeWALPrefix(ctx context.Context, main io.ReaderAt, mainSize int64, wal io.ReaderAt, walSize int64) (out walCommitPrefixReceipt, resultErr error) {
	if mainSize < 100 || walSize < 32 || mainSize > walLogicalImageMaxBytes || walSize > walLogicalImageMaxBytes-mainSize {
		return out, ErrStateLimit
	}
	var mainHeader [100]byte
	if err := readLogicalAt(ctx, main, mainHeader[:], 0); err != nil {
		return out, err
	}
	pageSize, err := logicalMainPageSize(mainHeader[:])
	if err != nil {
		return out, err
	}
	var header [32]byte
	if err := readLogicalAt(ctx, wal, header[:], 0); err != nil {
		return out, err
	}
	magic := binary.BigEndian.Uint32(header[:4])
	if (magic != 0x377f0682 && magic != 0x377f0683) || binary.BigEndian.Uint32(header[4:8]) != 3007000 || binary.BigEndian.Uint32(header[8:12]) != pageSize {
		return out, ErrStateUnsafe
	}
	var order binary.ByteOrder = binary.LittleEndian
	if magic == 0x377f0683 {
		order = binary.BigEndian
	}
	sum1, sum2 := walChecksum(order, header[:24], 0, 0)
	if sum1 != binary.BigEndian.Uint32(header[24:28]) || sum2 != binary.BigEndian.Uint32(header[28:32]) {
		return out, ErrStateUnsafe
	}
	hash := sha256.New()
	_, _ = hash.Write(header[:])
	frameSize := int64(pageSize) + 24
	frame := make([]byte, int(frameSize))
	for offset, number := int64(32), uint32(1); offset <= walSize-frameSize; offset, number = offset+frameSize, number+1 {
		if err := readLogicalAt(ctx, wal, frame, offset); err != nil {
			return out, err
		}
		if binary.BigEndian.Uint32(frame[:4]) == 0 || !bytes.Equal(frame[8:16], header[16:24]) {
			break
		}
		sum1, sum2 = walChecksum(order, frame[:8], sum1, sum2)
		sum1, sum2 = walChecksum(order, frame[24:], sum1, sum2)
		if sum1 != binary.BigEndian.Uint32(frame[16:20]) || sum2 != binary.BigEndian.Uint32(frame[20:24]) {
			break
		}
		_, _ = hash.Write(frame)
		if pages := binary.BigEndian.Uint32(frame[4:8]); pages != 0 {
			out.WALCommitFrame, out.WALCommitPages = number, pages
			out.WALPrefixSize = offset + frameSize
			copy(out.WALPrefixSHA256[:], hash.Sum(nil))
		}
	}
	if out.WALCommitFrame == 0 {
		return walCommitPrefixReceipt{}, ctx.Err()
	}
	out.MainSize = mainSize
	out.MainSHA256, err = hashLogicalReader(ctx, main, mainSize)
	return out, err
}
