package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestNestedScopeRejectsContradictorySelectors(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	raw := fmt.Sprintf(`{"scope":{"group_id":"%s","worktree_id":"%s","profile_id":"%s"}}`, id, id, h)
	if _, err := DecodeStatusRequestJSON([]byte(raw), 16384); err == nil {
		t.Fatal("accepted mixed group and single scope")
	}
	if _, err := DecodeStatusRequestJSON([]byte(`{"scope":{"worktree_id":"invalid"}}`), 16384); err == nil {
		t.Fatal("accepted incomplete scope")
	}
	req, err := DecodeStatusRequestJSON([]byte(`{"scope":{}}`), 16384)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = req.Scope.SelectionMode(0); err == nil {
		t.Fatal("empty scope bypassed binding check")
	}
}
