package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidateWireJSON rejects ambiguous and unbounded JSON without coercion. Unlike
// identity decoding, finite fractional numbers are allowed for display data.
// Callers must still apply the appropriate schema and semantic validators.
func ValidateWireJSON(data []byte, maxBytes int) error {
	if maxBytes <= 0 || len(data) > maxBytes || !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8 or wire budget")
	}
	if err := validateSurrogates(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := wireValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}
	return nil
}
func wireValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return fmt.Errorf("wire nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch v := token.(type) {
	case nil, bool, string:
		return nil
	case json.Number:
		n, err := strconv.ParseFloat(v.String(), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("invalid numeric value")
		}
		if n == 0 {
			mantissa := strings.SplitN(strings.ToLower(v.String()), "e", 2)[0]
			for _, digit := range mantissa {
				if digit >= '1' && digit <= '9' {
					return fmt.Errorf("numeric value underflows")
				}
			}
		}
		return nil
	case json.Delim:
		switch v {
		case '[':
			for decoder.More() {
				if err := wireValue(decoder, depth+1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return fmt.Errorf("invalid array")
			}
			return nil
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] || len(seen) >= 4096 {
					return fmt.Errorf("invalid, duplicate or excessive object member")
				}
				seen[name] = true
				if err := wireValue(decoder, depth+1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return fmt.Errorf("invalid object")
			}
			return nil
		}
	}
	return fmt.Errorf("invalid wire value")
}
