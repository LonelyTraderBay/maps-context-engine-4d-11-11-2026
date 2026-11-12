package model

import (
	"errors"
	"strings"
	"testing"
)

func TestPinnedUnavailableMemberCannotSilentlyUpgrade(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	svc := "s"
	rev := int64(1)
	old := ScopeVector{Kind: "federated", Members: []ScopeMember{}, GroupID: &id, GroupRevision: &rev, CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{{ServiceID: &svc, WorktreeID: &id, ProfileID: &h, Status: "index_missing", ReasonCode: "NO_INDEX"}}}
	var err error
	old.VectorDigest, err = ComputeScopeVectorDigest(old, 16384)
	if err != nil {
		t.Fatal(err)
	}
	m := ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: id, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, ServiceID: &svc, IndexIncarnation: id, RecordsIncarnation: id}
	next := old
	next.Members = []ScopeMember{m}
	next.UnavailableMembers = []MemberUnavailable{}
	next.VectorDigest, err = ComputeScopeVectorDigest(next, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err = ComparePinnedScope(old, next, 16384); !errors.Is(err, ErrScopeChanged) {
		t.Fatalf("accepted new member: %v", err)
	}
}
