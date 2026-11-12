package model

import "sort"

// NormalizeScopeVector validates the digest and returns copies ordered using the
// scope-v1 recipe; it does not resolve current membership or grant access.
func NormalizeScopeVector(scope ScopeVector, maxBytes int) (ScopeVector, error) {
	if err := VerifyScopeVectorDigest(scope, maxBytes); err != nil {
		return ScopeVector{}, err
	}
	members := append([]ScopeMember{}, scope.Members...)
	sort.SliceStable(members, func(i, j int) bool { return compareScopeMemberKey(members[i], members[j]) < 0 })
	unavailable, err := NormalizeUnavailableMembers(scope.UnavailableMembers, maxBytes)
	if err != nil {
		return ScopeVector{}, err
	}
	result := scope
	result.Members = members
	result.UnavailableMembers = unavailable
	return result, nil
}
