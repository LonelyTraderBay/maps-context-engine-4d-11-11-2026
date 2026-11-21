package events

import (
	"strconv"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func (a *analyzer) bind(owner model.GraphIdentity, code []CodeCapture, bindings []Binding) error {
	captures := make(map[string]CodeCapture)
	total := 0
	for _, item := range code {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		total += len(item.Bytes)
		if total > 4<<20 {
			return schema.ErrLimit
		}
		if _, exists := captures[item.Ref.Path]; exists || scope(item.Ref) != owner {
			return schema.ErrCapture
		}
		if err := schema.ValidateCapture(item.Ref, item.Bytes); err != nil {
			return err
		}
		captures[item.Ref.Path] = item
	}
	operations := make(map[model.EntityRef]Operation)
	type qualified struct{ identifier, version string }
	count := make(map[qualified]int)
	for _, op := range a.out.Operations {
		operations[op.Entity.Ref] = op
		count[qualified{op.Entity.Key.Identifier, op.Entity.Key.Version}]++
	}
	accepted := make(map[string]Binding)
	represented := make(map[model.EntityRef]map[model.EntityRef]bool)
	for _, b := range bindings {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		capture, exists := captures[b.Observation.Path]
		if !exists || scope(b.Observation) != owner || b.Observation.Range.StartByte >= b.Observation.Range.EndByte ||
			b.Observation.ValidateCapturedBytes(capture.Bytes) != nil {
			return schema.ErrCapture
		}
		op, found := operations[b.Operation]
		if !found || !op.Bindable || count[qualified{op.Entity.Key.Identifier, op.Entity.Key.Version}] != 1 ||
			b.Symbol.Kind != "symbol" || b.Symbol.GraphIdentity != owner || b.Symbol.SymbolID == "" ||
			!matches(op, b) {
			if err := a.gap("CONTRACT_EVENT_BINDING_MISMATCH", b.Observation); err != nil {
				return err
			}
			continue
		}
		site := b.Observation
		key := digest(b.Operation.EntityID, b.Message.EntityID, b.Symbol.SymbolID, site.Path, site.ContentHash,
			strconv.FormatInt(site.Range.StartByte, 10), strconv.FormatInt(site.Range.EndByte, 10))
		accepted[key] = b
		if represented[b.Operation] == nil {
			represented[b.Operation] = make(map[model.EntityRef]bool)
		}
		represented[b.Operation][b.Message] = true
	}
	for _, key := range keys(accepted) {
		b := accepted[key]
		kind := "event_producer_binding"
		if b.Action == "receive" {
			kind = "event_consumer_binding"
		}
		op := operations[b.Operation]
		if err := a.link(op.Entity.Ref, &b.Symbol, key, kind, "type_resolved", op.Entity.Declaration, b.Observation); err != nil {
			return err
		}
	}
	for _, op := range a.out.Operations {
		if op.Bindable && len(represented[op.Entity.Ref]) != len(op.Messages) {
			if err := a.gap("CONTRACT_EVENT_CODE_UNVERIFIED", op.Entity.Declaration); err != nil {
				return err
			}
		}
	}
	return nil
}

func matches(op Operation, b Binding) bool {
	if op.Action != b.Action || op.Broker != b.Broker || op.Address != b.Address {
		return false
	}
	for _, message := range op.Messages {
		if message.Entity.Ref == b.Message && message.Codec == b.Codec && message.SchemaVersion == b.SchemaVersion {
			return true
		}
	}
	return false
}
