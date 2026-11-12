package model

import (
	"strings"
	"testing"
)

func TestBudgetLabelsAndLimits(t *testing.T) {
	good := `{"max_output_bytes":24576,"serialized_bytes":100,"token_count":null,"token_estimate":null,"tokenizer_id":null,"estimate_only":false,"delivery_mode":"mcp_dual"}`
	if _, err := DecodeBudgetJSON([]byte(good), 8192); err != nil {
		t.Fatal(err)
	}
	estimate := strings.Replace(strings.Replace(good, `"token_estimate":null`, `"token_estimate":10`, 1), `"estimate_only":false`, `"estimate_only":true`, 1)
	if _, err := DecodeBudgetJSON([]byte(estimate), 8192); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(good, `"max_output_bytes":24576`, `"max_output_bytes":4095`, 1),
		strings.Replace(good, `"serialized_bytes":100`, `"serialized_bytes":24577`, 1),
		strings.Replace(good, `"token_count":null`, `"token_count":2`, 1),
		strings.Replace(good, `"token_estimate":null`, `"token_estimate":2`, 1),
		strings.Replace(good, `"delivery_mode":"mcp_dual"`, `"delivery_mode":"invented"`, 1),
		strings.Replace(good, `,"tokenizer_id":null`, "", 1),
	} {
		if _, err := DecodeBudgetJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
