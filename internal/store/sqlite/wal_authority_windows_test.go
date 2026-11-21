package sqlite

import (
	"context"
	"crypto/sha256"
	"testing"
)

func TestWindowsWALAuthorityAcceptsExistingStagedGenesis(t *testing.T) {
	identity := walAuthorityIdentity{Authority: [16]byte{1}, Store: [32]byte{2}, Incarnation: [16]byte{3}}
	key := [32]byte{4}
	image := walLogicalImage{PageSize: 4096, PageCount: 1, SHA256: [32]byte{5}}
	// The E782 packaged controller encoded the offline genesis this way.
	legacy := encodeWALAuthorityRecord(identity, key, [32]byte{}, walAuthorityRecord{
		kind: walAuthorityBootstrap, walAuthorityReceipt: walAuthorityReceipt{Sequence: 1, Image: image},
	})
	legacyHash := sha256.Sum256(legacy[:])
	head := encodeWALHeadRecord(identity, key, [32]byte{}, walJournalPosition{Size: walAuthorityRecordSize, SHA256: legacyHash})
	state, err := replayWindowsWALAuthority(context.Background(), identity, key, legacy[:], head)
	if err != nil || state.head.Sequence != 0 || state.head.Image != image || state.receiptChain != legacyHash {
		t.Fatalf("existing staged genesis was not recognized: %v", err)
	}
}
