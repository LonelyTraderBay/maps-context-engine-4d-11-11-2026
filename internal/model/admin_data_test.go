package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestAdminData(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	group := fmt.Sprintf(`{"operation":"group_admin","groups":[{"config":{"group_id":"%s","name":"group","members":[{"service_id":"svc","worktree_id":"%s","profile_id":"%s","namespace":{"environment":"dev","protocol":"http","base_origin":null,"route_prefix":null,"contract_version":null}}]},"revision":1,"status":"active"}]}`, id, id, h)
	if _, err := DecodeGroupAdminDataJSON([]byte(group), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeGroupAdminDataJSON([]byte(strings.Replace(group, `"status":"active"`, `"status":"bad"`, 1)), 16384); err == nil {
		t.Fatal("accepted invalid group status")
	}
	profile := fmt.Sprintf(`{"operation":"profile_admin","worktree_id":"%s","profile_name":"default","profile_id":"%s","revision":1}`, id, h)
	if _, err := DecodeProfileAdminDataJSON([]byte(profile), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeProfileAdminDataJSON([]byte(strings.Replace(profile, `"revision":1`, `"revision":0`, 1)), 16384); err == nil {
		t.Fatal("accepted revision 0")
	}
	policy := fmt.Sprintf(`{"operation":"policy_admin","worktree_id":"%s","policy_digest":"%s","policy_epoch":1}`, id, h)
	if _, err := DecodePolicyAdminDataJSON([]byte(policy), 16384); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePolicyAdminDataJSON([]byte(strings.Replace(policy, `"policy_epoch":1`, `"policy_epoch":null`, 1)), 16384); err == nil {
		t.Fatal("accepted null epoch")
	}
}
