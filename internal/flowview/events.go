package flowview

import (
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/events"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type EventSite struct {
	StepID    string
	Action    string
	Operation model.EntityRef
	Source    model.SourceRef
	Delivery  events.Delivery
}

type eventBinding struct {
	op       events.Operation
	site     model.SourceRef
	evidence model.EvidenceRef
}

// Event analyses come from the owner of T061's verified code bindings.
// Contract declarations alone do not create an event step in a Go function.
func (p *projector) admitEvents() error {
	if len(p.options.Events) > 8 {
		return ErrLimit
	}
	count := 0
	links := make(map[string]bool)
	for _, input := range p.options.Events {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		if err := schema.ValidateBatch(input.Batch); err != nil {
			return err
		}
		ops := make(map[model.EntityRef]events.Operation)
		for _, op := range input.Operations {
			if op.Entity.Ref.GraphIdentity != p.program.identity || ops[op.Entity.Ref].Entity.Kind != "" ||
				(op.Action != "send" && op.Action != "receive") {
				return ErrInput
			}
			ops[op.Entity.Ref] = op
		}
		for _, link := range input.Batch.Links {
			action := ""
			switch link.Kind {
			case "event_producer_binding":
				action = "send"
			case "event_consumer_binding":
				action = "receive"
			default:
				continue
			}
			if links[link.LinkID] {
				continue
			}
			links[link.LinkID] = true
			from, a := link.Source.(*model.EntityRef)
			to, b := link.Target.(*model.SymbolRef)
			ev := link.Evidence
			if !a || !b || from == nil || to == nil || to.GraphIdentity != p.program.identity ||
				ev.Origin != "static_analyzer" || ev.Kind != "type_resolved" || len(ev.SourceRefs) != 2 ||
				link.ResolutionStatus != "exact_in_supported_scope" {
				return ErrInput
			}
			op := ops[*from]
			fn := p.program.functions[to.SymbolID]
			if fn.id == "" || !op.Bindable || op.Action != action || op.Entity.Declaration != ev.SourceRefs[0] ||
				op.Delivery.Declaration != op.Entity.Declaration {
				return ErrInput
			}
			bounds := fn.file.source(fn.decl)
			site := ev.SourceRefs[1]
			if site.Path != bounds.Path || site.ContentHash != bounds.ContentHash ||
				site.Range.StartByte < bounds.Range.StartByte || site.Range.EndByte > bounds.Range.EndByte ||
				site.Range.StartByte >= site.Range.EndByte {
				return ErrInput
			}
			for _, ref := range ev.SourceRefs {
				if err := p.capturedEventSource(ref); err != nil {
					return err
				}
			}
			count++
			if count > 100 {
				return ErrLimit
			}
			p.events[fn.id] = append(p.events[fn.id], eventBinding{op: op, site: site, evidence: ev})
		}
	}
	for _, list := range p.events {
		slices.SortFunc(list, func(a, b eventBinding) int {
			if a.site.Range.StartByte != b.site.Range.StartByte {
				if a.site.Range.StartByte < b.site.Range.StartByte {
					return -1
				}
				return 1
			}
			return strings.Compare(a.evidence.EvidenceID, b.evidence.EvidenceID)
		})
	}
	return nil
}

func (p *projector) capturedEventSource(ref model.SourceRef) error {
	owner := p.program.identity
	if owner.WorktreeID != ref.WorktreeID || owner.ProfileID != ref.ProfileID || owner.GenerationID != ref.GenerationID {
		return ErrInput
	}
	if err := p.program.checkPath(p.ctx, ref.Path); err != nil {
		return err
	}
	file, ok := p.program.capture.File(ref.Path)
	if !ok || ref.ValidateCapturedBytes(file.Bytes()) != nil {
		return ErrInput
	}
	return nil
}

func (p *projector) attachEvents(fn function, entry string) {
	for _, item := range p.events[fn.id] {
		ref := p.program.symbol(fn.id)
		op := item.op.Entity.Ref
		id := p.step("event_"+item.op.Action+"_boundary", "unknown", "outcome_unknown", item.site,
			[]model.GraphRef{&ref, &op}, "type_resolved", "FLOW_EVENT_DELIVERY_UNOBSERVED")
		if id == "" {
			return
		}
		step := &p.out.Data.Steps[len(p.out.Data.Steps)-1]
		step.SourceRefs = append(step.SourceRefs, item.op.Entity.Declaration)
		step.EvidenceRefs[0] = item.evidence
		if item.op.Delivery.Ack != nil {
			p.note(id, "FLOW_EVENT_ACK_DECLARED", item.op.Delivery.Declaration)
		}
		if item.op.Delivery.Outbox != "" {
			p.note(id, "FLOW_EVENT_OUTBOX_DECLARED", item.op.Delivery.Declaration)
		}
		if edge := p.edge(entry, id, "async", nil, ""); edge != nil {
			edge.EvidenceRefs = []model.EvidenceRef{item.evidence}
		}
		p.out.Events = append(p.out.Events, EventSite{StepID: id, Action: item.op.Action,
			Operation: op, Source: item.site, Delivery: item.op.Delivery})
	}
}
