package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// DecodeIdentityJSON rejects lossy or ambiguous identity input before canonicalization.
// It does not replace the operation-specific schema and semantic validators.
func DecodeIdentityJSON(data []byte, maxBytes int) (CanonicalValue, error) {
	if maxBytes <= 0 || len(data) > maxBytes || !utf8.Valid(data) {
		return CanonicalValue{}, fmt.Errorf("invalid UTF-8 or identity input budget")
	}
	if err := validateSurrogates(data); err != nil {
		return CanonicalValue{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := identityValue(decoder, 0)
	if err != nil {
		return CanonicalValue{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return CanonicalValue{}, fmt.Errorf("trailing JSON value")
		}
		return CanonicalValue{}, err
	}
	return value, nil
}

func identityValue(decoder *json.Decoder, depth int) (CanonicalValue, error) {
	if depth > 32 {
		return CanonicalValue{}, fmt.Errorf("identity nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return CanonicalValue{}, err
	}
	switch v := token.(type) {
	case nil:
		return CanonicalNull(), nil
	case bool:
		return CanonicalBool(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return CanonicalValue{}, fmt.Errorf("identity number must be an exact integer")
		}
		return CanonicalInt(n)
	case string:
		return CanonicalString(v)
	case json.Delim:
		switch v {
		case '[':
			var items []CanonicalValue
			for decoder.More() {
				item, err := identityValue(decoder, depth+1)
				if err != nil {
					return CanonicalValue{}, err
				}
				items = append(items, item)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return CanonicalValue{}, fmt.Errorf("invalid JSON array")
			}
			return CanonicalArray(items), nil
		case '{':
			var fields []CanonicalField
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] || len(fields) >= 4096 {
					return CanonicalValue{}, fmt.Errorf("invalid, duplicate, or excessive JSON member")
				}
				seen[name] = true
				value, err := identityValue(decoder, depth+1)
				if err != nil {
					return CanonicalValue{}, err
				}
				fields = append(fields, CanonicalField{Key: name, Value: value})
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return CanonicalValue{}, fmt.Errorf("invalid JSON object")
			}
			return CanonicalObject(fields)
		}
	}
	return CanonicalValue{}, fmt.Errorf("unsupported identity JSON value")
}

// encoding/json replaces lone surrogate escapes, so reject them before decoding.
func validateSurrogates(data []byte) error {
	inside := false
	for i := 0; i < len(data); i++ {
		if !inside {
			if data[i] == '"' {
				inside = true
			}
			continue
		}
		if data[i] == '"' {
			inside = false
			continue
		}
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return fmt.Errorf("truncated JSON escape")
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return fmt.Errorf("truncated Unicode escape")
		}
		unit, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return fmt.Errorf("invalid Unicode escape")
		}
		i += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return fmt.Errorf("unpaired low surrogate")
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return fmt.Errorf("unpaired high surrogate")
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("invalid surrogate pair")
			}
			i += 6
		}
	}
	return nil
}
