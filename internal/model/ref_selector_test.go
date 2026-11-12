package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestRefSelectorWireAndPinnedSelection(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	ref := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"id"}`, id, hash, id)
	good := `{"kind":"ref","ref":` + ref + `,"service_id":null}`
	selector, err := DecodeRefSelectorJSON([]byte(good), 8192)
	if err != nil || selector.Ref == nil || selector.ServiceID != nil {
		t.Fatalf("%+v %v", selector, err)
	}
	cases := []string{strings.Replace(good, `"kind":"ref"`, `"kind":"symbol"`, 1), strings.Replace(good, `,"service_id":null`, "", 1), strings.Replace(good, `"service_id":null`, `"service_id":""`, 1), strings.Replace(good, `"ref":`, `"unexpected":1,"ref":`, 1)}
	for _, bad := range cases {
		if _, err := DecodeRefSelectorJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
