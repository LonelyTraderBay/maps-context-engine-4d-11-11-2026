package model

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"unicode/utf8"
)

var ErrRecordConflict = errors.New("record mutation conflict")

type MutationIdentity struct {
	MutationID  string
	PrincipalID string
	RequestHash string
}

func validMutationIdentity(v MutationIdentity) bool {
	return uuid(v.MutationID) && v.PrincipalID != "" && utf8.ValidString(v.PrincipalID) && lowerHex(v.RequestHash, 64)
}

// CheckMutationReplay returns true only for the same principal and request
// bound to the same mutation ID. The caller must authenticate the principal and
// perform the lookup and mutation atomically in the durable records store.
func CheckMutationReplay(existing *MutationIdentity, incoming MutationIdentity) (bool, error) {
	if !validMutationIdentity(incoming) {
		return false, fmt.Errorf("invalid mutation identity")
	}
	if existing == nil {
		return false, nil
	}
	if !validMutationIdentity(*existing) || existing.MutationID != incoming.MutationID {
		return false, fmt.Errorf("invalid stored mutation identity")
	}
	samePrincipal := subtle.ConstantTimeCompare([]byte(existing.PrincipalID), []byte(incoming.PrincipalID)) == 1
	sameRequest := subtle.ConstantTimeCompare([]byte(existing.RequestHash), []byte(incoming.RequestHash)) == 1
	if !samePrincipal || !sameRequest {
		return false, ErrRecordConflict
	}
	return true, nil
}
