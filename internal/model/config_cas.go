package model

import (
	"crypto/subtle"
	"errors"
	"fmt"
)

var ErrConfigConflict = errors.New("runtime config conflict")

// CheckExpectedConfigDigest only evaluates a CAS precondition. The caller must
// hold the config lock across the check and atomic write, and authenticate actor.
func CheckExpectedConfigDigest(request RuntimeConfigRequest, currentDigest *string) error {
	if request.Action != "set" || !request.ExpectedDigestSet || request.Config == nil {
		return fmt.Errorf("invalid config set request")
	}
	if currentDigest != nil && !lowerHex(*currentDigest, 64) || request.ExpectedDigest != nil && !lowerHex(*request.ExpectedDigest, 64) {
		return fmt.Errorf("invalid config digest")
	}
	if (currentDigest == nil) != (request.ExpectedDigest == nil) {
		return ErrConfigConflict
	}
	if currentDigest != nil && subtle.ConstantTimeCompare([]byte(*currentDigest), []byte(*request.ExpectedDigest)) != 1 {
		return ErrConfigConflict
	}
	return nil
}
