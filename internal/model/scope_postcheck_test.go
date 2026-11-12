package model

import (
	"errors"
	"strings"
	"testing"
)

func TestPinnedScopePostcheck(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	m := ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: id, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, IndexIncarnation: id, RecordsIncarnation: id}
	a := ScopeVector{Kind: "single", Members: []ScopeMember{m}, CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{}}
	var err error
	a.VectorDigest, err = ComputeScopeVectorDigest(a, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err = ComparePinnedScope(a, a, 16384); err != nil {
		t.Fatal(err)
	}
	b := a
	b.Members = append([]ScopeMember{}, a.Members...)
	b.Members[0].RecordEpoch++
	b.VectorDigest, err = ComputeScopeVectorDigest(b, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err = ComparePinnedScope(a, b, 16384); !errors.Is(err, ErrScopeChanged) {
		t.Fatalf("failed to detect record change: %v", err)
	}
	b.VectorDigest = a.VectorDigest
	if err = ComparePinnedScope(a, b, 16384); err == nil {
		t.Fatal("accepted forged digest")
	}
}
