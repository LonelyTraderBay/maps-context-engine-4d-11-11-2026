package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestMapping101MatchesCannotClaimUniqueFirstPage(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	binding := strings.Repeat("b", 64)
	anchorID := strings.Repeat("c", 64)
	anchor := fmt.Sprintf(`{"kind":"source_anchor","worktree_id":"%s","profile_id":"%s","generation_id":"%s","build_map_id":"%s","anchor_id":"%s","binding_id":"%s"}`, id, h, id, h, anchorID, binding)
	target := func(i int) string {
		return fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s%d"}`, id, h, id, i)
	}
	matches := make([]string, 101)
	for i := range matches {
		matches[i] = fmt.Sprintf(`{"match_id":"%064x","anchor_ref":%s,"target_ref":%s,"source_refs":[],"mapping_status":"candidate","evidence_refs":[]}`, i+1, anchor, target(i))
	}
	seen := map[string]bool{}
	for _, page := range [][]string{matches[:100], matches[100:]} {
		raw := fmt.Sprintf(`{"operation":"mapping_resolve","requested_ref":%s,"matches":[%s],"build_map_id":"%s","binding_id":"%s","resolver_digest":"%s","mapping_status":"ambiguous","why_matched":"fixture"}`, target(0), strings.Join(page, ","), h, binding, h)
		data, err := DecodeMappingDataJSON([]byte(raw), 131072)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range data.Matches {
			symbol := m.TargetRef.(*SymbolRef).SymbolID
			if seen[symbol] || m.AnchorRef.AnchorID != anchorID {
				t.Fatal("duplicate or unpaired mapping")
			}
			seen[symbol] = true
		}
	}
	if len(seen) != 101 {
		t.Fatal("missing mapping")
	}
	one := int64(1)
	if err := ValidateMappingCompleteness("exact_in_supported_scope", false, &one); err == nil {
		t.Fatal("first page falsely marked unique")
	}
	total := int64(101)
	if err := ValidateMappingCompleteness("exact_in_supported_scope", true, &total); err == nil {
		t.Fatal("multiple mappings falsely marked unique")
	}
	if err := ValidateMappingCompleteness("exact_in_supported_scope", true, &one); err != nil {
		t.Fatal(err)
	}
}
