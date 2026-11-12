package model

import (
	"crypto/subtle"
	"errors"
)

var ErrScopeChanged = errors.New("pinned scope changed")

// ComparePinnedScope verifies both digests and rejects any change in the full
// vector. The caller must obtain the second vector from trusted stores after
// work; comparing client-supplied assertions cannot prove freshness or access.
func ComparePinnedScope(pinned, current ScopeVector, maxBytes int) error {
	if err := VerifyScopeVectorDigest(pinned, maxBytes); err != nil {
		return err
	}
	if err := VerifyScopeVectorDigest(current, maxBytes); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(pinned.VectorDigest), []byte(current.VectorDigest)) != 1 {
		return ErrScopeChanged
	}
	return nil
}
