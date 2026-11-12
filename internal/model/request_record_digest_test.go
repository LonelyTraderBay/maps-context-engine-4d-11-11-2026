package model

import (
	"strings"
	"testing"
)

func TestRequestDigestPrincipalAndTypedSchema(t *testing.T) {
	r, err := DecodeStatusRequestJSON([]byte(`{}`), 16384)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RequestDigest("status", "actor", "StatusRequest", r, 16384, 4)
	if err != nil || got != "cecb25734186512aed55b5f4c88a4b859f3716126488e4742f32f32b663a4374" {
		t.Fatalf("request digest %s %v", got, err)
	}
	if _, err := RequestDigest("status", "actor", "RecordListRequest", r, 16384, 4); err == nil {
		t.Fatal("accepted wrong request schema")
	}
	other, err := RequestDigest("status", "other", "StatusRequest", r, 16384, 4)
	if err != nil || other == got {
		t.Fatal("principal must affect hash")
	}
}
func TestRecordPayloadDigestPreservesRationale(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	p := UIMappingPayload{SourceRef: SourceRef{WorktreeID: id, ProfileID: h, GenerationID: id, Path: "a.go", ContentHash: h, Range: ByteRange{StartByte: 0, EndByte: 1}}, DestinationRef: &SymbolRef{Kind: "symbol", GraphIdentity: GraphIdentity{WorktreeID: id, ProfileID: h, GenerationID: id}, SymbolID: "item"}, Rationale: "reason"}
	a, err := UIMappingPayloadDigest(p, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if a != "cd5793a259df525abe426742456e16452ccc7c2c3e82f714744307f8c5828102" {
		t.Fatalf("digest differs from independent Python oracle: %s", a)
	}
	p.Rationale = "reason "
	b, err := UIMappingPayloadDigest(p, 16384)
	if err != nil || a == b {
		t.Fatal("trimmed rationale")
	}
}
