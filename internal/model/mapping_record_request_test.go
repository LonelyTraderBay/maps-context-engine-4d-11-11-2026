package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestMappingRecordRequests(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	source := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s","path":"a.go","content_hash":"%s","range":{"start_byte":0,"end_byte":1}}`, id, h, id, h)
	target := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"item"}`, id, h, id)
	proposal := fmt.Sprintf(`{"source_ref":%s,"destination_ref":%s,"rationale":"why","client_mutation_id":"%s"}`, source, target, id)
	if _, err := DecodeMappingProposeRequestJSON([]byte(proposal), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMappingProposeRequestJSON([]byte(strings.Replace(proposal, `"rationale":"why"`, `"rationale":""`, 1)), 16384); err == nil {
		t.Fatal("accepted empty rationale")
	}
	review := fmt.Sprintf(`{"record_id":"%s","expected_version":1,"action":"approve","reason":"reviewed","client_mutation_id":"%s"}`, id, id)
	if _, err := DecodeMappingReviewRequestJSON([]byte(review), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(review, `"expected_version":1`, `"expected_version":0`, 1), strings.Replace(review, `"approve"`, `"execute"`, 1), strings.Replace(review, `"reason":"reviewed"`, `"reason":null`, 1)} {
		if _, err := DecodeMappingReviewRequestJSON([]byte(bad), 16384); err == nil {
			t.Fatal("accepted invalid review")
		}
	}
}
