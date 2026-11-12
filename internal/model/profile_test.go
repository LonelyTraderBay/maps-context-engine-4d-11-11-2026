package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestGoProfileDigestNormalization(t *testing.T) {
	hash := strings.Repeat("a", 64)
	format := `{"schema_version":1,"language":"go","goos":"linux","goarch":"amd64","build_tags":[%s],"cgo_enabled":false,"tests_enabled":true,"module_roots":["."],"toolchain_digest":"%s","provider_digest":"%s","module_mode":"readonly","analysis_mode":"typed_when_available","dependency_roots":[]}`
	first := fmt.Sprintf(format, `"z","a"`, hash, hash)
	second := fmt.Sprintf(format, `"a","z"`, hash, hash)
	a, err := DecodeEffectiveProfileJSON([]byte(first), 16384)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeEffectiveProfileJSON([]byte(second), 16384)
	if err != nil {
		t.Fatal(err)
	}
	da, err := ProfileDigest(a, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if da != "4a6cbbd3dcc8798c737b9bd44bcde3a691cde1109889ea2f6a756f9b3e772a71" {
		t.Fatalf("does not match Python reference: %s", da)
	}
	db, err := ProfileDigest(b, 16384)
	if err != nil || da != db {
		t.Fatalf("digest mismatch %s %s %v", da, db, err)
	}
	if _, err := DecodeEffectiveProfileJSON([]byte(fmt.Sprintf(format, `"a","a"`, hash, hash)), 16384); err == nil {
		t.Fatal("accepted duplicate build tags")
	}
	if _, err := DecodeEffectiveProfileJSON([]byte(strings.Replace(first, `"module_roots":["."]`, `"module_roots":["../x"]`, 1)), 16384); err == nil {
		t.Fatal("accepted traversal")
	}
}
func TestTSProfileClosedAndNoPlugins(t *testing.T) {
	hash := strings.Repeat("b", 64)
	raw := fmt.Sprintf(`{"schema_version":1,"language":"typescript_javascript","tsconfig_paths":["tsconfig.json"],"compiler_digest":"%s","node_digest":"%s","provider_digest":"%s","allow_project_plugins":false,"analysis_mode":"syntax"}`, hash, hash, hash)
	p, err := DecodeEffectiveProfileJSON([]byte(raw), 16384)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProfileDigest(p, 16384); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, `"allow_project_plugins":false`, `"allow_project_plugins":true`, 1), strings.Replace(raw, `"analysis_mode":`, `"extra":1,"analysis_mode":`, 1), strings.Replace(raw, `"tsconfig.json"`, `"../tsconfig.json"`, 1)} {
		if _, err := DecodeEffectiveProfileJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
