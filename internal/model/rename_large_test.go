package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenameResultDigestHandles1025EditsWithoutMutation(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	m := ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: id, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, IndexIncarnation: id, RecordsIncarnation: id}
	scope := ScopeVector{Kind: "single", Members: []ScopeMember{m}, CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = ComputeScopeVectorDigest(scope, 32768)
	if err != nil {
		t.Fatal(err)
	}
	req := RenameInitialRequest{Selector: PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: h, ByteOffset: 0}, NewName: "new", PreserveContracts: true}
	pre := RenameFilePrecondition{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: h}
	preB := pre
	preB.Path = "b.go"
	edits := make([]ProposedEdit, 1025)
	for i := range edits {
		start := int64((i / 2) * 2)
		path := "a.go"
		if i%2 == 1 {
			path = "b.go"
		}
		edits[i] = ProposedEdit{SourceRef: SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: path, ContentHash: h, Range: ByteRange{StartByte: start, EndByte: start + 1}}, NewText: fmt.Sprintf("n%d", i)}
	}
	reversed := append([]ProposedEdit{}, edits...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	a, err := RenameResultDigest(scope, req, h, []RenameFilePrecondition{pre, preB}, edits, []Gap{}, 4194304)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenameResultDigest(scope, req, h, []RenameFilePrecondition{preB, pre}, reversed, []Gap{}, 4194304)
	if err != nil || a != b {
		t.Fatalf("unstable large rename %v", err)
	}
	if reversed[0].SourceRef.Range.StartByte != 1024 {
		t.Fatal("mutated input edits")
	}
}
