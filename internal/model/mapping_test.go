package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestMappingMatchPairsAndBinding(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	binding := strings.Repeat("b", 64)
	resolver := strings.Repeat("c", 64)
	anchorID := strings.Repeat("d", 64)
	matchID := strings.Repeat("e", 64)
	anchor := fmt.Sprintf(`{"kind":"source_anchor","worktree_id":"%s","profile_id":"%s","generation_id":"%s","build_map_id":"%s","anchor_id":"%s","binding_id":"%s"}`, id, h, id, h, anchorID, binding)
	target := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	match := fmt.Sprintf(`{"match_id":"%s","anchor_ref":%s,"target_ref":%s,"source_refs":[],"mapping_status":"candidate","evidence_refs":[]}`, matchID, anchor, target)
	second := strings.Replace(strings.Replace(match, `"symbol_id":"s"`, `"symbol_id":"t"`, 1), matchID, strings.Repeat("f", 64), 1)
	raw := fmt.Sprintf(`{"operation":"mapping_resolve","requested_ref":%s,"matches":[%s,%s],"build_map_id":"%s","binding_id":"%s","resolver_digest":"%s","mapping_status":"candidate","why_matched":"fixture"}`, target, match, second, h, binding, resolver)
	d, err := DecodeMappingDataJSON([]byte(raw), 16384)
	if err != nil || len(d.Matches) != 2 || d.Matches[0].TargetRef == nil {
		t.Fatalf("%+v %v", d, err)
	}
	if d.Matches[0].TargetRef.(*SymbolRef).SymbolID != "s" || d.Matches[1].TargetRef.(*SymbolRef).SymbolID != "t" {
		t.Fatal("lost anchor-target pairing")
	}
	bad := strings.Replace(raw, `"binding_id":"`+binding+`"`, `"binding_id":"`+h+`"`, 1)
	if _, err := DecodeMappingDataJSON([]byte(bad), 16384); err == nil {
		t.Fatal("accepted conflicting binding")
	}
	request := fmt.Sprintf(`{"session_id":"%s","build_map_id":"%s","document_epoch":1,"hmr_epoch":0,"ref":%s}`, id, h, target)
	r, err := DecodeMappingResolveRequestJSON([]byte(request), 16384)
	if err != nil || r.Limit != 20 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestExactMappingAllowsSeveralAnchorsForOneTarget(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	binding := strings.Repeat("b", 64)
	target := fmt.Sprintf(`{"kind":"symbol","worktree_id":"%s","profile_id":"%s","generation_id":"%s","symbol_id":"s"}`, id, h, id)
	match := func(anchorID, matchID string) string {
		return fmt.Sprintf(`{"match_id":"%s","anchor_ref":{"kind":"source_anchor","worktree_id":"%s","profile_id":"%s","generation_id":"%s","build_map_id":"%s","anchor_id":"%s","binding_id":"%s"},"target_ref":%s,"source_refs":[],"mapping_status":"exact_in_supported_scope","evidence_refs":[]}`, matchID, id, h, id, h, anchorID, binding, target)
	}
	first := match(strings.Repeat("c", 64), strings.Repeat("d", 64))
	second := match(strings.Repeat("e", 64), strings.Repeat("f", 64))
	raw := fmt.Sprintf(`{"operation":"mapping_resolve","requested_ref":%s,"matches":[%s,%s],"build_map_id":"%s","binding_id":"%s","resolver_digest":"%s","mapping_status":"exact_in_supported_scope","why_matched":"fixture"}`, target, first, second, h, binding, h)
	if _, err := DecodeMappingDataJSON([]byte(raw), 16384); err != nil {
		t.Fatalf("one target with several anchors rejected: %v", err)
	}
	other := strings.Replace(second, `"symbol_id":"s"`, `"symbol_id":"other"`, 1)
	if _, err := DecodeMappingDataJSON([]byte(strings.Replace(raw, second, other, 1)), 16384); err == nil {
		t.Fatal("multiple targets falsely marked exact")
	}
	uncertain := strings.Replace(second, `"mapping_status":"exact_in_supported_scope"`, `"mapping_status":"candidate"`, 1)
	if _, err := DecodeMappingDataJSON([]byte(strings.Replace(raw, second, uncertain, 1)), 16384); err == nil {
		t.Fatal("candidate evidence falsely marked exact")
	}
}
