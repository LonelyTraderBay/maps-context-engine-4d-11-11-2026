package model

import (
	"errors"
	"strings"
	"testing"
)

func TestPinnedScopeRejectsReusedEpochsAfterRestore(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	newID := "11234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	m := ScopeMember{WorktreeID: id, ProfileID: h, GenerationID: id, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: id, RecordsIncarnation: id}
	old := ScopeVector{Kind: "single", Members: []ScopeMember{m}, CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{}}
	var err error
	old.VectorDigest, err = ComputeScopeVectorDigest(old, 16384)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ScopeVector){"catalog": func(s *ScopeVector) { s.CatalogIncarnation = newID }, "index": func(s *ScopeVector) { s.Members[0].IndexIncarnation = newID }, "records": func(s *ScopeVector) { s.Members[0].RecordsIncarnation = newID }} {
		t.Run(name, func(t *testing.T) {
			v := old
			v.Members = append([]ScopeMember{}, old.Members...)
			mutate(&v)
			v.VectorDigest, err = ComputeScopeVectorDigest(v, 16384)
			if err != nil {
				t.Fatal(err)
			}
			if err = ComparePinnedScope(old, v, 16384); !errors.Is(err, ErrScopeChanged) {
				t.Fatalf("accepted stale incarnation: %v", err)
			}
		})
	}
}
