package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenameDataNeverClaimsMutation(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	raw := fmt.Sprintf(`{"operation":"rename_preview","applied":false,"source_mutation_count":0,"validation_required":true,"edits":[],"conflicts":[],"preview_complete":false,"result_token":"token","result_id":"%s","result_digest":"%s","total_edits":0,"computation_complete":true}`, id, h)
	if _, err := DecodeRenameDataJSON([]byte(raw), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"applied":false`, `"applied":true`, 1), strings.Replace(raw, `"source_mutation_count":0`, `"source_mutation_count":1`, 1), strings.Replace(raw, `"validation_required":true`, `"validation_required":false`, 1), strings.Replace(raw, `"computation_complete":true`, `"computation_complete":false`, 1), strings.Replace(raw, `"edits":[]`, `"edits":null`, 1)} {
		if _, err := DecodeRenameDataJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
