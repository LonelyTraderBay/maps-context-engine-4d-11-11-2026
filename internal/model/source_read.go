package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type SourceReadRequest struct {
	SourceRef      SourceRef `json:"source_ref"`
	MaxOutputBytes *int64    `json:"max_output_bytes,omitempty"`
}
type SourceData struct {
	Operation string    `json:"operation"`
	SourceRef SourceRef `json:"source_ref"`
	Content   string    `json:"content"`
}

func DecodeSourceReadRequestJSON(data []byte, maxBytes int) (SourceReadRequest, error) {
	var r SourceReadRequest
	fields, err := optionalObject(data, maxBytes, "source_ref", "max_output_bytes")
	if err != nil {
		return r, err
	}
	raw, ok := fields["source_ref"]
	if !ok {
		return r, fmt.Errorf("missing source ref")
	}
	ref, err := DecodeSourceRefJSON(raw, maxBytes)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return SourceReadRequest{}, err
	}
	r.SourceRef = ref
	if !validOutputCap(r.MaxOutputBytes) {
		return SourceReadRequest{}, fmt.Errorf("invalid source output budget")
	}
	return r, nil
}
func DecodeSourceDataJSON(data []byte, maxBytes int) (SourceData, error) {
	var r SourceData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	fields, err := scopeObject(data, []string{"operation", "source_ref", "content"}, nil)
	if err != nil {
		return r, err
	}
	ref, err := DecodeSourceRefJSON(fields["source_ref"], maxBytes)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return SourceData{}, err
	}
	r.SourceRef = ref
	if r.Operation != "source_read" || !utf8.ValidString(r.Content) || utf8.RuneCountInString(r.Content) > 131072 || int64(len(r.Content)) != r.SourceRef.Range.EndByte-r.SourceRef.Range.StartByte {
		return SourceData{}, fmt.Errorf("invalid source data")
	}
	return r, nil
}

// ValidateCapturedContent checks exact byte equality with the declared range.
// It does not prove that the source was authorized or obtained securely.
func (r SourceData) ValidateCapturedContent(source []byte) error {
	if r.Operation != "source_read" || !utf8.ValidString(r.Content) || utf8.RuneCountInString(r.Content) > 131072 || int64(len(r.Content)) != r.SourceRef.Range.EndByte-r.SourceRef.Range.StartByte {
		return fmt.Errorf("invalid source data")
	}
	if err := r.SourceRef.ValidateCapturedBytes(source); err != nil {
		return err
	}
	if !bytes.Equal([]byte(r.Content), source[int(r.SourceRef.Range.StartByte):int(r.SourceRef.Range.EndByte)]) {
		return fmt.Errorf("source content does not match range")
	}
	return nil
}
