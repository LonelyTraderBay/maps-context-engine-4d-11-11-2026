package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestBridgePrimitiveShapes(t *testing.T) {
	h := strings.Repeat("a", 64)
	id := "01234567-89ab-cdef-0123-456789abcdef"
	anchor := fmt.Sprintf(`{"build_map_id":"%s","anchor_id":"%s"}`, h, h)
	if _, err := DecodeDOMAnchorRefJSON([]byte(anchor), 8192); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeDOMAnchorRefJSON([]byte(strings.Replace(anchor, `"anchor_id":`, `"source_ref":{},"anchor_id":`, 1)), 8192); err == nil {
		t.Fatal("accepted engine source ref in DOM anchor")
	}
	instance := fmt.Sprintf(`{"session_id":"%s","document_epoch":1,"hmr_epoch":0,"instance_id":"i1"}`, id)
	if _, err := DecodeInstanceRefJSON([]byte(instance), 8192); err != nil {
		t.Fatal(err)
	}
	rect, err := DecodeRectJSON([]byte(`{"x":1.5,"y":-2.25,"width":10,"height":5}`), 8192)
	if err != nil {
		t.Fatal(err)
	}
	if err := rect.ValidateBounds(100); err != nil {
		t.Fatal(err)
	}
	if err := rect.ValidateBounds(5); err == nil {
		t.Fatal("ignored caller bounds")
	}
	for _, bad := range []string{`{"x":0,"y":0,"width":-1,"height":1}`, `{"x":1e309,"y":0,"width":1,"height":1}`, `{"x":0,"y":0,"width":1,"height":1,"text":"secret"}`} {
		if _, err := DecodeRectJSON([]byte(bad), 8192); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
