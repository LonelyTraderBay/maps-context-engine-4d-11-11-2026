package contracts

import (
	"encoding/json"
	"testing"
)

func TestResponseOperationBindingsMatchSchema(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err = json.Unmarshal(schemaJSON, &d); err != nil {
		t.Fatal(err)
	}
	if len(responseOperation) != len(r.Operations()) {
		t.Fatal("response binding count mismatch")
	}
	for _, op := range r.Operations() {
		actual, ok := responseOperation[op.Name]
		if !ok {
			t.Fatalf("missing %s", op.Name)
		}
		var definition struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err = json.Unmarshal(d.Defs[op.Response], &definition); err != nil {
			t.Fatal(err)
		}
		var p struct {
			Const string   `json:"const"`
			Enum  []string `json:"enum"`
		}
		if err = json.Unmarshal(definition.Properties["operation"], &p); err != nil {
			t.Fatal(err)
		}
		match := p.Const == actual
		for _, v := range p.Enum {
			match = match || v == actual
		}
		if !match {
			t.Fatalf("%s binding %s outside %s", op.Name, actual, op.Response)
		}
	}
}
