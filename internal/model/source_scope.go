package model

// MatchPinnedSource checks the SourceRef identity against a verified vector.
// Ambiguous identities across services require an explicit service ID. The
// caller must still check captured bytes, policy, store epochs and authorization.
func MatchPinnedSource(scope ScopeVector, ref SourceRef, serviceID *string, maxBytes int) (ScopeMember, error) {
	if err := ref.validateIdentity(); err != nil {
		return ScopeMember{}, err
	}
	if err := VerifyScopeVectorDigest(scope, maxBytes); err != nil {
		return ScopeMember{}, err
	}
	file := &FileRef{Kind: "file", GraphIdentity: GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}, Path: ref.Path, ContentHash: &ref.ContentHash}
	return MatchPinnedMember(scope, file, serviceID)
}
