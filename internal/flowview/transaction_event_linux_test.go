//go:build linux

package flowview

import (
	"context"
	"errors"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/events"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const eventFixture = `{"asyncapi":"3.0.0","info":{"title":"Stock","version":"v1"},
 "servers":{"bus":{"host":"broker.invalid","protocol":"amqp","protocolVersion":"0.9.1"}},
 "defaultContentType":"application/json",
 "channels":{"stock":{"address":"stock.changed","messages":{"Changed":{"payload":{"type":"string"}}}}},
 "operations":{
  "publish":{"action":"send","channel":{"$ref":"#/channels/stock"},"x-mce4d-delivery":{"outbox":"orders.outbox"}},
  "consume":{"action":"receive","channel":{"$ref":"#/channels/stock"},"bindings":{"amqp":{"bindingVersion":"0.3.0","ack":true}}}
 }}`

func fixtureFunction(t *testing.T, program *Program, name string) function {
	t.Helper()
	for _, fn := range program.functions {
		if fn.decl.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("typed fixture missing %s", name)
	return function{}
}

func checkTransactionFixture(t *testing.T, ctx context.Context, program *Program) {
	t.Helper()
	t.Run("C101_C102_typed_transaction_and_unknown_outcomes", func(t *testing.T) {
		opt := Options{Steps: 80, Edges: 150, CallDepth: 1}
		fn := fixtureFunction(t, program, "deferredTx")
		view, err := program.Project(ctx, program.symbol(fn.id), opt)
		if err != nil || len(view.SQL) != 3 {
			t.Fatalf("typed transaction: %+v %v", view, err)
		}
		var transactionEdges int
		for _, edge := range view.Data.Edges {
			if edge.Kind == "transaction_link" {
				transactionEdges++
				if len(edge.EvidenceRefs) != 1 || len(edge.EvidenceRefs[0].SourceRefs) != 2 || edge.FromStepID != view.SQL[0].StepID {
					t.Fatalf("transaction caller evidence diverged: %+v", edge)
				}
			}
		}
		if transactionEdges != 2 || view.SQL[0].TransactionGroupID == "" ||
			view.SQL[1].Operation != "ROLLBACK" || view.SQL[2].Operation != "COMMIT" ||
			view.SQL[1].TransactionGroupID != view.SQL[0].TransactionGroupID ||
			view.SQL[2].TransactionGroupID != view.SQL[0].TransactionGroupID {
			t.Fatalf("wrong static group: %+v", view.SQL)
		}
		for _, site := range view.SQL {
			if site.OutcomeStatus != "outcome_unknown" {
				t.Fatal("static transaction inferred persistence")
			}
		}
		for _, step := range view.Data.Steps {
			if step.StepID == view.SQL[1].StepID && step.OrderSemantics != "unknown" {
				t.Fatal("defer became serial execution")
			}
		}

		alias := fixtureFunction(t, program, "aliasTx")
		unresolved, err := program.Project(ctx, program.symbol(alias.id), opt)
		if err != nil || len(unresolved.SQL) != 2 || unresolved.SQL[0].TransactionGroupID != "" ||
			unresolved.SQL[1].TransactionGroupID != "" {
			t.Fatalf("aliased Tx received a group: %+v %v", unresolved.SQL, err)
		}
		fn = fixtureFunction(t, program, "retryBoundary")
		retry, err := program.Project(ctx, program.symbol(fn.id), opt)
		if err != nil || len(retry.SQL) != 0 || len(retry.Data.Steps) != 2 || retry.Data.Steps[1].StepKind != "loop_boundary" ||
			retry.Data.Steps[1].OrderSemantics != "unknown" {
			t.Fatalf("retry loop inferred concrete attempts: %+v %v", retry, err)
		}
	})
}

func checkEventFixture(t *testing.T, ctx context.Context, program *Program) {
	t.Helper()
	t.Run("C104_capture_event_evidence_and_async_boundary", func(t *testing.T) {
		file, ok := program.capture.File("events.json")
		if !ok {
			t.Fatal("missing fixture event contract")
		}
		identity := program.identity
		data := events.Capture{Ref: model.SourceRef{Path: file.Path(), WorktreeID: identity.WorktreeID, ProfileID: identity.ProfileID,
			GenerationID: identity.GenerationID, ContentHash: file.ContentHash(), Range: model.ByteRange{EndByte: int64(file.Len())}},
			Bytes: file.Bytes(), Kind: "asyncapi", Version: "v1"}
		profile := schema.Profile{Namespace: "stock", ServiceID: "stock", Environment: "fixture"}
		declared, err := events.AnalyzeCaptured(ctx, profile, []events.Capture{data}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		producer, consumer := fixtureFunction(t, program, "publishOrder"), fixtureFunction(t, program, "consumeOrder")
		code := []events.CodeCapture{{Ref: producer.file.ref, Bytes: producer.file.raw}}
		proofs := []events.Binding{}
		for _, op := range declared.Operations {
			fn := producer
			if op.Action == "receive" {
				fn = consumer
			}
			proofs = append(proofs, events.Binding{Operation: op.Entity.Ref, Message: op.Messages[0].Entity.Ref,
				Symbol: program.symbol(fn.id), Observation: fn.file.source(fn.decl.Body.List[0]), Broker: op.Broker,
				Address: op.Address, Action: op.Action, Codec: op.Messages[0].Codec, SchemaVersion: op.Messages[0].SchemaVersion})
		}
		bound, err := events.AnalyzeCaptured(ctx, profile, []events.Capture{data}, code, proofs)
		if err != nil || len(bound.Coverage.Gaps) != 0 {
			t.Fatalf("explicit fixture event binding: %+v %v", bound.Coverage.Gaps, err)
		}
		opt := Options{Steps: 50, Edges: 100, CallDepth: 1, Events: []events.Analysis{declared}}
		without, err := program.Project(ctx, program.symbol(producer.id), opt)
		if err != nil || len(without.Events) != 0 {
			t.Fatalf("declaration became code evidence: %+v %v", without.Events, err)
		}
		opt.Events = []events.Analysis{bound}
		send, err := program.Project(ctx, program.symbol(producer.id), opt)
		if err != nil || len(send.Events) != 1 || send.Events[0].Action != "send" ||
			send.Events[0].Delivery.Outbox != "orders.outbox" {
			t.Fatalf("static producer: %+v %v", send.Events, err)
		}
		receive, err := program.Project(ctx, program.symbol(consumer.id), opt)
		if err != nil || len(receive.Events) != 1 || receive.Events[0].Action != "receive" ||
			receive.Events[0].Delivery.Ack == nil || !*receive.Events[0].Delivery.Ack {
			t.Fatalf("static consumer: %+v %v", receive.Events, err)
		}
		for _, view := range []Result{send, receive} {
			at := view.Events[0].StepID
			for _, step := range view.Data.Steps {
				if step.StepID == at && (step.OutcomeStatus != "outcome_unknown" || step.OrderSemantics != "unknown" ||
					len(step.EvidenceRefs) != 1 || step.EvidenceRefs[0].SourceRefs[0] != data.Ref) {
					t.Fatalf("inferred delivery or wrong evidence: %+v", step)
				}
			}
			for _, edge := range view.Data.Edges {
				if edge.ToStepID == at && edge.Kind != "async" || edge.FromStepID == at {
					t.Fatalf("event appended as sync sequence: %+v", edge)
				}
			}
		}
		for i := range bound.Batch.Links {
			link := &bound.Batch.Links[i]
			if link.Kind == "event_producer_binding" {
				link.Evidence.SourceRefs[1].ContentHash = data.Ref.ContentHash
				break
			}
		}
		if _, err := program.Project(ctx, program.symbol(producer.id), opt); !errors.Is(err, ErrInput) {
			t.Fatalf("stale event site emitted: %v", err)
		}
	})
}
