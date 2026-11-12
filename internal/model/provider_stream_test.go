package model

import (
	"strings"
	"testing"
)

func TestProviderStreamSequencesAndDigest(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	s, err := BeginProviderStream(id, h)
	if err != nil {
		t.Fatal(err)
	}
	batch := &ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: id, Sequence: 1, Packages: []PackageFact{}, Symbols: []SymbolFact{}, Relations: []RelationFact{}, Diagnostics: []DiagnosticFact{}}
	bad := *batch
	bad.Sequence = 2
	if _, err := AcceptProviderOutput(s, &bad, 16384); err == nil {
		t.Fatal("accepted skipped batch")
	}
	next, err := AcceptProviderOutput(s, batch, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptProviderOutput(next, batch, 16384); err == nil {
		t.Fatal("accepted duplicate batch")
	}
	done := &ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: id, Sequence: 2, Status: "complete_in_supported_scope", InputDigest: h, Capabilities: []Capability{}, Gaps: []Gap{}}
	wrong := *done
	wrong.InputDigest = strings.Repeat("b", 64)
	if _, err := AcceptProviderOutput(next, &wrong, 16384); err == nil {
		t.Fatal("accepted mismatched capture")
	}
	if _, err := next.TerminalStatus(); err == nil {
		t.Fatal("missing Done accepted")
	}
	terminal, err := AcceptProviderOutput(next, done, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := terminal.TerminalStatus(); err != nil || status != "complete_in_supported_scope" {
		t.Fatalf("terminal status %s %v", status, err)
	}
	if _, err := AcceptProviderOutput(terminal, done, 16384); err == nil {
		t.Fatal("accepted post-terminal output")
	}
}
