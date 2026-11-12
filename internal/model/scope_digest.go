package model

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"sort"
)

func compareScopeMemberKey(a, b ScopeMember) int {
	if c := compareOptional(a.ServiceID, b.ServiceID); c != 0 {
		return c
	}
	for _, pair := range [][2]string{{a.WorktreeID, b.WorktreeID}, {a.ProfileID, b.ProfileID}, {a.GenerationID, b.GenerationID}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

// ComputeScopeVectorDigest applies CT-04 to a structurally valid vector. It
// rejects contradictory copies of the same resolved member rather than hiding
// an inconsistent source/revision in a deterministic-looking identity.
// A digest is never authorization or proof that the vector is current.
func ComputeScopeVectorDigest(scope ScopeVector, maxBytes int) (string, error) {
	if err := scope.validateShapeForDigest(false); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		return "", fmt.Errorf("invalid scope digest budget")
	}
	members := append([]ScopeMember{}, scope.Members...)
	sort.SliceStable(members, func(i, j int) bool { return compareScopeMemberKey(members[i], members[j]) < 0 })
	for i := 1; i < len(members); i++ {
		if compareScopeMemberKey(members[i-1], members[i]) == 0 {
			a, err := json.Marshal(members[i-1])
			if err != nil {
				return "", err
			}
			b, err := json.Marshal(members[i])
			if err != nil {
				return "", err
			}
			if !bytes.Equal(a, b) {
				return "", fmt.Errorf("conflicting resolved member identity")
			}
		}
	}
	unavailable, err := NormalizeUnavailableMembers(scope.UnavailableMembers, maxBytes)
	if err != nil {
		return "", err
	}
	value := struct {
		Kind               string              `json:"kind"`
		Members            []ScopeMember       `json:"members"`
		GroupID            *string             `json:"group_id"`
		GroupRevision      *int64              `json:"group_revision"`
		CatalogIncarnation string              `json:"catalog_incarnation"`
		UnavailableMembers []MemberUnavailable `json:"unavailable_members"`
	}{scope.Kind, members, scope.GroupID, scope.GroupRevision, scope.CatalogIncarnation, unavailable}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("scope-v1", canonical, maxBytes)
	return digest, err
}
func VerifyScopeVectorDigest(scope ScopeVector, maxBytes int) error {
	if err := scope.validateShape(); err != nil {
		return err
	}
	digest, err := ComputeScopeVectorDigest(scope, maxBytes)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(scope.VectorDigest), []byte(digest)) != 1 {
		return fmt.Errorf("scope digest mismatch")
	}
	return nil
}
