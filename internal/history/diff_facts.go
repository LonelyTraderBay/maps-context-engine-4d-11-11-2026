package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type semanticFact struct {
	item    DiffItem
	exact   string
	keys    []string
	context string
}

func semanticKey(parts ...any) string {
	raw, _ := json.Marshal(parts)
	return string(raw)
}

func memberIdentity(member model.ScopeMember) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
}

func memberSource(member model.ScopeMember, path, sum string, span model.ByteRange) model.SourceRef {
	return model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
		Path: path, ContentHash: sum, Range: span}
}

func belongs(member model.ScopeMember, ref model.SourceRef) bool {
	return ref.WorktreeID == member.WorktreeID && ref.ProfileID == member.ProfileID && ref.GenerationID == member.GenerationID
}

func hasSource(captured *snapshot.Snapshot, ref model.SourceRef) bool {
	file, ok := captured.File(ref.Path)
	return ok && file.ContentHash() == ref.ContentHash && ref.Range.StartByte >= 0 &&
		ref.Range.EndByte <= int64(file.Len()) && ref.Range.EndByte >= ref.Range.StartByte &&
		file.CheckRange(int(ref.Range.StartByte), int(ref.Range.EndByte)) == nil
}

func (d *diffRun) sourceDigest(captured *snapshot.Snapshot, ref model.SourceRef) (string, error) {
	if !hasSource(captured, ref) {
		return "", ErrDiffInput
	}
	size := ref.Range.EndByte - ref.Range.StartByte
	if size > d.bytes {
		return "", ErrDiffLimit
	}
	d.bytes -= size
	file, _ := captured.File(ref.Path)
	raw, err := file.Range(int(ref.Range.StartByte), int(ref.Range.EndByte))
	if err != nil {
		return "", ErrDiffInput
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (d *diffRun) retainFact(fact semanticFact) error {
	cost := len(fact.item.ID) + len(fact.item.Label) + len(fact.exact) + len(fact.context)
	for _, key := range fact.keys {
		cost += len(key)
	}
	d.bytes -= int64(cost)
	if d.bytes < 0 {
		return ErrDiffLimit
	}
	return nil
}

func (d *diffRun) factsFor(ctx context.Context, view DiffMember) (map[string][]semanticFact, error) {
	out := make(map[string][]semanticFact)
	symbols := make(map[string]semanticFact, len(view.Symbols))
	for _, s := range view.Symbols {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.Path == nil || s.Range == nil {
			d.gap("DIFF_SYMBOL_SOURCE_UNAVAILABLE")
			continue
		}
		file, ok := view.Capture.File(*s.Path)
		if !ok || s.SymbolID == "" || s.QualifiedName == "" || len(s.QualifiedName) > 8192 || len(s.Signature) > 131072 {
			return nil, ErrDiffInput
		}
		if _, dup := symbols[s.SymbolID]; dup {
			return nil, ErrDiffInput
		}
		source := memberSource(view.Member, *s.Path, file.ContentHash(), *s.Range)
		digest, err := d.sourceDigest(view.Capture, source)
		if err != nil {
			return nil, err
		}
		qualified := semanticKey(s.Kind, s.QualifiedName, s.Exported)
		anchor := semanticKey(qualified, *s.Path, s.Range.StartByte)
		signature := semanticKey(qualified, *s.Path, s.Signature)
		fact := semanticFact{
			item: DiffItem{ID: s.SymbolID, Label: s.QualifiedName, Source: source,
				Ref: &model.SymbolRef{Kind: "symbol", GraphIdentity: memberIdentity(view.Member), SymbolID: s.SymbolID}},
			exact:   semanticKey(anchor, s.Signature, digest, s.Range.EndByte),
			keys:    []string{semanticKey("position", anchor), semanticKey("declaration", signature), semanticKey("body", qualified, s.Signature, digest)},
			context: signature,
		}
		if err := d.retainFact(fact); err != nil {
			return nil, err
		}
		symbols[s.SymbolID] = fact
		out["symbol"] = append(out["symbol"], fact)
	}
	relations, err := d.relationFacts(ctx, view, symbols)
	if err != nil {
		return nil, err
	}
	out["relation"] = relations
	out["contract"], err = d.contractFacts(ctx, view)
	if err != nil {
		return nil, err
	}
	for _, facts := range out {
		slices.SortFunc(facts, func(l, r semanticFact) int {
			return strings.Compare(l.item.ID, r.item.ID)
		})
	}
	return out, nil
}

func (d *diffRun) relationFacts(ctx context.Context, view DiffMember, symbols map[string]semanticFact) ([]semanticFact, error) {
	out := make([]semanticFact, 0, len(view.Relations))
	ids := map[string]bool{}
	counts := map[string]int{}
	for _, fact := range symbols {
		counts[fact.context]++
	}
	for _, r := range view.Relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.RelationID == "" || ids[r.RelationID] {
			return nil, ErrDiffInput
		}
		ids[r.RelationID] = true
		from, ok := symbols[r.SourceSymbolID]
		if !ok || r.TargetSymbolID == nil {
			d.gap("DIFF_RELATION_UNRESOLVED")
			continue
		}
		to, ok := symbols[*r.TargetSymbolID]
		if !ok || counts[from.context] != 1 || counts[to.context] != 1 {
			d.gap("DIFF_RELATION_UNRESOLVED")
			continue
		}
		file, ok := view.Capture.File(r.SitePath)
		if !ok {
			return nil, ErrDiffInput
		}
		source := memberSource(view.Member, r.SitePath, file.ContentHash(), r.Range)
		sum, err := d.sourceDigest(view.Capture, source)
		if err != nil {
			return nil, err
		}
		position := semanticKey(r.Kind, from.context, to.context, r.SitePath, r.Range.StartByte)
		fact := semanticFact{item: DiffItem{ID: r.RelationID, Label: r.Kind, Ref: from.item.Ref, Source: source},
			exact: semanticKey(position, r.EvidenceKind, sum, r.Range.EndByte, r.Reason),
			keys:  []string{semanticKey("site", position), semanticKey("occurrence", r.Kind, from.context, to.context, sum)}}
		if err := d.retainFact(fact); err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	return out, nil
}

func (d *diffRun) contractFacts(ctx context.Context, view DiffMember) ([]semanticFact, error) {
	if len(view.Domain.Entities)+len(view.Domain.Fields)+len(view.Domain.Links) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(view.Domain)
	if err != nil {
		return nil, ErrDiffInput
	}
	batch, err := contracts.DecodeBatch(raw)
	if err != nil {
		return nil, ErrDiffInput
	}
	fields := map[string][]string{}
	for _, field := range batch.Fields {
		if field.Entity.GraphIdentity != memberIdentity(view.Member) || !hasSource(view.Capture, field.Declaration) {
			return nil, ErrDiffInput
		}
		fields[field.Entity.EntityID] = append(fields[field.Entity.EntityID], semanticKey(field.FieldPath, field.Direction,
			field.Type, field.Nullable, field.Unit, field.Currency, field.Precision))
	}
	out := make([]semanticFact, 0, len(batch.Entities))
	ids := map[string]bool{}
	for _, entity := range batch.Entities {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entity.Ref.GraphIdentity != memberIdentity(view.Member) || ids[entity.Ref.EntityID] {
			return nil, ErrDiffInput
		}
		ids[entity.Ref.EntityID] = true
		sum, err := d.sourceDigest(view.Capture, entity.Declaration)
		if err != nil {
			return nil, err
		}
		key := semanticKey(entity.Key, entity.Kind, entity.Plane)
		items := fields[entity.Ref.EntityID]
		slices.Sort(items)
		ref := entity.Ref
		fact := semanticFact{item: DiffItem{ID: ref.EntityID, Label: entity.Key.Identifier, Ref: &ref, Source: entity.Declaration},
			exact: semanticKey(key, entity.Declaration.Path, sum, items),
			keys:  []string{key}}
		if err := d.retainFact(fact); err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	for entityID := range fields {
		if !ids[entityID] {
			return nil, ErrDiffInput
		}
	}
	return out, nil
}
