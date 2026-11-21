package events

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var fixtureProfile = schema.Profile{ServiceID: "inventory", Environment: "fixture", Namespace: "inventory"}

func source(name, contents string) model.SourceRef {
	return model.SourceRef{Path: name, WorktreeID: "01234567-0123-4123-8123-012345678901", ProfileID: strings.Repeat("a", 64),
		GenerationID: "11234567-0123-4123-8123-012345678901", ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(contents))),
		Range: model.ByteRange{EndByte: int64(len(contents))}}
}

func captured(name, kind, contents string) Capture {
	return Capture{Kind: kind, Version: "v1", Ref: source(name, contents), Bytes: []byte(contents)}
}

func asyncDocument(host, address string) string {
	return fmt.Sprintf(`{"asyncapi":"3.0.0","info":{"title":"Synthetic inventory","version":"v1"},
	 "servers":{"broker":{"host":%q,"protocol":"amqp","protocolVersion":"0.9.1","pathname":"/tenant"}},
	 "defaultContentType":"application/json",
	 "channels":{"inventory":{"address":%q,"servers":[{"$ref":"#/servers/broker"}],
	       "messages":{"Changed":{"$ref":"#/components/messages/Changed"}}}},
	 "operations":{
	   "sendInventory":{"action":"send","channel":{"$ref":"#/channels/inventory"}},
	   "receiveInventory":{"action":"receive","channel":{"$ref":"#/channels/inventory"},
	        "messages":[{"$ref":"#/channels/inventory/messages/Changed"}],
	        "bindings":{"amqp":{"bindingVersion":"0.3.0","ack":false}},
	        "x-mce4d-delivery":{"retry_limit":3,"replay":true,"dead_letter":"inventory.failed","deduplication":"event_id","inbox":"applied_events"}}
	 },
	 "components":{"messages":{"Changed":{"x-mce4d-schema-version":"r2","payload":{"$ref":"#/components/schemas/Stock"}}},
	  "schemas":{"Stock":{"type":"object","required":["event_id"],"properties":{"event_id":{"type":"string"},"quantity":{"type":"integer"}}}}}
	}`, host, address)
}

func requireGap(t *testing.T, out Analysis, code string) {
	t.Helper()
	for _, g := range out.Coverage.Gaps {
		if g.Code == code && len(g.SourceRefs) == 1 {
			return
		}
	}
	t.Fatalf("missing %s in %+v", code, out.Coverage.Gaps)
}

func siteBinding(op Operation, code CodeCapture, id string) Binding {
	msg := op.Messages[0]
	return Binding{Operation: op.Entity.Ref, Message: msg.Entity.Ref, Symbol: model.SymbolRef{
		Kind: "symbol", GraphIdentity: op.Entity.Ref.GraphIdentity, SymbolID: id}, Observation: code.Ref,
		Broker: op.Broker, Address: op.Address, Action: op.Action, Codec: msg.Codec, SchemaVersion: msg.SchemaVersion}
}

