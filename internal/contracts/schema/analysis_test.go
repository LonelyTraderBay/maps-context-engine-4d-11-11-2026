package schema

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var profile = Profile{ServiceID: "inventory", Environment: "fixture", Namespace: "inventory.local"}

func captured(name, kind, version, text string) Capture {
	return Capture{Kind: kind, Version: version, Bytes: []byte(text), Ref: model.SourceRef{
		WorktreeID: "01234567-0123-4123-8123-012345678901", ProfileID: strings.Repeat("a", 64),
		GenerationID: "11234567-0123-4123-8123-012345678901", Path: name,
		ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), Range: model.ByteRange{EndByte: int64(len(text))},
	}}
}

const draft = `"$schema":"https://json-schema.org/draft/2020-12/schema"`

func requireGaps(t *testing.T, out Analysis, codes ...string) {
	t.Helper()
	for _, code := range codes {
		found := false
		for _, gap := range out.Coverage.Gaps {
			found = found || gap.Code == code && len(gap.SourceRefs) == 1
		}
		if !found {
			t.Fatalf("missing gap %s in %+v", code, out.Coverage.Gaps)
		}
	}
}

func refsAreCaptured(t *testing.T, out Analysis, captures []Capture) {
	t.Helper()
	corpus := make(map[string][]byte)
	for _, c := range captures {
		corpus[c.Ref.Path] = c.Bytes
	}
	check := func(ref model.SourceRef) {
		t.Helper()
		if err := ref.ValidateCapturedBytes(corpus[ref.Path]); err != nil || ref.Range.EndByte <= ref.Range.StartByte {
			t.Fatalf("invalid source ref %+v: %v", ref, err)
		}
	}
	for _, entity := range out.Batch.Entities {
		check(entity.Declaration)
	}
	for _, field := range out.Batch.Fields {
		check(field.Declaration)
	}
	for _, link := range out.Batch.Links {
		for _, ref := range link.Evidence.SourceRefs {
			check(ref)
		}
	}
	for _, gap := range out.Coverage.Gaps {
		for _, ref := range gap.SourceRefs {
			check(ref)
		}
	}
}

func TestC060LocalSchemasPreserveVersionFieldsAndByteOffsets(t *testing.T) {
	captures := []Capture{
		captured("schemas/input.json", "json_schema", "2026.1", "{"+draft+`,
		  "description":"Mặt hàng", "type":"object","properties":{
		    "details":{"$ref":"shared.json#/$defs/USD~1~0"},
		    "note":{"type":["string","null"],"default":"not submitted"}
		  }}`),
		captured("schemas/shared.json", "json_schema", "2026.1", "{"+draft+`,"$defs":{"USD/~":{
		  "type":"object","properties":{"total":{"type":"number","default":1.75,"x-currency":"USD","x-precision":2}}
		}}}`),
	}
	ctx := context.Background()
	out, err := AnalyzeCaptured(ctx, profile, captures)
	if err != nil || len(out.Coverage.Gaps) != 0 || len(out.Batch.Entities) != 3 ||
		len(out.Batch.Fields) != 3 || len(out.Batch.Links) != 1 {
		t.Fatalf("schema capture: %+v, %v", out, err)
	}
	link := out.Batch.Links[0]
	ref := link.Evidence.SourceRefs[0]
	if got := string(captures[0].Bytes[ref.Range.StartByte:ref.Range.EndByte]); got != `"shared.json#/$defs/USD~1~0"` {
		t.Fatalf("reference byte span = %q", got)
	}
	for _, field := range out.Batch.Fields {
		switch field.FieldPath {
		case "/properties/note":
			if field.Type != "string" || field.Nullable == nil || !*field.Nullable {
				t.Fatalf("nullable union lost: %+v", field)
			}
		case "/properties/total":
			if field.Type != "number" || field.Nullable == nil || *field.Nullable || field.Currency == nil ||
				*field.Currency != "USD" || field.Precision == nil || *field.Precision != 2 {
				t.Fatalf("monetary metadata lost: %+v", field)
			}
		}
	}
	refsAreCaptured(t, out, captures)
	reordered, err := AnalyzeCaptured(ctx, profile, []Capture{captures[1], captures[0]})
	if err != nil || !reflect.DeepEqual(out, reordered) {
		t.Fatalf("capture ordering changed projection: %v", err)
	}
	captures[1].Version = "2026.2"
	mismatch, err := AnalyzeCaptured(ctx, profile, captures)
	if err != nil || len(mismatch.Batch.Links) != 0 {
		t.Fatalf("cross-version link formed: %+v %v", mismatch, err)
	}
	requireGaps(t, mismatch, "CONTRACT_REF_VERSION_MISMATCH")
}

