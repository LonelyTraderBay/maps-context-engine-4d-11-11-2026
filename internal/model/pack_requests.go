package model

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type ExpandRequest struct {
	PackToken      string `json:"pack_token"`
	NextCursor     string `json:"next_cursor"`
	MaxOutputBytes *int64 `json:"max_output_bytes,omitempty"`
}
type ValidateRequest struct {
	PackToken   *string `json:"pack_token,omitempty"`
	ResultToken *string `json:"result_token,omitempty"`
	Verify      *string `json:"verify,omitempty"`
}

func validToken(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= 32768
}

// These decoders validate wire structure; token authenticity and purpose are
// checked later by the token owner against its actual scope and principal.
func DecodeExpandRequestJSON(data []byte, maxBytes int) (ExpandRequest, error) {
	var r ExpandRequest
	fields, err := optionalObject(data, maxBytes, "pack_token", "next_cursor", "max_output_bytes")
	if err != nil {
		return r, err
	}
	if _, ok := fields["pack_token"]; !ok {
		return r, fmt.Errorf("missing pack token")
	}
	if _, ok := fields["next_cursor"]; !ok {
		return r, fmt.Errorf("missing cursor")
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return ExpandRequest{}, err
	}
	if !validToken(r.PackToken) || !validToken(r.NextCursor) || (r.MaxOutputBytes != nil && (*r.MaxOutputBytes < 4096 || *r.MaxOutputBytes > 131072)) {
		return ExpandRequest{}, fmt.Errorf("invalid expand request")
	}
	return r, nil
}
func DecodeValidateRequestJSON(data []byte, maxBytes int) (ValidateRequest, error) {
	var r ValidateRequest
	if _, err := optionalObject(data, maxBytes, "pack_token", "result_token", "verify"); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return ValidateRequest{}, err
	}
	if (r.PackToken == nil) == (r.ResultToken == nil) || (r.PackToken != nil && !validToken(*r.PackToken)) || (r.ResultToken != nil && !validToken(*r.ResultToken)) || (r.Verify != nil && *r.Verify != "workspace") {
		return ValidateRequest{}, fmt.Errorf("invalid validate request")
	}
	return r, nil
}
