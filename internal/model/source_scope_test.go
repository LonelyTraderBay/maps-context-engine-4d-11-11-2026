package model

import (
	"errors"
	"strings"
	"testing"
)

func TestPinnedSourceServiceAmbiguity(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	a, b := "a", "b"
	m := ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: id, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, ServiceID: &a, IndexIncarnation: id, RecordsIncarnation: id}
	n := m
	n.ServiceID = &b
	rev := int64(1)
	scope := ScopeVector{Kind: "federated", Members: []ScopeMember{m, n}, GroupID: &id, GroupRevision: &rev, CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = ComputeScopeVectorDigest(scope, 16384)
	if err != nil {
		t.Fatal(err)
	}
	ref := SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: h, Range: ByteRange{StartByte: 0, EndByte: 1}}
	if _, err = MatchPinnedSource(scope, ref, nil, 16384); !errors.Is(err, ErrAmbiguousMember) {
		t.Fatalf("expected ambiguity: %v", err)
	}
	if _, err = MatchPinnedSource(scope, ref, &b, 16384); err != nil {
		t.Fatal(err)
	}
	other := "other"
	if _, err = MatchPinnedSource(scope, ref, &other, 16384); !errors.Is(err, ErrMemberNotInScope) {
		t.Fatalf("expected no match: %v", err)
	}
	scope.VectorDigest = h
	if _, err = MatchPinnedSource(scope, ref, &b, 16384); err == nil {
		t.Fatal("accepted forged vector digest")
	}
}
