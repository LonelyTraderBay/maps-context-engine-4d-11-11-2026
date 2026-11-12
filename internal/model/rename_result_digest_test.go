package model

import (
	"strings"
	"testing"
)

func TestRenameResultDigestDeterministic(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	m := ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: id, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, IndexIncarnation: id, RecordsIncarnation: id}
	scope := ScopeVector{Kind: "single", Members: []ScopeMember{m}, VectorDigest: "69b8752c1cab39f53ab6c24a9c985f6190a04875a9282a5dc69f4f93c45a990d", CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{}}
	initial := RenameInitialRequest{Selector: PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: h, ByteOffset: 0}, NewName: "new", PreserveContracts: true}
	p := RenameFilePrecondition{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: h}
	ref := SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: h, Range: ByteRange{StartByte: 0, EndByte: 1}}
	edit := ProposedEdit{SourceRef: ref, NewText: "new"}
	g1 := Gap{Code: "B", Reason: "second", SourceRefs: []SourceRef{}}
	g2 := Gap{Code: "A", Reason: "first", SourceRefs: []SourceRef{}}
	a, err := RenameResultDigest(scope, initial, h, []RenameFilePrecondition{p}, []ProposedEdit{edit, edit}, []Gap{g1, g2}, 32768)
	if err != nil || a != "19a1bbddbb13803f8c483c725ac457509c2598b208acbccacb5668b36219de82" {
		t.Fatalf("digest %s %v", a, err)
	}
	b, err := RenameResultDigest(scope, initial, h, []RenameFilePrecondition{p}, []ProposedEdit{edit}, []Gap{g2, g1}, 32768)
	if err != nil || a != b {
		t.Fatal("conflict order or dedup changed digest")
	}
	bad := edit
	bad.SourceRef.Range = ByteRange{StartByte: 0, EndByte: 2}
	if _, err := RenameResultDigest(scope, initial, h, []RenameFilePrecondition{p}, []ProposedEdit{edit, bad}, []Gap{}, 32768); err == nil {
		t.Fatal("accepted overlapping edits")
	}
	if _, err := RenameResultDigest(scope, initial, h, []RenameFilePrecondition{}, []ProposedEdit{edit}, []Gap{}, 32768); err == nil {
		t.Fatal("accepted edit without file precondition")
	}
	p.ContentHash = strings.Repeat("b", 64)
	if _, err := RenameResultDigest(scope, initial, h, []RenameFilePrecondition{p}, []ProposedEdit{edit}, []Gap{}, 32768); err == nil {
		t.Fatal("accepted changed file hash")
	}
}