func TestC061DeclaredDeliveryAndExplicitProducerConsumer(t *testing.T) {
	ctx := context.Background()
	doc := captured("events.json", "asyncapi", asyncDocument("amqp-one.invalid:5672", "inventory.changed"))
	raw := "package fixture\nfunc emit(event string) {}\nfunc consume(event string) {}\n"
	site := CodeCapture{Ref: source("inventory.go", raw), Bytes: []byte(raw)}
	out, err := AnalyzeCaptured(ctx, fixtureProfile, []Capture{doc}, []CodeCapture{site}, nil)
	if err != nil || len(out.Operations) != 2 || len(out.Batch.Entities) != 4 ||
		len(out.Batch.Fields) != 2 || len(out.Batch.Links) != 5 {
		t.Fatalf("wrong captured graph: %+v %v", out, err)
	}
	requireGap(t, out, "CONTRACT_EVENT_CODE_UNVERIFIED")
	for _, op := range out.Operations {
		if op.Entity.Plane != "intended" || !op.Bindable || len(op.Messages) != 1 || op.Messages[0].SchemaVersion != "r2" {
			t.Fatalf("declaration lacks qualification: %+v", op)
		}
		if op.Action == "receive" {
			control := op.Delivery
			if control.Ack == nil || *control.Ack || control.RetryLimit == nil || *control.RetryLimit != 3 ||
				control.Replay == nil || !*control.Replay || control.DeadLetter != "inventory.failed" ||
				control.Deduplication != "event_id" || control.Inbox != "applied_events" ||
				control.Declaration != doc.Ref || control.Pointer != "#/operations/receiveInventory" {
				t.Fatalf("delivery provenance: %+v", control)
			}
		}
	}
	a := siteBinding(out.Operations[0], site, "verified-consumer")
	b := siteBinding(out.Operations[1], site, "verified-producer")
	bound, err := AnalyzeCaptured(ctx, fixtureProfile, []Capture{doc}, []CodeCapture{site}, []Binding{a, b, a})
	if err != nil || len(bound.Batch.Links) != 7 || len(bound.Coverage.Gaps) != 0 {
		t.Fatalf("explicit binding failed: %+v %v", bound, err)
	}
	kinds := make(map[string]bool)
	for _, link := range bound.Batch.Links {
		if link.Evidence.Kind != "type_resolved" {
			continue
		}
		kinds[link.Kind] = len(link.Evidence.SourceRefs) == 2 && link.Evidence.SourceRefs[1] == site.Ref
	}
	if !kinds["event_producer_binding"] || !kinds["event_consumer_binding"] {
		t.Fatalf("typed role evidence missing: %+v", kinds)
	}
}

func TestC061SameTopicDifferentBrokerEnvAndSchemaHaveNoBridge(t *testing.T) {
	ctx := context.Background()
	first := captured("events.json", "asyncapi", asyncDocument("one.invalid", "inventory.changed"))
	second := captured("events.json", "asyncapi", asyncDocument("two.invalid", "inventory.changed"))
	raw := "package fixture\nfunc consume(event string) {}\n"
	site := CodeCapture{Ref: source("inventory.go", raw), Bytes: []byte(raw)}
	original, err := AnalyzeCaptured(ctx, fixtureProfile, []Capture{first}, nil, nil)
	if err != nil || len(original.Operations) == 0 {
		t.Fatalf("missing baseline: %+v %v", original, err)
	}
	originalProof := siteBinding(original.Operations[0], site, "verified-consumer")
	mismatch := originalProof
	mismatch.SchemaVersion = "r1"
	for _, test := range []struct {
		doc     Capture
		profile schema.Profile
		proof   Binding
	}{
		{second, fixtureProfile, originalProof},
		{first, schema.Profile{ServiceID: "inventory", Environment: "elsewhere", Namespace: "inventory"}, originalProof},
		{first, fixtureProfile, mismatch},
	} {
		out, err := AnalyzeCaptured(ctx, test.profile, []Capture{test.doc}, []CodeCapture{site}, []Binding{test.proof})
		if err != nil || len(out.Batch.Links) != 5 {
			t.Fatalf("cross-boundary site must not bind: %+v %v", out, err)
		}
		requireGap(t, out, "CONTRACT_EVENT_BINDING_MISMATCH")
	}
}