func TestC060RemoteCyclesScopeAndMalformedJSONProduceGaps(t *testing.T) {
	captures := []Capture{
		captured("one.json", "json_schema", "v1", "{"+draft+`,"$defs":{
		 "start":{"$ref":"two.json#/$defs/end"}, "remote":{"$ref":"https://schema.invalid/item"},
		 "escaped":{"$ref":"../outside.json"}, "unseen":{"$ref":"absent.json"},
		 "scope":{"$id":"alternate.json","$ref":"two.json#/$defs/end"}
		}}`),
		captured("two.json", "json_schema", "v1", "{"+draft+`,"$defs":{"end":{"$ref":"one.json#/$defs/start"}}}`),
		captured("duplicate.json", "json_schema", "v1", "{"+draft+`,"type":"object","\u0074ype":"string"}`),
		captured("outside.yaml", "openapi", "v1", "openapi: 3.1.1"),
		captured("old.json", "json_schema", "v1", `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`),
	}
	out, err := AnalyzeCaptured(context.Background(), profile, captures)
	if err != nil || len(out.Batch.Links) != 0 || out.Coverage.Status != "partial" {
		t.Fatalf("unsupported references were linked: %+v %v", out, err)
	}
	requireGaps(t, out, "CONTRACT_REF_CYCLE", "CONTRACT_REF_REMOTE", "CONTRACT_REF_OUTSIDE_CAPTURE",
		"CONTRACT_SCHEMA_SCOPE_UNSUPPORTED", "CONTRACT_JSON_UNSUPPORTED", "CONTRACT_DIALECT_UNSUPPORTED")
	refsAreCaptured(t, out, captures)
}

func TestC060OpenAPIMinorVersionsAndDirections(t *testing.T) {
	v30 := `{"openapi":"3.0.4","info":{"version":"v1"},"servers":[{"url":"/api/v1"}],"paths":{
	 "/orders":{"post":{"operationId":"postOrders",
	   "requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"note":{"type":"string","nullable":true}}}}}},
	   "responses":{"200":{"content":{"application/json":{"schema":{"type":"object","properties":{
	      "ok":{"type":"boolean"},"item":{"$ref":"other.json#/components/schemas/Order"}}}}}}}
	  }}
	}}`
	v31 := `{"openapi":"3.1.1","info":{"version":"v1"},"paths":{},"components":{"schemas":{"Order":{"type":"object"}}}}`
	captures := []Capture{captured("api.json", "openapi", "v1", v30), captured("other.json", "openapi", "v1", v31)}
	out, err := AnalyzeCaptured(context.Background(), profile, captures)
	if err != nil || len(out.Operations) != 1 || len(out.Batch.Fields) != 3 || len(out.Batch.Links) != 2 {
		t.Fatalf("OAS declarations: %+v %v", out, err)
	}
	op := out.Operations[0]
	if op.Entity.Plane != "intended" || op.Server != "/api/v1" || op.OperationID != "postOrders" || !op.Bindable ||
		op.Method != "POST" || op.Path != "/orders" {
		t.Fatalf("incorrect operation: %+v", op)
	}
	for _, field := range out.Batch.Fields {
		if field.FieldPath == "/properties/note" && (field.Direction != "input" || field.Nullable == nil || !*field.Nullable) ||
			field.FieldPath != "/properties/note" && field.Direction != "output" {
			t.Fatalf("wrong field direction or type: %+v", field)
		}
	}
	requireGaps(t, out, "CONTRACT_REF_VERSION_MISMATCH")
	refsAreCaptured(t, out, captures)
}

