package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestScopeRequestModes(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	single := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s"}`, id, hash, id)
	group := fmt.Sprintf(`{"group_id":"%s"}`, id)
	for _, tc := range []struct {
		input    string
		bindings int
		want     string
	}{{single, 0, "single"}, {group, 0, "group"}, {`{}`, 1, "bound"}} {
		req, err := DecodeScopeRequestJSON([]byte(tc.input), 16384)
		if err != nil {
			t.Fatal(err)
		}
		mode, err := req.SelectionMode(tc.bindings)
		if err != nil || mode != tc.want {
			t.Fatalf("%s: %s %v", tc.input, mode, err)
		}
	}
	for name, input := range map[string]string{
		"unknown":          `{"unknown":1}`,
		"null":             `{"group_id":null}`,
		"missing profile":  fmt.Sprintf(`{"worktree_id":"%s"}`, id),
		"missing worktree": fmt.Sprintf(`{"profile_id":"%s"}`, hash),
		"group and single": fmt.Sprintf(`{"group_id":"%s","worktree_id":"%s","profile_id":"%s"}`, id, id, hash),
		"generation only":  fmt.Sprintf(`{"generation_id":"%s"}`, id),
		"invalid group":    `{"group_id":"group"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req, err := DecodeScopeRequestJSON([]byte(input), 16384)
			if err == nil {
				_, err = req.SelectionMode(1)
			}
			if err == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
	empty, err := DecodeScopeRequestJSON([]byte(`{}`), 16384)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, 2} {
		if _, err := empty.SelectionMode(count); err == nil {
			t.Fatal("accepted ambiguous binding")
		}
	}
}

func TestPinnedScopeRequestCannotMixLiveSelection(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	vector := fmt.Sprintf(`{"kind":"federated","members":[],"vector_digest":"%s","group_id":"%s","group_revision":1,"catalog_incarnation":"%s","unavailable_members":[]}`, hash, id, id)
	req, err := DecodeScopeRequestJSON([]byte(`{"pinned_vector":`+vector+`}`), 16384)
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := req.SelectionMode(0); err != nil || mode != "pinned" {
		t.Fatalf("%s %v", mode, err)
	}
	req.GroupID = &id
	if _, err := req.SelectionMode(0); err == nil {
		t.Fatal("mixed pinned and group")
	}
	req.GroupID = nil
	req.PinnedVector.Members = nil
	if _, err := req.SelectionMode(0); err == nil {
		t.Fatal("accepted null member array")
	}
}
