package model

import "testing"

func TestNormalizeUnavailableMembersUsesCanonicalTiebreaker(t *testing.T) {
	service := "service"
	input := []MemberUnavailable{
		{Status: "access_denied", ReasonCode: "aa"},
		{ServiceID: &service, Status: "access_denied", ReasonCode: "first"},
		{Status: "access_denied", ReasonCode: "z"},
	}
	output, err := NormalizeUnavailableMembers(input, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if output[0].ReasonCode != "z" || output[1].ReasonCode != "aa" || output[2].ServiceID == nil {
		t.Fatalf("wrong order: %+v", output)
	}
	if input[0].ReasonCode != "aa" {
		t.Fatal("mutated input order")
	}
	reversed := []MemberUnavailable{input[2], input[1], input[0]}
	again, err := NormalizeUnavailableMembers(reversed, 16384)
	if err != nil {
		t.Fatal(err)
	}
	for i := range output {
		if output[i].ReasonCode != again[i].ReasonCode {
			t.Fatal("order depends on input")
		}
	}
	if _, err := NormalizeUnavailableMembers(input, 1); err == nil {
		t.Fatal("ignored byte budget")
	}
	if _, err := NormalizeUnavailableMembers([]MemberUnavailable{{Status: "invented", ReasonCode: "x"}}, 16384); err == nil {
		t.Fatal("accepted invalid status")
	}
}
