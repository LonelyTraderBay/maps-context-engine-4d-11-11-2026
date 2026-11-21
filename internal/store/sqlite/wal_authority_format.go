//go:build linux || windows

package sqlite

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

const (
	walAuthorityRecordSize = 512
	walAuthorityMaxBytes   = 16 << 20
	walAuthorityName       = "wal-authority.log"
	walAuthorityBootstrap  = byte(1)
	walHeadName            = "wal-head.log"
	walHeadRecordSize      = 192
	walHeadMaxBytes        = walAuthorityMaxBytes / walAuthorityRecordSize * walHeadRecordSize
)

// This identity and key must come from a trusted owner, never from the journal
// being authenticated. Store binds the database kind and worktree/catalog ID;
// Incarnation and Authority distinguish replacements from the previous store.
type walAuthorityIdentity struct {
	Authority   [16]byte
	Store       [32]byte
	Incarnation [16]byte
}

type walAuthorityReceipt struct {
	Sequence  uint64
	Operation [32]byte
	Image     walLogicalImage
	Prefix    walCommitPrefixReceipt
}

type walAuthorityRecord struct {
	kind byte
	walAuthorityReceipt
}

type walJournalPosition struct {
	Size   int64
	SHA256 [32]byte
}

func encodeWALAuthorityRecord(identity walAuthorityIdentity, key, chain [32]byte, r walAuthorityRecord) (raw [walAuthorityRecordSize]byte) {
	copy(raw[:8], "MCEWAL1\x00")
	raw[8] = r.kind
	binary.BigEndian.PutUint64(raw[16:24], r.Sequence)
	copy(raw[24:40], identity.Authority[:])
	copy(raw[40:72], identity.Store[:])
	copy(raw[72:88], identity.Incarnation[:])
	copy(raw[88:120], chain[:])
	copy(raw[120:152], r.Operation[:])
	binary.BigEndian.PutUint32(raw[152:156], r.Image.PageSize)
	binary.BigEndian.PutUint32(raw[156:160], r.Image.PageCount)
	copy(raw[160:192], r.Image.SHA256[:])
	binary.BigEndian.PutUint64(raw[192:200], uint64(r.Prefix.MainSize))
	copy(raw[200:232], r.Prefix.MainSHA256[:])
	binary.BigEndian.PutUint64(raw[232:240], uint64(r.Prefix.WALPrefixSize))
	copy(raw[240:272], r.Prefix.WALPrefixSHA256[:])
	binary.BigEndian.PutUint32(raw[272:276], r.Prefix.WALCommitFrame)
	binary.BigEndian.PutUint32(raw[276:280], r.Prefix.WALCommitPages)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(raw[:480])
	copy(raw[480:], mac.Sum(nil))
	return raw
}

func encodeWALHeadRecord(identity walAuthorityIdentity, key, chain [32]byte, next walJournalPosition) []byte {
	raw := make([]byte, walHeadRecordSize)
	copy(raw[:8], "MCEHEAD1")
	binary.BigEndian.PutUint64(raw[8:16], uint64(next.Size/walAuthorityRecordSize))
	copy(raw[16:32], identity.Authority[:])
	copy(raw[32:64], identity.Store[:])
	copy(raw[64:80], identity.Incarnation[:])
	binary.BigEndian.PutUint64(raw[80:88], uint64(next.Size))
	copy(raw[88:120], next.SHA256[:])
	copy(raw[120:152], chain[:])
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte("MCE4D-WAL-HEAD1\x00"))
	_, _ = mac.Write(raw[:160])
	copy(raw[160:], mac.Sum(nil))
	return raw
}
