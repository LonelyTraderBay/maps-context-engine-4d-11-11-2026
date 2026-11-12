package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestScopeVectorWireShape(t *testing.T) {
	hash := strings.Repeat("a", 64)
	id := "01234567-89ab-cdef-0123-456789abcdef"
	member := fmt.Sprintf(`{"worktree_id":"%s","profile_id":"%s","generation_id":"%s","snapshot_digest":"%s","policy_digest":"%s","policy_epoch":1,"binding_revision":1,"record_epoch":0,"service_id":null,"index_incarnation":"%s","records_incarnation":"%s"}`, id, hash, id, hash, hash, id, id)
	single := fmt.Sprintf(`{"kind":"single","members":[%s],"vector_digest":"%s","group_id":null,"group_revision":null,"catalog_incarnation":"%s","unavailable_members":[]}`, member, hash, id)
	federated := fmt.Sprintf(`{"kind":"federated","members":[],"vector_digest":"%s","group_id":"%s","group_revision":1,"catalog_incarnation":"%s","unavailable_members":[{"service_id":null,"worktree_id":null,"profile_id":null,"status":"access_denied","reason_code":"REDACTED"}]}`, hash, id, id)
	for name, input := range map[string]string{"single": single, "all unavailable": federated} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeScopeVectorJSON([]byte(input), 16384); err != nil {
				t.Fatal(err)
			}
		})
	}
	cases := map[string]string{
		"missing field":      strings.Replace(single, `"group_id":null,`, "", 1),
		"unknown field":      strings.Replace(single, `"kind":`, `"extra":true,"kind":`, 1),
		"unknown member":     strings.Replace(single, `"policy_epoch":1`, `"extra":0,"policy_epoch":1`, 1),
		"missing nullable":   strings.Replace(single, `"service_id":null,`, "", 1),
		"null members":       strings.Replace(single, `"members":[`+member+`]`, `"members":null`, 1),
		"negative epoch":     strings.Replace(single, `"record_epoch":0`, `"record_epoch":-1`, 1),
		"fraction epoch":     strings.Replace(single, `"policy_epoch":1`, `"policy_epoch":1.0`, 1),
		"single group":       strings.Replace(single, `"group_id":null`, `"group_id":"`+id+`"`, 1),
		"no single member":   strings.Replace(single, `"members":[`+member+`]`, `"members":[]`, 1),
		"federated no group": strings.Replace(federated, `"group_id":"`+id+`"`, `"group_id":null`, 1),
		"wrong status":       strings.Replace(federated, `"access_denied"`, `"invented"`, 1),
		"bad reason":         strings.Replace(federated, `"REDACTED"`, `""`, 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeScopeVectorJSON([]byte(input), 16384); err == nil {
				t.Fatal("accepted invalid scope vector")
			}
		})
	}
}
