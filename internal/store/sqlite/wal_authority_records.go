//go:build linux || windows

package sqlite

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
)

const (
	walAuthorityPrepare        = byte(2)
	walAuthorityCommit         = byte(3)
	walAuthorityAbort          = byte(4)
	walAuthorityCommitQuiesced = byte(5)
)

var (
	errWALAuthorityRecovery = errors.New("WAL authority requires reconciliation")
	errWALAuthorityPending  = errors.New("WAL transaction outcome is pending")
	errWALAuthorityBusy     = errors.New("WAL authority is in use")
)

type walReceiptState struct {
	head    walAuthorityReceipt
	pending *walAuthorityReceipt
}

func validAuthorityImage(image walLogicalImage) bool {
	return image.PageSize >= 512 && image.PageSize <= 65536 && image.PageSize&(image.PageSize-1) == 0 && image.PageCount > 0 &&
		uint64(image.PageSize)*uint64(image.PageCount) <= uint64(walLogicalImageMaxBytes) && image.SHA256 != [32]byte{}
}

func validAuthorityPrefix(prefix walCommitPrefixReceipt, image walLogicalImage) bool {
	return prefix.MainSize >= 100 && prefix.MainSize%int64(image.PageSize) == 0 && prefix.WALCommitFrame > 0 &&
		prefix.WALCommitPages == image.PageCount && prefix.WALPrefixSize >= 32 &&
		uint64(prefix.WALPrefixSize) == 32+uint64(prefix.WALCommitFrame)*(uint64(image.PageSize)+24) &&
		prefix.MainSize <= walLogicalImageMaxBytes-prefix.WALPrefixSize && prefix.MainSHA256 != [32]byte{} && prefix.WALPrefixSHA256 != [32]byte{}
}

func (l *walReceiptState) apply(r walAuthorityRecord, first bool) error {
	if first {
		if r.kind != walAuthorityBootstrap || r.Sequence != 0 || r.Operation != [32]byte{} || r.Prefix != (walCommitPrefixReceipt{}) || !validAuthorityImage(r.Image) {
			return errWALAuthorityRecovery
		}
		l.head = r.walAuthorityReceipt
		return nil
	}
	if r.Sequence == 0 || l.head.Sequence == math.MaxUint64 || r.Sequence != l.head.Sequence+1 || r.Operation == [32]byte{} || !validAuthorityImage(r.Image) {
		return errWALAuthorityRecovery
	}
	switch r.kind {
	case walAuthorityPrepare:
		if l.pending != nil || r.Image != l.head.Image || r.Prefix != (walCommitPrefixReceipt{}) {
			return errWALAuthorityRecovery
		}
		p := r.walAuthorityReceipt
		l.pending = &p
	case walAuthorityCommit, walAuthorityCommitQuiesced, walAuthorityAbort:
		if l.pending == nil || r.Operation != l.pending.Operation || r.Sequence != l.pending.Sequence {
			return errWALAuthorityRecovery
		}
		if r.kind == walAuthorityCommit && !validAuthorityPrefix(r.Prefix, r.Image) ||
			r.kind == walAuthorityCommitQuiesced && r.Prefix != (walCommitPrefixReceipt{}) ||
			r.kind == walAuthorityAbort && (r.Image != l.head.Image || r.Prefix != l.head.Prefix) {
			return errWALAuthorityRecovery
		}
		l.head = r.walAuthorityReceipt
		l.pending = nil
	default:
		return errWALAuthorityRecovery
	}
	return nil
}

func decodeWALAuthorityRecord(identity walAuthorityIdentity, key, chain [32]byte, raw []byte) (walAuthorityRecord, error) {
	var r walAuthorityRecord
	if len(raw) != walAuthorityRecordSize || string(raw[:8]) != "MCEWAL1\x00" || !bytes.Equal(raw[9:16], make([]byte, 7)) ||
		!bytes.Equal(raw[280:480], make([]byte, 200)) || !bytes.Equal(raw[24:40], identity.Authority[:]) ||
		!bytes.Equal(raw[40:72], identity.Store[:]) || !bytes.Equal(raw[72:88], identity.Incarnation[:]) || !bytes.Equal(raw[88:120], chain[:]) {
		return r, errWALAuthorityRecovery
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(raw[:480])
	if !hmac.Equal(raw[480:], mac.Sum(nil)) {
		return r, errWALAuthorityRecovery
	}
	r.kind, r.Sequence = raw[8], binary.BigEndian.Uint64(raw[16:24])
	copy(r.Operation[:], raw[120:152])
	r.Image.PageSize, r.Image.PageCount = binary.BigEndian.Uint32(raw[152:156]), binary.BigEndian.Uint32(raw[156:160])
	copy(r.Image.SHA256[:], raw[160:192])
	r.Prefix.MainSize = int64(binary.BigEndian.Uint64(raw[192:200]))
	copy(r.Prefix.MainSHA256[:], raw[200:232])
	r.Prefix.WALPrefixSize = int64(binary.BigEndian.Uint64(raw[232:240]))
	copy(r.Prefix.WALPrefixSHA256[:], raw[240:272])
	r.Prefix.WALCommitFrame, r.Prefix.WALCommitPages = binary.BigEndian.Uint32(raw[272:276]), binary.BigEndian.Uint32(raw[276:280])
	return r, nil
}
