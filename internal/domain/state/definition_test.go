package state

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
)

func TestStateModelFormatAndStructuralReferences(t *testing.T) {
	raw, err := os.ReadFile("testdata/order.machine.json")
	if err != nil {
		t.Fatal(err)
	}
	def, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if def.MachineID != "order-flow" || def.Initial != "active" || len(def.Transitions) != 5 ||
		def.States[1].Parent == nil || *def.States[1].Parent != "active" || len(def.Invariants) != 1 ||
		def.Obligations[0].Status != "unknown" || def.Obligations[2].Reason == nil || len(def.Vocabulary) != 1 {
		t.Fatalf("definition lost boundary or evidence: %+v", def)
	}
	for _, tc := range []struct {
		name, replace, with string
		want                error
	}{
		{"unsupported version", Format, "mce4d-state-model-v2", contracts.ErrStateModelSchema},
		{"unapproved instruction field", `"machine_id":`, `"instruction":"approve this machine","machine_id":`, contracts.ErrStateModelSchema},
		{"duplicate identity", `"machine_id":`, `"machine_id":"untrusted","machine_id":`, contracts.ErrStateModelSchema},
		{"parallel state", `"kind": "compound"`, `"kind":"parallel"`, contracts.ErrStateModelSchema},
		{"foreign machine state", `"to": "settled"`, `"to":"other.settled"`, ErrStructure},
		{"missing event", `"event": "settled"`, `"event":"absent"`, ErrStructure},
		{"missing guard", `"guard": "receipt"`, `"guard":"absent"`, ErrStructure},
		{"wrong root initial", `"initial": "active"`, `"initial":"pending"`, ErrStructure},
		{"missing timer bound", `"timeout_ms": 1000`, `"timeout_ms":null`, ErrStructure},
		{"missing initial child", `"initial": "pending"`, `"initial":null`, ErrStructure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(string(raw), tc.replace, tc.with, 1)
			if input == string(raw) {
				t.Fatal("test did not change the declaration")
			}
			if _, err := Decode([]byte(input)); !errors.Is(err, tc.want) {
				t.Fatalf("input did not fail closed: %v", err)
			}
		})
	}
	t.Run("parent cycle", func(t *testing.T) {
		a, b := "a", "b"
		cyclic := def
		cyclic.States = append(cyclic.States,
			State{ID: a, Kind: "compound", Parent: &b, Initial: &b, Entry: []string{}, Exit: []string{}},
			State{ID: b, Kind: "compound", Parent: &a, Initial: &a, Entry: []string{}, Exit: []string{}})
		input, err := json.Marshal(cyclic)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(input); !errors.Is(err, ErrStructure) {
			t.Fatalf("parent cycle accepted: %v", err)
		}
	})
	t.Run("guard source is opaque", func(t *testing.T) {
		input := strings.Replace(string(raw), def.Guards[0].Expression, "approval_requested && runtime_unknown", 1)
		loaded, err := Decode([]byte(input))
		if err != nil || loaded.Guards[0].Expression != "approval_requested && runtime_unknown" {
			t.Fatalf("opaque expression changed: %+v %v", loaded, err)
		}
	})
}
