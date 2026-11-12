package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"unicode/utf8"
)

// RequestDigest hashes the validated, defaulted request received for an
// operation. The principal must come from authenticated server context; cursor
// omission for continuation comparison is a separate operation-specific step.
func RequestDigest(operation, principalID, schema string, request Request, maxBytes, cpus int) (string, error) {
	if !validID(operation) || principalID == "" || !utf8.ValidString(principalID) || request == nil {
		return "", fmt.Errorf("invalid request identity")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	validated, err := DecodeRequestJSON(schema, raw, maxBytes, cpus)
	if err != nil {
		return "", err
	}
	if reflect.TypeOf(validated) != reflect.TypeOf(request) {
		return "", fmt.Errorf("request schema mismatch")
	}
	raw, err = json.Marshal(validated)
	if err != nil {
		return "", err
	}
	v, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	op, err := CanonicalString(operation)
	if err != nil {
		return "", err
	}
	principal, err := CanonicalString(principalID)
	if err != nil {
		return "", err
	}
	value, err := CanonicalObject([]CanonicalField{{Key: "operation", Value: op}, {Key: "principal_id", Value: principal}, {Key: "request", Value: v}})
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("request-v1", value, maxBytes)
	return digest, err
}

// UIMappingPayloadDigest validates and hashes the closed supported payload; it
// neither establishes source authorization nor approves a record.
func UIMappingPayloadDigest(payload UIMappingPayload, maxBytes int) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	valid, err := DecodeUIMappingPayloadJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	raw, err = json.Marshal(valid)
	if err != nil {
		return "", err
	}
	v, err := DecodeIdentityJSON(raw, maxBytes)
	if err != nil {
		return "", err
	}
	_, digest, err := EncodeCanonical("record-payload-v1", v, maxBytes)
	return digest, err
}
