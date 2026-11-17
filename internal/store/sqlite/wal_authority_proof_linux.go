//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"io"
)

// A capture proof connects a journal receipt to the existing read-only recovery
// views. It does not authorize a root or establish capture immutability. The
// owner must prove those conditions, including quiescence and isolated file
// descriptors, before supplying ReaderAt values. Neither constructor writes.
type walAuthorityCaptureProof struct {
	log          *walAuthorityLog
	receipt      walAuthorityReceipt
	pair         WALSnapshotPair
	checkpointed bool
}

func (l *walAuthorityLog) proveWALCapture(ctx context.Context, main io.ReaderAt, mainSize int64, wal io.ReaderAt, walSize int64, maxBytes int64) (*walAuthorityCaptureProof, error) {
	head, err := l.Head(ctx)
	if err != nil {
		return nil, err
	}
	image, err := digestCommittedWALImage(ctx, main, mainSize, wal, walSize, head.Prefix, maxBytes)
	if err != nil {
		return nil, err
	}
	if image != head.Image {
		return nil, ErrStateChanged
	}
	walHash, err := hashLogicalReader(ctx, wal, walSize)
	if err != nil {
		return nil, err
	}
	proof := &walAuthorityCaptureProof{log: l, receipt: head, pair: WALSnapshotPair{
		MainSize: mainSize, WALSize: walSize, MainSHA256: head.Prefix.MainSHA256, WALSHA256: walHash,
		WALCommitFrame: head.Prefix.WALCommitFrame, WALCommitPages: head.Prefix.WALCommitPages,
	}}
	if err := proof.current(ctx); err != nil {
		return nil, err
	}
	return proof, nil
}

// The owner must additionally prove a complete checkpoint and absence of any
// relevant WAL/journal. Missing files alone never establish that condition.
func (l *walAuthorityLog) proveCheckpointedCapture(ctx context.Context, main io.ReaderAt, size, maxBytes int64) (*walAuthorityCaptureProof, error) {
	head, err := l.Head(ctx)
	if err != nil {
		return nil, err
	}
	image, err := digestCheckpointedMain(ctx, main, size, head.Image.PageCount, maxBytes)
	if err != nil {
		return nil, err
	}
	if image != head.Image {
		return nil, ErrStateChanged
	}
	proof := &walAuthorityCaptureProof{log: l, receipt: head, checkpointed: true}
	if err := proof.current(ctx); err != nil {
		return nil, err
	}
	return proof, nil
}

func (p *walAuthorityCaptureProof) current(ctx context.Context) error {
	if p == nil || p.log == nil {
		return ErrStateUnsafe
	}
	head, err := p.log.Head(ctx)
	if err != nil {
		return err
	}
	if head != p.receipt {
		return ErrStateChanged
	}
	return nil
}

func (p *walAuthorityCaptureProof) VerifyWAL(ctx context.Context, pair WALSnapshotPair) error {
	if p == nil || p.checkpointed || p.pair != pair {
		return ErrStateChanged
	}
	return p.current(ctx)
}

func (p *walAuthorityCaptureProof) VerifyCheckpoint(ctx context.Context, digest [sha256.Size]byte) error {
	if p == nil || !p.checkpointed || digest != p.receipt.Image.SHA256 {
		return ErrStateChanged
	}
	return p.current(ctx)
}
