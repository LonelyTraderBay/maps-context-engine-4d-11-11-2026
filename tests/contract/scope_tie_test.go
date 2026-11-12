package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// Both lists are sorted by the currently specified keys: their keys tie, but
// their payload differs. This records D009; it does not choose a tie rule.
func TestScopeSortTieCounterexample(t *testing.T) {
	member := `{"service_id":null,"worktree_id":null,"profile_id":null,"status":"access_denied","reason_code":"%s"}`
	a, b := fmt.Sprintf(member, "REASON_A"), fmt.Sprintf(member, "REASON_B")
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	format := `{"kind":"federated","members":[],"vector_digest":"%s","group_id":"%s","group_revision":1,"catalog_incarnation":"%s","unavailable_members":[%s,%s]}`
	one, two := fmt.Sprintf(format, hash, id, id, a, b), fmt.Sprintf(format, hash, id, id, b, a)
	expected := []string{"1c95fdb38de90e25c3ba5043441cb289e4cf8517bdbfe4a5589afaf22c583dd0", "bce548229ad9290680ce4bc8e356a2198fb81aceafac516658d99fa8e5a9f8fd"}
	for i, raw := range []string{one, two} {
		if _, err := model.DecodeScopeVectorJSON([]byte(raw), 16384); err != nil {
			t.Fatal(err)
		}
		preimage := strings.Replace(raw, `"vector_digest":"`+hash+`",`, "", 1)
		value, err := model.DecodeIdentityJSON([]byte(preimage), 16384)
		if err != nil {
			t.Fatal(err)
		}
		_, digest, err := model.EncodeCanonical("scope-v1", value, 16384)
		if err != nil {
			t.Fatal(err)
		}
		if digest != expected[i] {
			t.Fatalf("fixture %d: got %s", i, digest)
		}
	}
	if expected[0] == expected[1] {
		t.Fatal("counterexample must distinguish the two orders")
	}
}

// The approved rule normalizes both input orders to the same canonical preimage.
func TestApprovedUnavailableTieBreak(t *testing.T) {
	member := `{"service_id":null,"worktree_id":null,"profile_id":null,"status":"access_denied","reason_code":"%s"}`
	a, b := fmt.Sprintf(member, "REASON_A"), fmt.Sprintf(member, "REASON_B")
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	format := `{"kind":"federated","members":[],"vector_digest":"%s","group_id":"%s","group_revision":1,"catalog_incarnation":"%s","unavailable_members":[%s,%s]}`
	for _, raw := range []string{fmt.Sprintf(format, hash, id, id, a, b), fmt.Sprintf(format, hash, id, id, b, a)} {
		scope, err := model.DecodeScopeVectorJSON([]byte(raw), 16384)
		if err != nil {
			t.Fatal(err)
		}
		scope.UnavailableMembers, err = model.NormalizeUnavailableMembers(scope.UnavailableMembers, 16384)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(scope)
		if err != nil {
			t.Fatal(err)
		}
		preimage := strings.Replace(string(encoded), `"vector_digest":"`+hash+`",`, "", 1)
		value, err := model.DecodeIdentityJSON([]byte(preimage), 16384)
		if err != nil {
			t.Fatal(err)
		}
		_, digest, err := model.EncodeCanonical("scope-v1", value, 16384)
		if err != nil {
			t.Fatal(err)
		}
		if digest != "1c95fdb38de90e25c3ba5043441cb289e4cf8517bdbfe4a5589afaf22c583dd0" {
			t.Fatalf("wrong normalized digest: %s", digest)
		}
	}
}
