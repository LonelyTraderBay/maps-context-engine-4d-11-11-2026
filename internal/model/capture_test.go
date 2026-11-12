package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestCaptureManifestDigestAndDuplicatePaths(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	dh := strings.Repeat("b", 64)
	file := `{"path":"%s","status":"captured","content_hash":"` + h + `","byte_length":3,"reason_code":null}`
	format := `{"schema_version":1,"worktree_id":"%s","profile_id":"%s","policy_digest":"%s","provider_digest":"%s","files":[%s,%s],"dependency_input_digests":["%s","%s"],"snapshot_digest":"%s"}`
	input := fmt.Sprintf(format, id, h, h, h, fmt.Sprintf(file, "z.go"), fmt.Sprintf(file, "a.go"), dh, h, h)
	m, err := DecodeCaptureManifestJSON([]byte(input), 32768)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ComputeCaptureDigest(m, 32768)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "2f6910400150349c0a718b36ebfc5ce946556762cd6572c126b9b0bbfb03431c" {
		t.Fatalf("does not match Python reference: %s", digest)
	}
	if _, err := ComputeCaptureDigest(m, 1); err == nil {
		t.Fatal("ignored digest input budget")
	}
	if m.Files[0].Path != "z.go" {
		t.Fatal("mutated manifest")
	}
	if err := VerifyCaptureDigest(m, 32768); err == nil {
		t.Fatal("accepted mismatched digest")
	}
	m.SnapshotDigest = digest
	if err := VerifyCaptureDigest(m, 32768); err != nil {
		t.Fatal(err)
	}
	m.Files[0], m.Files[1] = m.Files[1], m.Files[0]
	m.DependencyInputDigests[0], m.DependencyInputDigests[1] = m.DependencyInputDigests[1], m.DependencyInputDigests[0]
	again, err := ComputeCaptureDigest(m, 32768)
	if err != nil || again != digest {
		t.Fatal("sorting changed digest")
	}
	m.DependencyInputDigests = append(m.DependencyInputDigests, m.DependencyInputDigests[0])
	changed, err := ComputeCaptureDigest(m, 32768)
	if err != nil || changed == digest {
		t.Fatal("dependency multiplicity lost")
	}
	for _, bad := range []string{strings.Replace(input, `"path":"z.go"`, `"path":"a.go"`, 1), strings.Replace(input, `"status":"captured"`, `"status":"parse_error"`, 1), strings.Replace(input, `"byte_length":3`, `"byte_length":null`, 1)} {
		if _, err := DecodeCaptureManifestJSON([]byte(bad), 32768); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
