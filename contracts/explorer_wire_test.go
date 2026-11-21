package contracts

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

//go:embed explorer-wire.v1.json
var explorerWireJSON []byte

func TestExplorerWireParity(t *testing.T) {
	if _, err := model.DecodeIdentityJSON(explorerWireJSON, len(explorerWireJSON)); err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Bridge map[model.BridgeDirection][]string `json:"bridge"`
		HTTP   map[string][]json.RawMessage       `json:"http"`
	}
	dec := json.NewDecoder(strings.NewReader(string(explorerWireJSON)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		t.Fatal(err)
	}
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	httpCount := 0
	for _, op := range registry.Operations() {
		if op.Transport != HTTP {
			continue
		}
		httpCount++
		want, err := json.Marshal([]any{responseOperation[op.Name], op.RequestHardLimit(), op.responseHardLimit()})
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(wire.HTTP[op.Name])
		if err != nil || string(got) != string(want) {
			t.Fatalf("%s contract mismatch: got %s, want %s", op.Name, got, want)
		}
	}
	if len(wire.HTTP) != httpCount {
		t.Fatalf("got %d HTTP routes, want %d", len(wire.HTTP), httpCount)
	}

	var schemas struct {
		Defs map[string]struct {
			OneOf []struct {
				Ref string `json:"$ref"`
			} `json:"oneOf"`
			Properties map[string]struct {
				Const json.RawMessage `json:"const"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaJSON, &schemas); err != nil {
		t.Fatal(err)
	}
	if len(wire.Bridge) != 2 {
		t.Fatal("expected two bridge directions")
	}
	for _, direction := range []model.BridgeDirection{model.ParentToSDK, model.SDKToParent} {
		actual := make(map[string]bool)
		for _, message := range wire.Bridge[direction] {
			if actual[message] || !model.BridgeDirectionAllowed(message, direction) {
				t.Fatalf("unexpected %s in %s", message, direction)
			}
			actual[message] = true
		}
		variants := schemas.Defs["BridgeMessage"].OneOf
		if len(variants) != 23 {
			t.Fatalf("expected 23 bridge variants, got %d", len(variants))
		}
		for _, variant := range variants {
			def := schemas.Defs[strings.TrimPrefix(variant.Ref, "#/$defs/")]
			var message string
			if err := json.Unmarshal(def.Properties["type"].Const, &message); err != nil ||
				message == "" || actual[message] != model.BridgeDirectionAllowed(message, direction) {
				t.Fatalf("incorrect direction registry for %s in %s", message, direction)
			}
			delete(actual, message)
		}
		if len(actual) != 0 {
			t.Fatalf("registered messages absent from bridge schema: %v", actual)
		}
	}
}
