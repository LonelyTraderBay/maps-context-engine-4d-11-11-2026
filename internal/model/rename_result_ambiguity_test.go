package model

import (
	"errors"
	"strings"
	"testing"
)

func TestRenameDigestRejectsAmbiguousService(t *testing.T) {
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
	req := RenameInitialRequest{Selector: PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: h, ByteOffset: 0}, NewName: "new", PreserveContracts: true}
	p := RenameFilePrecondition{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: h}
	_, err = RenameResultDigest(scope, req, h, []RenameFilePrecondition{p}, []ProposedEdit{}, []Gap{}, 16384)
	if !errors.Is(err, ErrAmbiguousMember) {
		t.Fatalf("expected ambiguous member: %v", err)
	}
}
