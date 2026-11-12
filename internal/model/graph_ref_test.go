package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGraphRefsAndPinnedMemberSelection(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	serviceA, serviceB := "service-a", "service-b"
	symbol := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"symbol"}`, id, hash, id)
	file := fmt.Sprintf(`{"kind":"file","worktree_id":"%s","profile_id":"%s","generation_id":"%s","path":"docs/a.md","content_hash":null}`, id, hash, id)
	for name, raw := range map[string]string{"symbol": symbol, "file metadata": file} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeGraphRefJSON([]byte(raw), 4096); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, raw := range map[string]string{"unknown": strings.Replace(symbol, `"symbol_id":`, `"extra":1,"symbol_id":`, 1), "wrong kind": strings.Replace(symbol, `"symbol"`, `"unknown"`, 1), "missing": strings.Replace(file, `,"content_hash":null`, "", 1), "invalid path": strings.Replace(file, "docs/a.md", "../a", 1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeGraphRefJSON([]byte(raw), 4096); err == nil {
				t.Fatal("accepted invalid ref")
			}
		})
	}
	ref, err := DecodeGraphRefJSON([]byte(symbol), 4096)
	if err != nil {
		t.Fatal(err)
	}
	base := ScopeMember{WorktreeID: id, ProfileID: hash, GenerationID: id, SnapshotDigest: hash, PolicyDigest: hash, PolicyEpoch: 1, BindingRevision: 1, IndexIncarnation: id, RecordsIncarnation: id}
	first, second := base, base
	first.ServiceID = &serviceA
	second.ServiceID = &serviceB
	rev := int64(1)
	scope := ScopeVector{Kind: "federated", Members: []ScopeMember{first, second}, VectorDigest: hash, GroupID: &id, GroupRevision: &rev, CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{}}
	if _, err := MatchPinnedMember(scope, ref, nil); !errors.Is(err, ErrAmbiguousMember) {
		t.Fatalf("want ambiguous, got %v", err)
	}
	match, err := MatchPinnedMember(scope, ref, &serviceB)
	if err != nil || match.ServiceID == nil || *match.ServiceID != serviceB {
		t.Fatalf("wrong service: %+v, %v", match, err)
	}
	missing := "service-c"
	if _, err := MatchPinnedMember(scope, ref, &missing); !errors.Is(err, ErrMemberNotInScope) {
		t.Fatalf("want missing, got %v", err)
	}
}
