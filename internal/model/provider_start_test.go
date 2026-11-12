package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestProviderStartBindsProfileAndProvider(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	provider := strings.Repeat("b", 64)
	profile := &GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: h, ProviderDigest: provider, ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	digest, err := ProfileDigest(profile, 8192)
	if err != nil {
		t.Fatal(err)
	}
	rawProfile, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"provider_protocol":1,"type":"analyze_start","job_id":"%s","provider_id":"go","provider_digest":"%s","profile_id":"%s","generation_id":"%s","snapshot_root":"/tmp/snap","manifest_path":"/tmp/manifest","manifest_digest":"%s","profile":%s,"max_frame_bytes":16777216,"deadline_at":"2026-11-11T06:00:00Z"}`, id, provider, digest, id, h, rawProfile)
	if _, err := DecodeProviderStartJSON([]byte(raw), 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"profile_id":"`+digest+`"`, `"profile_id":"`+h+`"`, 1), strings.Replace(raw, `"provider_digest":"`+provider+`"`, `"provider_digest":"`+h+`"`, 1), strings.Replace(raw, `"max_frame_bytes":16777216`, `"max_frame_bytes":16777217`, 1), strings.Replace(raw, `"deadline_at":"2026-11-11T06:00:00Z"`, `"deadline_at":"bad"`, 1)} {
		if _, err := DecodeProviderStartJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
