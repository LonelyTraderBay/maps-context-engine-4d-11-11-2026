package model

import (
	"strings"
	"testing"
)

func TestRenameResultDigestTiedEditsAcrossWorktrees(t *testing.T) {
	i := "01234567-89ab-cdef-0123-456789abcdef"
	j := "11234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	a, b := "a", "b"
	one := ScopeMember{WorktreeID: i, ProfileID: h, GenerationID: i, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, ServiceID: &a, IndexIncarnation: i, RecordsIncarnation: i}
	two := one
	two.WorktreeID = j
	two.ServiceID = &b
	rev := int64(1)
	scope := ScopeVector{Kind: "federated", Members: []ScopeMember{one, two}, VectorDigest: "e1b25f0bd41a9b2a20e49f4853a29dfa2919da79d232e382aa9131bf845fadfe", GroupID: &i, GroupRevision: &rev, CatalogIncarnation: i, UnavailableMembers: []MemberUnavailable{}}
	req := RenameInitialRequest{Selector: PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: h, ByteOffset: 0}, NewName: "new", PreserveContracts: true}
	p := RenameFilePrecondition{WorktreeID: i, ProfileID: h, GenerationID: i, Path: "a.go", ContentHash: h}
	q := p
	q.WorktreeID = j
	e := ProposedEdit{SourceRef: SourceRef{WorktreeID: i, ProfileID: h, GenerationID: i, Path: "a.go", ContentHash: h, Range: ByteRange{StartByte: 0, EndByte: 1}}, NewText: "new"}
	f := e
	f.SourceRef.WorktreeID = j
	x, err := RenameResultDigest(scope, req, h, []RenameFilePrecondition{q, p}, []ProposedEdit{f, e, f}, []Gap{}, 32768)
	if err != nil || x != "617d9b16aec3efb881d154783dfc17a35e7e5fc762ae6924110ef28ea72ba7be" {
		t.Fatalf("digest %s %v", x, err)
	}
	reordered := scope
	reordered.Members = []ScopeMember{two, one}
	y, err := RenameResultDigest(reordered, req, h, []RenameFilePrecondition{p, q}, []ProposedEdit{e, f}, []Gap{}, 32768)
	if err != nil || x != y {
		t.Fatalf("unstable digest %s %s %v", x, y, err)
	}
	req.Scope = &ScopeRequest{PinnedVector: &scope}
	xPinned, err := RenameResultDigest(scope, req, h, []RenameFilePrecondition{p, q}, []ProposedEdit{e, f}, []Gap{}, 32768)
	if err != nil {
		t.Fatal(err)
	}
	req.Scope = &ScopeRequest{PinnedVector: &reordered}
	yPinned, err := RenameResultDigest(reordered, req, h, []RenameFilePrecondition{q, p}, []ProposedEdit{f, e}, []Gap{}, 32768)
	if err != nil || xPinned != yPinned {
		t.Fatalf("nested pinned vector order changed digest: %v", err)
	}
	changed := reordered
	changed.Members = append([]ScopeMember{}, reordered.Members...)
	changed.Members[0].RecordEpoch++
	changed.VectorDigest, err = ComputeScopeVectorDigest(changed, 32768)
	if err != nil {
		t.Fatal(err)
	}
	req.Scope = &ScopeRequest{PinnedVector: &changed}
	if _, err = RenameResultDigest(scope, req, h, []RenameFilePrecondition{p, q}, []ProposedEdit{e, f}, []Gap{}, 32768); err == nil {
		t.Fatal("accepted different pinned request vector")
	}
}
