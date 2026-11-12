package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildManifestDigestAndOwnerCycles(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	hash := strings.Repeat("a", 64)
	a := strings.Repeat("b", 64)
	b := strings.Repeat("c", 64)
	raw := fmt.Sprintf(`{"schema_version":1,"worktree_id":"%s","profile_id":"%s","build_map_id":"%s","provider_version":"v1","build_config_digest":"%s","input_manifest_digest":"%s","mode":"development","module_coverage":"partial","sources":[{"path":"z.tsx","content_hash":"%s"},{"path":"a.tsx","content_hash":"%s"}],"anchors":[{"anchor_id":"%s","kind":"element_site","path":"z.tsx","content_hash":"%s","range":{"start_byte":0,"end_byte":2},"owner_anchor_id":"%s"},{"anchor_id":"%s","kind":"component_definition","path":"a.tsx","content_hash":"%s","range":{"start_byte":0,"end_byte":2},"owner_anchor_id":null}],"provider_digest":"%s"}`, id, hash, hash, hash, hash, hash, hash, b, hash, a, a, hash, hash)
	m, err := DecodeBuildManifestJSON([]byte(raw), 32768)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ComputeBuildMapDigest(m, 32768)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "c08d5673dcca45d6b7d37776aca725924ac4e09f85c03a9afb6b670bd17aeb4e" {
		t.Fatalf("does not match Python reference: %s", digest)
	}
	if m.Sources[0].Path != "z.tsx" || m.Anchors[0].AnchorID != b {
		t.Fatal("input order mutated")
	}
	m.BuildMapID = digest
	if err := VerifyBuildMapDigest(m, 32768); err != nil {
		t.Fatal(err)
	}
	m.Sources[0], m.Sources[1] = m.Sources[1], m.Sources[0]
	m.Anchors[0], m.Anchors[1] = m.Anchors[1], m.Anchors[0]
	again, err := ComputeBuildMapDigest(m, 32768)
	if err != nil || again != digest {
		t.Fatal("order changed identity")
	}
	m.Anchors[0].OwnerAnchorID = &b
	if _, err := ComputeBuildMapDigest(m, 32768); err == nil {
		t.Fatal("accepted cycle")
	}
	for _, bad := range []string{strings.Replace(raw, `"path":"z.tsx","content_hash"`, `"path":"a.tsx","content_hash"`, 1), strings.Replace(raw, `"kind":"element_site"`, `"kind":"invented"`, 1), strings.Replace(raw, `"module_coverage":"partial"`, `"module_coverage":"everything"`, 1)} {
		if _, err := DecodeBuildManifestJSON([]byte(bad), 32768); err == nil {
			t.Fatalf("accepted invalid manifest %s", bad)
		}
	}
}
