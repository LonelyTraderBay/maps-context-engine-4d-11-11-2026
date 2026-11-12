package contracts

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

//go:embed operations.v1.json
var operationJSON []byte

//go:embed mce4d.v1.schema.json
var schemaJSON []byte

type Transport string

const (
	MCP  Transport = "MCP"
	HTTP Transport = "HTTP"
	CLI  Transport = "CLI"
)

type Operation struct {
	Name      string
	Transport Transport
	Request   string
	Response  string
}
type Registry struct{ operations map[string]Operation }

// LoadRegistry loads the S43 artifacts embedded at build time. It checks names,
// transport counts and schema references; it does not implement dispatch,
// authorize a caller or prove that a capability is implemented.
func LoadRegistry() (*Registry, error) {
	if _, err := model.DecodeIdentityJSON(operationJSON, len(operationJSON)); err != nil {
		return nil, err
	}
	var raw map[string][]string
	if err := json.Unmarshal(operationJSON, &raw); err != nil {
		return nil, err
	}
	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, err
	}
	entries := make(map[string]Operation, len(raw))
	counts := map[Transport]int{}
	for name, types := range raw {
		var transport Transport
		switch {
		case strings.HasPrefix(name, "mce4d_"):
			transport = MCP
		case strings.HasPrefix(name, "GET /"), strings.HasPrefix(name, "POST /"):
			transport = HTTP
		case strings.HasPrefix(name, "CLI "):
			transport = CLI
		default:
			return nil, fmt.Errorf("invalid operation name")
		}
		if len(types) != 2 || types[0] == "" || types[1] == "" {
			return nil, fmt.Errorf("invalid operation definition")
		}
		if _, ok := schema.Defs[types[0]]; !ok {
			return nil, fmt.Errorf("missing request schema")
		}
		if _, ok := schema.Defs[types[1]]; !ok {
			return nil, fmt.Errorf("missing response schema")
		}
		entries[name] = Operation{Name: name, Transport: transport, Request: types[0], Response: types[1]}
		counts[transport]++
	}
	if counts[MCP] != 6 || counts[HTTP] != 18 || counts[CLI] != 7 {
		return nil, fmt.Errorf("invalid operation transport counts")
	}
	return &Registry{operations: entries}, nil
}
func (r *Registry) Lookup(transport Transport, name string) (Operation, error) {
	if r == nil {
		return Operation{}, fmt.Errorf("registry unavailable")
	}
	op, ok := r.operations[name]
	if !ok || op.Transport != transport {
		return Operation{}, fmt.Errorf("operation unavailable for transport")
	}
	return op, nil
}
func (r *Registry) Operations() []Operation {
	if r == nil {
		return nil
	}
	result := make([]Operation, 0, len(r.operations))
	for _, op := range r.operations {
		result = append(result, op)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
