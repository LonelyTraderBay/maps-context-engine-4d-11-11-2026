package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"
)

const maxExactInteger int64 = 9007199254740991

type canonicalKind uint8

const (
	canonicalNull canonicalKind = iota
	canonicalBool
	canonicalInt
	canonicalString
	canonicalArray
	canonicalObject
)

// CanonicalValue is a typed codec input, not a substitute for schema or authorization checks.
type CanonicalValue struct {
	kind   canonicalKind
	flag   bool
	int64  int64
	text   string
	items  []CanonicalValue
	fields []CanonicalField
}

type CanonicalField struct {
	Key   string
	Value CanonicalValue
}

func CanonicalNull() CanonicalValue { return CanonicalValue{} }

func CanonicalBool(v bool) CanonicalValue {
	return CanonicalValue{kind: canonicalBool, flag: v}
}

func CanonicalInt(v int64) (CanonicalValue, error) {
	if v < -maxExactInteger || v > maxExactInteger {
		return CanonicalValue{}, fmt.Errorf("integer outside exact range")
	}
	return CanonicalValue{kind: canonicalInt, int64: v}, nil
}

func CanonicalString(v string) (CanonicalValue, error) {
	if !utf8.ValidString(v) {
		return CanonicalValue{}, fmt.Errorf("invalid UTF-8 string")
	}
	return CanonicalValue{kind: canonicalString, text: v}, nil
}

func CanonicalArray(values []CanonicalValue) CanonicalValue {
	return CanonicalValue{kind: canonicalArray, items: append([]CanonicalValue(nil), values...)}
}

func CanonicalObject(fields []CanonicalField) (CanonicalValue, error) {
	if len(fields) > 4096 {
		return CanonicalValue{}, fmt.Errorf("too many object members")
	}
	copyFields := append([]CanonicalField(nil), fields...)
	for _, field := range copyFields {
		if !utf8.ValidString(field.Key) {
			return CanonicalValue{}, fmt.Errorf("invalid UTF-8 object key")
		}
	}
	sort.Slice(copyFields, func(i, j int) bool { return copyFields[i].Key < copyFields[j].Key })
	for i := 1; i < len(copyFields); i++ {
		if copyFields[i-1].Key == copyFields[i].Key {
			return CanonicalValue{}, fmt.Errorf("duplicate object member")
		}
	}
	return CanonicalValue{kind: canonicalObject, fields: copyFields}, nil
}

// EncodeCanonical encodes validated values using MCE-C14N1 with an explicit byte cap.
// Callers must enforce the schema and recipe for the specific domain before calling it.
func EncodeCanonical(domain string, value CanonicalValue, maxBytes int) ([]byte, string, error) {
	if domain == "" || !utf8.ValidString(domain) || maxBytes <= 0 {
		return nil, "", fmt.Errorf("invalid canonical domain or budget")
	}
	w := canonicalWriter{limit: maxBytes}
	if err := w.write("MCE4D\x00C14N1\x00"); err != nil {
		return nil, "", err
	}
	if err := w.stringValue(domain); err != nil {
		return nil, "", err
	}
	if err := w.value(value, 0); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(w.data)
	return w.data, hex.EncodeToString(sum[:]), nil
}

type canonicalWriter struct {
	data  []byte
	limit int
}

func (w *canonicalWriter) write(value string) error {
	if len(value) > w.limit-len(w.data) {
		return fmt.Errorf("canonical output exceeds budget")
	}
	w.data = append(w.data, value...)
	return nil
}

func (w *canonicalWriter) stringValue(value string) error {
	if err := w.write("s" + strconv.Itoa(len(value)) + ":"); err != nil {
		return err
	}
	return w.write(value)
}

func (w *canonicalWriter) value(v CanonicalValue, depth int) error {
	if depth > 32 {
		return fmt.Errorf("canonical nesting exceeds limit")
	}
	switch v.kind {
	case canonicalNull:
		return w.write("n")
	case canonicalBool:
		if v.flag {
			return w.write("t")
		}
		return w.write("f")
	case canonicalInt:
		return w.write("i" + strconv.FormatInt(v.int64, 10) + ";")
	case canonicalString:
		return w.stringValue(v.text)
	case canonicalArray:
		if err := w.write("a" + strconv.Itoa(len(v.items)) + ":"); err != nil {
			return err
		}
		for _, item := range v.items {
			if err := w.value(item, depth+1); err != nil {
				return err
			}
		}
	case canonicalObject:
		if err := w.write("o" + strconv.Itoa(len(v.fields)) + ":"); err != nil {
			return err
		}
		for _, field := range v.fields {
			if err := w.stringValue(field.Key); err != nil {
				return err
			}
			if err := w.value(field.Value, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid canonical value kind")
	}
	return nil
}
