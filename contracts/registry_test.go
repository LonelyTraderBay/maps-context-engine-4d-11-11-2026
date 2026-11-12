package contracts

import "testing"

func TestRegistryTransportBoundaries(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		transport           Transport
		name, input, output string
	}{
		{MCP, "mce4d_locate", "LocateRequest", "LocateData"},
		{HTTP, "GET /workspace", "StatusRequest", "StatusData"},
		{CLI, "CLI config", "RuntimeConfigRequest", "RuntimeConfigData"},
	} {
		op, err := registry.Lookup(tc.transport, tc.name)
		if err != nil || op.Request != tc.input || op.Response != tc.output {
			t.Fatalf("%s: %+v %v", tc.name, op, err)
		}
	}
	for _, tc := range []struct {
		transport Transport
		name      string
	}{{HTTP, "CLI config"}, {MCP, "CLI group"}, {CLI, "POST /graph/query"}, {HTTP, "POST /missing"}} {
		if _, err := registry.Lookup(tc.transport, tc.name); err == nil {
			t.Fatalf("accepted %s", tc.name)
		}
	}
	if len(registry.Operations()) != 31 {
		t.Fatal("registry incomplete")
	}
}
