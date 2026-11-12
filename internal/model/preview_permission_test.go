package model

import (
	"strings"
	"testing"
)

func TestPreviewPermissionDigestAndShape(t *testing.T) {
	raw := `{"schema_version":1,"profile_id":"dev","approved_origin":"http://localhost:3000","approved_path_prefix":"/","target_environment":"local_mock","source_worktree_ids":["11234567-89ab-cdef-0123-456789abcdef","01234567-89ab-cdef-0123-456789abcdef"],"allow_attach":true,"allow_interact":false,"allow_scroll":false,"allowed_dev_destinations":["http://b","http://a"],"attestation_id":"a","attested_by":"owner","attested_at":"2026-11-11T00:00:00Z","expires_at":"2026-11-12T00:00:00Z","version":1}`
	p, err := DecodePreviewPermissionProfileJSON([]byte(raw), 16384)
	if err != nil {
		t.Fatal(err)
	}
	a, err := PreviewPermissionDigest(p, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if a != "b873da0467ce3939ccaf4cb176567a20eece3ade2568fb62d597b89409eafc31" {
		t.Fatalf("digest differs from independent Python oracle: %s", a)
	}
	p.SourceWorktreeIDs[0], p.SourceWorktreeIDs[1] = p.SourceWorktreeIDs[1], p.SourceWorktreeIDs[0]
	p.AllowedDevDestinations[0], p.AllowedDevDestinations[1] = p.AllowedDevDestinations[1], p.AllowedDevDestinations[0]
	b, err := PreviewPermissionDigest(p, 16384)
	if err != nil || a != b {
		t.Fatalf("unstable digest %s %s %v", a, b, err)
	}
	dup := strings.Replace(raw, `"http://b","http://a"`, `"http://a","http://a"`, 1)
	if _, err := DecodePreviewPermissionProfileJSON([]byte(dup), 16384); err == nil {
		t.Fatal("accepted duplicates")
	}
}
