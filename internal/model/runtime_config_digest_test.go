package model

import "testing"

func TestRuntimeConfigDigestRequiresResolvedCPU(t *testing.T) {
	if _, err := RuntimeConfigDigest(DefaultRuntimeConfig(), 16384); err == nil {
		t.Fatal("accepted unresolved CPU sentinel")
	}
	config, err := DecodeRuntimeConfigJSON([]byte(`{"schema_version":1}`), 8192, 8)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := RuntimeConfigDigest(config, 16384)
	if err != nil || digest != "859ef2a770850d75d230ef72e405e24ff748d8ba9286508c102b771172d1074c" {
		t.Fatalf("digest=%s err=%v", digest, err)
	}
}
