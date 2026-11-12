package model

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
)

type ProviderBatch struct {
	ProviderProtocol int64            `json:"provider_protocol"`
	Type             string           `json:"type"`
	JobID            string           `json:"job_id"`
	Sequence         int64            `json:"sequence"`
	Packages         []PackageFact    `json:"packages"`
	Symbols          []SymbolFact     `json:"symbols"`
	Relations        []RelationFact   `json:"relations"`
	Diagnostics      []DiagnosticFact `json:"diagnostics"`
}
type ProviderDone struct {
	ProviderProtocol int64        `json:"provider_protocol"`
	Type             string       `json:"type"`
	JobID            string       `json:"job_id"`
	Sequence         int64        `json:"sequence"`
	Status           string       `json:"status"`
	InputDigest      string       `json:"input_digest"`
	Capabilities     []Capability `json:"capabilities"`
	Gaps             []Gap        `json:"gaps"`
	Error            *ErrorInfo   `json:"error"`
}
type ProviderCancel struct {
	ProviderProtocol int64  `json:"provider_protocol"`
	Type             string `json:"type"`
	JobID            string `json:"job_id"`
}

// DecodeProviderBatchJSON validates a bounded frame's facts, not job order,
// endpoint membership, actual source bytes or transport authenticity.
func DecodeProviderBatchJSON(data []byte, maxBytes int) (ProviderBatch, error) {
	maxBytes = providerFrameLimit(maxBytes)
	var p ProviderBatch
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	f, err := scopeObject(data, []string{"provider_protocol", "type", "job_id", "sequence", "packages", "symbols", "relations", "diagnostics"}, nil)
	if err != nil {
		return p, err
	}
	for _, spec := range []struct {
		name  string
		limit int
	}{{"packages", 512}, {"symbols", 2048}, {"relations", 4096}, {"diagnostics", 512}} {
		var items []json.RawMessage
		if err := json.Unmarshal(f[spec.name], &items); err != nil {
			return p, err
		}
		if len(items) > spec.limit {
			return p, fmt.Errorf("too many provider facts")
		}
		for _, item := range items {
			switch spec.name {
			case "packages":
				_, err = DecodePackageFactJSON(item, maxBytes)
			case "symbols":
				_, err = DecodeSymbolFactJSON(item, maxBytes)
			case "relations":
				_, err = DecodeRelationFactJSON(item, maxBytes)
			case "diagnostics":
				_, err = DecodeDiagnosticFactJSON(item, maxBytes)
			}
			if err != nil {
				return p, err
			}
		}
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return ProviderBatch{}, err
	}
	if p.ProviderProtocol != 1 || p.Type != "analyze_batch" || !uuid(p.JobID) || p.Sequence < 1 || p.Sequence > maxExactInteger || p.Packages == nil || p.Symbols == nil || p.Relations == nil || p.Diagnostics == nil {
		return ProviderBatch{}, fmt.Errorf("invalid provider batch")
	}
	return p, nil
}
func DecodeProviderDoneJSON(data []byte, maxBytes int) (ProviderDone, error) {
	maxBytes = providerFrameLimit(maxBytes)
	var p ProviderDone
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	f, err := scopeObject(data, []string{"provider_protocol", "type", "job_id", "sequence", "status", "input_digest", "capabilities", "gaps", "error"}, map[string]bool{"error": true})
	if err != nil {
		return p, err
	}
	var caps, gaps []json.RawMessage
	if err := json.Unmarshal(f["capabilities"], &caps); err != nil {
		return p, err
	}
	if err := json.Unmarshal(f["gaps"], &gaps); err != nil {
		return p, err
	}
	if len(caps) > 128 || len(gaps) > 64 {
		return p, fmt.Errorf("too many provider outcomes")
	}
	for _, x := range caps {
		if _, err := DecodeCapabilityJSON(x, maxBytes); err != nil {
			return p, err
		}
	}
	for _, x := range gaps {
		if _, err := DecodeGapJSON(x, maxBytes); err != nil {
			return p, err
		}
	}
	if !bytes.Equal(bytes.TrimSpace(f["error"]), []byte("null")) {
		if _, err := DecodeErrorInfoJSON(f["error"], maxBytes); err != nil {
			return p, err
		}
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return ProviderDone{}, err
	}
	if p.ProviderProtocol != 1 || p.Type != "analyze_done" || !uuid(p.JobID) || p.Sequence < 1 || p.Sequence > maxExactInteger || !lowerHex(p.InputDigest, 64) || p.Capabilities == nil || p.Gaps == nil {
		return ProviderDone{}, fmt.Errorf("invalid provider outcome")
	}
	switch p.Status {
	case "complete_in_supported_scope", "partial", "failed", "cancelled":
	default:
		return ProviderDone{}, fmt.Errorf("invalid provider status")
	}
	return p, nil
}
func (p ProviderDone) VerifyInputDigest(expected string) error {
	if !lowerHex(expected, 64) || !lowerHex(p.InputDigest, 64) || subtle.ConstantTimeCompare([]byte(expected), []byte(p.InputDigest)) != 1 {
		return fmt.Errorf("provider input digest mismatch")
	}
	return nil
}
func DecodeProviderCancelJSON(data []byte, maxBytes int) (ProviderCancel, error) {
	maxBytes = providerFrameLimit(maxBytes)
	var p ProviderCancel
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	if _, err := scopeObject(data, []string{"provider_protocol", "type", "job_id"}, nil); err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return ProviderCancel{}, err
	}
	if p.ProviderProtocol != 1 || p.Type != "cancel" || !uuid(p.JobID) {
		return ProviderCancel{}, fmt.Errorf("invalid provider cancel")
	}
	return p, nil
}