func TestC061WildcardUnresolvedReferencesAndVersionStayPartial(t *testing.T) {
	one := asyncDocument("one.invalid", "inventory.*")
	two := strings.ReplaceAll(asyncDocument("one.invalid", "inventory.changed"), "#/components/schemas/Stock", "https://schema.invalid/stock")
	third := strings.Replace(asyncDocument("one.invalid", "inventory.changed"), `"type":"object","required"`,
		`"$ref":"#/components/schemas/Stock","required"`, 1)
	mismatch := captured("mismatch.json", "asyncapi", asyncDocument("one.invalid", "inventory.changed"))
	mismatch.Version = "v2"
	files := []Capture{captured("wild.json", "asyncapi", one), captured("ref.json", "asyncapi", two),
		captured("scope.json", "asyncapi", third), mismatch}
	out, err := AnalyzeCaptured(context.Background(), fixtureProfile, files, nil, nil)
	if err != nil || len(out.Operations) != 4 || out.Coverage.Status != "partial" {
		t.Fatalf("unexpected gap corpus: %+v %v", out, err)
	}
	for _, op := range out.Operations {
		if op.Bindable {
			t.Fatalf("unresolved message set treated as bindable: %+v", op)
		}
	}
	requireGap(t, out, "CONTRACT_EVENT_ADDRESS_UNRESOLVED")
	requireGap(t, out, "CONTRACT_EVENT_CODEC_SCHEMA_UNRESOLVED")
	requireGap(t, out, "CONTRACT_EVENT_MESSAGE_SET_UNRESOLVED")
	requireGap(t, out, "CONTRACT_ASYNCAPI_VERSION_MISMATCH")
}

func TestC061MalformedAckAndReplayDoNotBecomeDeliveryEvidence(t *testing.T) {
	contents := strings.ReplaceAll(asyncDocument("one.invalid", "inventory.changed"), `"ack":false`, `"ack":0`)
	contents = strings.ReplaceAll(contents, `"replay":true`, `"replay":"true"`)
	doc := captured("events.json", "asyncapi", contents)
	out, err := AnalyzeCaptured(context.Background(), fixtureProfile, []Capture{doc}, nil, nil)
	if err != nil || len(out.Operations) != 2 {
		t.Fatalf("invalid delivery suppressed declarations: %+v %v", out, err)
	}
	op := out.Operations[0]
	if op.Bindable || op.Delivery.Ack != nil || op.Delivery.Replay != nil {
		t.Fatalf("invented delivery evidence: %+v", op)
	}
	requireGap(t, out, "CONTRACT_EVENT_DELIVERY_UNRESOLVED")
}

func TestC061RedisCacheNamespaceAndTTLRemainDeclarations(t *testing.T) {
	contents := `{"format":"redis-cache-v1","version":"v1","namespace":"inventory",
	 "backend":{"database":4,"instance":"redis-fixture"},
	 "entries":{
	  "available":{"key":"inventory:available","type":"integer","ttl_seconds":30},
	  "category":{"key":"inventory:category","type":"string"},
	  "elsewhere":{"key":"other:available","type":"integer"},
	  "pattern":{"key":"inventory:*","type":"integer"},
	  "expired":{"key":"inventory:expired","type":"integer","ttl_seconds":0}}}`
	doc := captured("cache.json", "redis_cache", contents)
	out, err := AnalyzeCaptured(context.Background(), fixtureProfile, []Capture{doc}, nil, nil)
	if err != nil || len(out.Cache) != 2 || len(out.Batch.Links) != 0 || len(out.Batch.Fields) != 2 {
		t.Fatalf("cache scope error: %+v %v", out, err)
	}
	first := out.Cache[0]
	if first.Database != 4 || first.BackendID != "redis-fixture" || first.TTLSeconds == nil ||
		*first.TTLSeconds != 30 || out.Cache[1].TTLSeconds != nil || first.Entity.Plane != "intended" {
		t.Fatalf("cache TTL/scope semantics lost: %+v", out.Cache)
	}
	requireGap(t, out, "CONTRACT_CACHE_KEY_OR_TTL_UNRESOLVED")
	doc.Bytes = []byte("{}")
	if _, err := AnalyzeCaptured(context.Background(), fixtureProfile, []Capture{doc}, nil, nil); !errors.Is(err, schema.ErrCapture) {
		t.Fatalf("stale capture accepted: %v", err)
	}
}
