package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestScopeVectorDigestNormalizesUnavailableTies(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	member := `{"service_id":null,"worktree_id":null,"profile_id":null,"status":"access_denied","reason_code":"%s"}`
	a, b := fmt.Sprintf(member, "REASON_A"), fmt.Sprintf(member, "REASON_B")
	input := fmt.Sprintf(`{"kind":"federated","members":[],"vector_digest":"%s","group_id":"%s","group_revision":1,"catalog_incarnation":"%s","unavailable_members":[%s,%s]}`, hash, id, id, b, a)
	scope, err := DecodeScopeVectorJSON([]byte(input), 16384)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ComputeScopeVectorDigest(scope, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "1c95fdb38de90e25c3ba5043441cb289e4cf8517bdbfe4a5589afaf22c583dd0" {
		t.Fatalf("wrong digest %s", digest)
	}
	if scope.UnavailableMembers[0].ReasonCode != "REASON_B" {
		t.Fatal("mutated caller order")
	}
	if err := VerifyScopeVectorDigest(scope, 16384); err == nil {
		t.Fatal("accepted wrong digest")
	}
	scope.VectorDigest = digest
	if err := VerifyScopeVectorDigest(scope, 16384); err != nil {
		t.Fatal(err)
	}
	scope.UnavailableMembers[0].ReasonCode = "changed"
	if err := VerifyScopeVectorDigest(scope, 16384); err == nil {
		t.Fatal("accepted tampered vector")
	}
}
func TestScopeDigestRejectsConflictingResolvedIdentity(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	service := "s"
	revision := int64(1)
	a := ScopeMember{WorktreeID: id, ProfileID: hash, GenerationID: id, SnapshotDigest: hash, PolicyDigest: hash, PolicyEpoch: 1, BindingRevision: 1, ServiceID: &service, IndexIncarnation: id, RecordsIncarnation: id}
	b := a
	b.PolicyEpoch = 2
	v := ScopeVector{Kind: "federated", Members: []ScopeMember{a, b}, GroupID: &id, GroupRevision: &revision, CatalogIncarnation: id, UnavailableMembers: []MemberUnavailable{}}
	if _, err := ComputeScopeVectorDigest(v, 16384); err == nil {
		t.Fatal("accepted contradictory epochs for the same resolved member")
	}
}
