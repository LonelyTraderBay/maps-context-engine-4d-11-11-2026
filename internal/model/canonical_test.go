package model

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalVectorsFromSpecification(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Maps-Context-Engine-4D_SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "<!-- BEGIN MCE4D_HASH_VECTORS_V1 -->")
	end := strings.Index(text, "<!-- END MCE4D_HASH_VECTORS_V1 -->")
	if start < 0 || end <= start {
		t.Fatal("hash vector block missing")
	}
	block := text[start:end]
	start = strings.Index(block, "```json\n")
	end = strings.LastIndex(block, "```")
	if start < 0 || end <= start+8 {
		t.Fatal("hash vector JSON fence missing")
	}
	var document struct {
		Valid []struct {
			Case     string          `json:"case"`
			Domain   string          `json:"domain"`
			Value    json.RawMessage `json:"value"`
			Encoding string          `json:"encoding_hex"`
			Digest   string          `json:"digest"`
		} `json:"valid"`
	}
	if err := json.Unmarshal([]byte(block[start+8:end]), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Valid) != 14 {
		t.Fatalf("got %d vectors, want 14", len(document.Valid))
	}
	for _, vector := range document.Valid {
		t.Run(vector.Case, func(t *testing.T) {
			decoder := json.NewDecoder(strings.NewReader(string(vector.Value)))
			decoder.UseNumber()
			var raw any
			if err := decoder.Decode(&raw); err != nil {
				t.Fatal(err)
			}
			value := fixtureValue(t, raw)
			encoded, hash, err := EncodeCanonical(vector.Domain, value, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(encoded) != vector.Encoding || hash != vector.Digest {
				t.Fatalf("encoding=%x hash=%s", encoded, hash)
			}
			decoded, err := DecodeIdentityJSON(vector.Value, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			encoded, hash, err = EncodeCanonical(vector.Domain, decoded, 1<<20)
			if err != nil || hex.EncodeToString(encoded) != vector.Encoding || hash != vector.Digest {
				t.Fatalf("decoded encoding=%x hash=%s err=%v", encoded, hash, err)
			}
		})
	}
}

func TestIdentityJSONRejectsAmbiguousOrInvalidInput(t *testing.T) {
	for _, raw := range []string{
		`{"a":1,"\u0061":2}`,
		`"\ud800"`, `"\udc00"`, `"\ud800x"`,
		`1.0`, `1e0`, `9007199254740992`, `-9007199254740992`,
		`{} {}`, string([]byte{'"', 0xff, '"'}),
		strings.Repeat("[", 34) + "null" + strings.Repeat("]", 34),
	} {
		if _, err := DecodeIdentityJSON([]byte(raw), 1<<20); err == nil {
			t.Errorf("invalid identity input accepted: %q", raw)
		}
	}
	if _, err := DecodeIdentityJSON([]byte(`"abc"`), 2); err == nil {
		t.Fatal("input over budget accepted")
	}
	value, err := DecodeIdentityJSON([]byte(`"\ud83d\ude00"`), 100)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := EncodeCanonical("test-v1", value, 100)
	if err != nil || !strings.HasSuffix(string(encoded), "s4:😀") {
		t.Fatalf("valid surrogate pair encoding=%q err=%v", encoded, err)
	}
}

func fixtureValue(t *testing.T, raw any) CanonicalValue {
	t.Helper()
	switch v := raw.(type) {
	case nil:
		return CanonicalNull()
	case bool:
		return CanonicalBool(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			t.Fatal(err)
		}
		out, err := CanonicalInt(n)
		if err != nil {
			t.Fatal(err)
		}
		return out
	case string:
		out, err := CanonicalString(v)
		if err != nil {
			t.Fatal(err)
		}
		return out
	case []any:
		items := make([]CanonicalValue, len(v))
		for i := range v {
			items[i] = fixtureValue(t, v[i])
		}
		return CanonicalArray(items)
	case map[string]any:
		fields := make([]CanonicalField, 0, len(v))
		for k, value := range v {
			fields = append(fields, CanonicalField{Key: k, Value: fixtureValue(t, value)})
		}
		out, err := CanonicalObject(fields)
		if err != nil {
			t.Fatal(err)
		}
		return out
	default:
		t.Fatalf("unexpected fixture value %T", raw)
		return CanonicalNull()
	}
}

func TestCanonicalRejectsInvalidValuesAndBounds(t *testing.T) {
	if _, err := CanonicalInt(9007199254740992); err == nil {
		t.Fatal("integer outside exact range accepted")
	}
	if _, err := CanonicalString(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, err := CanonicalObject([]CanonicalField{{Key: "x"}, {Key: "x"}}); err == nil {
		t.Fatal("duplicate object member accepted")
	}
	deep := CanonicalNull()
	for i := 0; i < 34; i++ {
		deep = CanonicalArray([]CanonicalValue{deep})
	}
	if _, _, err := EncodeCanonical("test-v1", deep, 1024); err == nil {
		t.Fatal("excessive depth accepted")
	}
	if _, _, err := EncodeCanonical("test-v1", CanonicalNull(), 3); err == nil {
		t.Fatal("output over budget accepted")
	}
}
