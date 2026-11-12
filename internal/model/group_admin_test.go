package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestGroupAdminRequestAuthorityShape(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	config := fmt.Sprintf(`{"group_id":"%s","name":"Example","members":[{"service_id":"svc","worktree_id":"%s","profile_id":"%s","namespace":{"environment":"dev","protocol":"http","base_origin":null,"route_prefix":null,"contract_version":null}}]}`, id, id, h)
	put := fmt.Sprintf(`{"action":"put","expected_revision":0,"config":%s}`, config)
	if _, err := DecodeGroupAdminRequestJSON([]byte(put), 16384); err != nil {
		t.Fatal(err)
	}
	get := fmt.Sprintf(`{"action":"get","group_id":"%s"}`, id)
	if _, err := DecodeGroupAdminRequestJSON([]byte(get), 16384); err != nil {
		t.Fatal(err)
	}
	list, err := DecodeGroupAdminRequestJSON([]byte(`{"action":"list"}`), 16384)
	if err != nil || list.Limit == nil || *list.Limit != 20 {
		t.Fatalf("%+v %v", list, err)
	}
	for _, bad := range []string{`{"action":"get"}`, `{"action":"remove"}`, strings.Replace(put, `"expected_revision":0,`, "", 1), `{"action":"list","group_id":"` + id + `"}`, strings.Replace(put, `"protocol":"http"`, `"protocol":"shell"`, 1)} {
		if _, err := DecodeGroupAdminRequestJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
