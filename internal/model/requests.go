package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type PositionSelector struct {
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	ByteOffset  int64  `json:"byte_offset"`
}
type StatusRequest struct {
	Scope  *ScopeRequest `json:"scope,omitempty"`
	Verify *string       `json:"verify,omitempty"`
}
type LocateRequest struct {
	Scope    *ScopeRequest     `json:"scope,omitempty"`
	Mode     string            `json:"mode"`
	Query    *string           `json:"query,omitempty"`
	Position *PositionSelector `json:"position,omitempty"`
	Kinds    []string          `json:"kinds,omitempty"`
	Package  *string           `json:"package,omitempty"`
	Limit    int64             `json:"limit"`
	Cursor   *string           `json:"cursor,omitempty"`
}

func optionalObject(data []byte, maxBytes int, allowed ...string) (map[string]json.RawMessage, error) {
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("expected object")
	}
	names := map[string]bool{}
	for _, name := range allowed {
		names[name] = true
	}
	for name, value := range fields {
		if !names[name] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("unknown or null field %s", name)
		}
	}
	return fields, nil
}
func decodeOptionalScope(fields map[string]json.RawMessage, maxBytes int) (*ScopeRequest, error) {
	raw, ok := fields["scope"]
	if !ok {
		return nil, nil
	}
	scope, err := DecodeScopeRequestJSON(raw, maxBytes)
	if err != nil {
		return nil, err
	}
	delete(fields, "scope")
	return &scope, nil
}
func DecodeStatusRequestJSON(data []byte, maxBytes int) (StatusRequest, error) {
	var r StatusRequest
	fields, err := optionalObject(data, maxBytes, "scope", "verify")
	if err != nil {
		return r, err
	}
	r.Scope, err = decodeOptionalScope(fields, maxBytes)
	if err != nil {
		return StatusRequest{}, err
	}
	if raw, ok := fields["verify"]; ok {
		if err := json.Unmarshal(raw, &r.Verify); err != nil {
			return StatusRequest{}, err
		}
		if *r.Verify != "none" && *r.Verify != "workspace" {
			return StatusRequest{}, fmt.Errorf("invalid verification mode")
		}
	}
	return r, nil
}
func DecodeLocateRequestJSON(data []byte, maxBytes int) (LocateRequest, error) {
	var r LocateRequest
	fields, err := optionalObject(data, maxBytes, "scope", "mode", "query", "position", "kinds", "package", "limit", "cursor")
	if err != nil {
		return r, err
	}
	r.Scope, err = decodeOptionalScope(fields, maxBytes)
	if err != nil {
		return LocateRequest{}, err
	}
	if raw, ok := fields["position"]; ok {
		p, err := DecodePositionSelectorJSON(raw, maxBytes)
		if err != nil {
			return LocateRequest{}, err
		}
		r.Position = &p
		delete(fields, "position")
	}
	_, hasMode := fields["mode"]
	_, hasLimit := fields["limit"]
	remaining, err := json.Marshal(fields)
	if err != nil {
		return LocateRequest{}, err
	}
	var body LocateRequest
	if err := json.Unmarshal(remaining, &body); err != nil {
		return LocateRequest{}, err
	}
	body.Scope = r.Scope
	body.Position = r.Position
	if !hasMode {
		body.Mode = "symbol"
	}
	if !hasLimit {
		body.Limit = 20
	}
	switch body.Mode {
	case "symbol", "file", "module", "topic", "text":
	default:
		return LocateRequest{}, fmt.Errorf("invalid locate mode")
	}
	lengthOK := func(s string, max int) bool {
		return utf8.ValidString(s) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= max
	}
	if (body.Query == nil) == (body.Position == nil) || (body.Query != nil && !lengthOK(*body.Query, 512)) || (body.Package != nil && !lengthOK(*body.Package, 512)) || (body.Cursor != nil && !lengthOK(*body.Cursor, 32768)) || body.Limit < 1 || body.Limit > 100 || len(body.Kinds) > 16 {
		return LocateRequest{}, fmt.Errorf("invalid locate request")
	}
	for _, kind := range body.Kinds {
		if !validID(kind) {
			return LocateRequest{}, fmt.Errorf("invalid locate kind")
		}
	}
	return body, nil
}

// DecodePositionSelectorJSON validates identity only; callers verify the offset
// against captured bytes, membership and policy before use.
func DecodePositionSelectorJSON(data []byte, maxBytes int) (PositionSelector, error) {
	var p PositionSelector
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	if _, err := scopeObject(data, []string{"kind", "path", "content_hash", "byte_offset"}, nil); err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return PositionSelector{}, err
	}
	if p.Kind != "source_position" || !validSourcePath(p.Path) || !lowerHex(p.ContentHash, 64) || p.ByteOffset < 0 || p.ByteOffset > maxExactInteger {
		return PositionSelector{}, fmt.Errorf("invalid source position")
	}
	return p, nil
}
