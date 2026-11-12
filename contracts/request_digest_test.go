package contracts

import "testing"

func TestRegistryRequestDigestBindsTransport(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.RequestDigest(MCP, "mce4d_workspace_status", "actor", []byte(`{}`), 16384, 4)
	if err != nil || got != "cecb25734186512aed55b5f4c88a4b859f3716126488e4742f32f32b663a4374" {
		t.Fatalf("digest %s %v", got, err)
	}
	if _, err := r.RequestDigest(HTTP, "mce4d_workspace_status", "actor", []byte(`{}`), 16384, 4); err == nil {
		t.Fatal("wrong transport accepted")
	}
}