func TestC060Proto3TypedMessagesLocalImportsAndWireNumbers(t *testing.T) {
	captures := []Capture{
		captured("orders.proto", "protobuf", "v1", `syntax = "proto3";
		 package inventory;
		 import "data/stock.proto";
		 option go_package = "example.invalid/inventory";
		 // Mục hàng
		 message Request { optional string id = 1; repeated .warehouse.Stock parts = 2; }
		 service Inventory { rpc Lookup(Request) returns (.warehouse.Stock); }
		`),
		captured("data/stock.proto", "protobuf", "v1", `syntax="proto3"; package warehouse; message Stock { int64 qty = 12; }`),
	}
	out, err := AnalyzeCaptured(context.Background(), profile, captures)
	if err != nil || len(out.Coverage.Gaps) != 0 || len(out.Batch.Entities) != 4 ||
		len(out.Batch.Links) != 4 || len(out.ProtoFields) != 3 {
		t.Fatalf("proto declarations: %+v %v", out, err)
	}
	if f := out.ProtoFields[2]; f.Name != "parts" || f.Number != 2 || f.Cardinality != "repeated" ||
		string(captures[0].Bytes[f.Declaration.Range.StartByte:f.Declaration.Range.EndByte]) != "repeated .warehouse.Stock parts = 2;" {
		t.Fatalf("wrong field byte span/wire metadata: %+v", f)
	}
	refsAreCaptured(t, out, captures)
}

func TestC060ProtoVersionCycleAndUnsupportedDeclarations(t *testing.T) {
	captures := []Capture{
		captured("a.proto", "protobuf", "v1", `syntax="proto3"; package inv; import "b.proto"; message A{ B b=1; }`),
		captured("b.proto", "protobuf", "v1", `syntax="proto3"; package inv; import "a.proto"; message B{ A a=1; }`),
		captured("c.proto", "protobuf", "v2", `syntax="proto3"; package inv; import "b.proto"; message C{ B b=1; }`),
		captured("unsafe.proto", "protobuf", "v1", `syntax="proto3"; package inv; import "http://proto.invalid/a.proto";`),
		captured("enum.proto", "protobuf", "v1", `syntax="proto3"; package inv; enum Flag { OFF = 0; }`),
		captured("edition.proto", "protobuf", "v1", `edition="2023"; package inv;`),
	}
	out, err := AnalyzeCaptured(context.Background(), profile, captures)
	if err != nil || len(out.Batch.Links) != 0 {
		t.Fatalf("unsupported protobuf linked: %+v, %v", out, err)
	}
	requireGaps(t, out, "CONTRACT_PROTO_TYPE_UNRESOLVED", "CONTRACT_PROTO_IMPORT_CYCLE",
		"CONTRACT_PROTO_IMPORT_VERSION_MISMATCH", "CONTRACT_PROTO_IMPORT_OUTSIDE_CAPTURE",
		"CONTRACT_PROTO_DECLARATION_UNSUPPORTED", "CONTRACT_PROTO_SYNTAX_UNSUPPORTED")
	refsAreCaptured(t, out, captures)
}

func TestC060CaptureIdentityAndBudgets(t *testing.T) {
	doc := captured("schema.json", "json_schema", "v1", "{"+draft+"}")
	tampered := doc
	tampered.Bytes = []byte("{}")
	if _, err := AnalyzeCaptured(context.Background(), profile, []Capture{tampered}); !errors.Is(err, ErrCapture) {
		t.Fatalf("wrong result for tampered bytes: %v", err)
	}
	if _, err := AnalyzeCaptured(context.Background(), profile, []Capture{doc, doc}); !errors.Is(err, ErrCapture) {
		t.Fatalf("duplicate capture accepted: %v", err)
	}
	deep := captured("deep.json", "json_schema", "v1", "{\"a\":"+strings.Repeat("[", maxDepth+2)+"0"+strings.Repeat("]", maxDepth+2)+"}")
	if _, err := AnalyzeCaptured(context.Background(), profile, []Capture{deep}); !errors.Is(err, ErrLimit) {
		t.Fatalf("deep JSON bypassed limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AnalyzeCaptured(ctx, profile, []Capture{doc}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
