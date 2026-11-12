package model

import (
	"errors"
	"strings"
	"testing"
)

func TestMutationReplayBindsPrincipalAndRequest(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	one := MutationIdentity{MutationID: id, PrincipalID: "actor", RequestHash: strings.Repeat("a", 64)}
	if replay, err := CheckMutationReplay(nil, one); err != nil || replay {
		t.Fatalf("new mutation: %v %v", replay, err)
	}
	if replay, err := CheckMutationReplay(&one, one); err != nil || !replay {
		t.Fatalf("replay: %v %v", replay, err)
	}
	two := one
	two.RequestHash = strings.Repeat("b", 64)
	if _, err := CheckMutationReplay(&one, two); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("changed request: %v", err)
	}
	two = one
	two.PrincipalID = "other"
	if _, err := CheckMutationReplay(&one, two); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("changed principal: %v", err)
	}
}
