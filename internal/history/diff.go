package history

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const diffScopeBytes = 256 << 10

var (
	ErrDiffInput           = errors.New("invalid semantic snapshot comparison")
	ErrDiffSnapshotMissing = errors.New("requested retained comparison snapshot is unavailable")
	ErrDiffScopeChanged    = errors.New("snapshot comparison scope changed")
	ErrDiffLimit           = errors.New("semantic snapshot comparison exceeds its budget")
)

type DiffRequest struct {
	Left, Right model.ScopeVector
	MaxFacts    int
	MaxChanges  int
}

// DiffMember holds closed facts from one authorized published generation.
// Complete permits reporting absent rows as additions and removals.
type DiffMember struct {
	Member    model.ScopeMember
	Capture   *snapshot.Snapshot
	Symbols   []model.SymbolFact
	Relations []model.RelationFact
	Domain    contracts.Batch
	Decisions []model.RecordSummary
	Complete  bool
}

type DiffItem struct {
	ID     string
	Label  string
	Ref    model.GraphRef
	Source model.SourceRef
}

type SemanticChange struct {
	ServiceID *string
	Category  string
	Change    string
	Match     string
	Before    *DiffItem
	After     *DiffItem
}

type ProviderDelta struct {
	ServiceID     *string
	WorktreeID    string
	LeftProfile   string
	RightProfile  string
	LeftProvider  string
	RightProvider string
}

type DecisionDrift struct {
	RecordID      string
	Version       int64
	Status        string
	Before        model.SourceRef
	After         *model.SourceRef
	AfterVersion  *int64
	AfterApproval *string
}

type KnownConsumer struct {
	Side      string
	ServiceID *string
	Ref       model.GraphRef
	Source    model.SourceRef
}

type SnapshotDiff struct {
	Left, Right    model.ScopeVector
	Changes        []SemanticChange
	ProviderDeltas []ProviderDelta
	Decisions      []DecisionDrift
	Consumers      []KnownConsumer
	Gaps           []model.Gap
	Partial        bool
	ExactMatches   int
}

type DiffResolver struct {
	// Observe resolves the exact retained vector, never the active heads.
	Observe    func(context.Context, model.ScopeVector) (model.ScopeVector, error)
	ReadMember func(context.Context, model.ScopeVector, model.ScopeMember, int) (DiffMember, error)
}

type diffRun struct {
	result  SnapshotDiff
	facts   int
	changes int
	bytes   int64
}

func (r DiffResolver) Compare(ctx context.Context, req DiffRequest) (SnapshotDiff, error) {
	if ctx == nil || r.Observe == nil || r.ReadMember == nil || req.MaxFacts < 1 || req.MaxFacts > 100000 ||
		req.MaxChanges < 1 || req.MaxChanges > 5000 || model.VerifyScopeVectorDigest(req.Left, diffScopeBytes) != nil ||
		model.VerifyScopeVectorDigest(req.Right, diffScopeBytes) != nil {
		return SnapshotDiff{}, ErrDiffInput
	}
	left, err := model.NormalizeScopeVector(req.Left, diffScopeBytes)
	if err != nil {
		return SnapshotDiff{}, ErrDiffInput
	}
	right, err := model.NormalizeScopeVector(req.Right, diffScopeBytes)
	if err != nil || left.Kind != right.Kind || !reflect.DeepEqual(left.GroupID, right.GroupID) {
		return SnapshotDiff{}, ErrDiffInput
	}
	if err := r.recheck(ctx, left, right); err != nil {
		return SnapshotDiff{}, err
	}
	d := diffRun{result: SnapshotDiff{Left: left, Right: right, Changes: []SemanticChange{}, ProviderDeltas: []ProviderDelta{},
		Decisions: []DecisionDrift{}, Consumers: []KnownConsumer{}, Gaps: []model.Gap{}},
		facts: req.MaxFacts, changes: req.MaxChanges, bytes: 64 << 20}
	if len(left.UnavailableMembers)+len(right.UnavailableMembers) != 0 {
		d.gap("DIFF_MEMBER_UNAVAILABLE")
	}
	after := map[string]model.ScopeMember{}
	for _, member := range right.Members {
		after[diffMemberKey(member)] = member
	}
	for _, member := range left.Members {
		key := diffMemberKey(member)
		other, ok := after[key]
		if !ok || member.WorktreeID != other.WorktreeID {
			d.gap("DIFF_MEMBER_UNAVAILABLE")
			continue
		}
		delete(after, key)
		old, err := r.read(ctx, left, member, &d.facts)
		if err != nil {
			return SnapshotDiff{}, err
		}
		newer, err := r.read(ctx, right, other, &d.facts)
		if err != nil {
			return SnapshotDiff{}, err
		}
		if err := d.compareMember(ctx, old, newer); err != nil {
			return SnapshotDiff{}, err
		}
	}
	if len(after) != 0 {
		d.gap("DIFF_MEMBER_UNAVAILABLE")
	}
	if err := r.recheck(ctx, left, right); err != nil {
		return SnapshotDiff{}, err
	}
	return d.result, nil
}

func diffMemberKey(member model.ScopeMember) string {
	if member.ServiceID != nil {
		return *member.ServiceID
	}
	return member.WorktreeID
}

func (r DiffResolver) recheck(ctx context.Context, vectors ...model.ScopeVector) error {
	for _, pinned := range vectors {
		if err := ctx.Err(); err != nil {
			return err
		}
		seen, err := r.Observe(ctx, pinned)
		if err != nil {
			return err
		}
		if model.ComparePinnedScope(pinned, seen, diffScopeBytes) != nil {
			return ErrDiffScopeChanged
		}
	}
	return ctx.Err()
}

func (r DiffResolver) read(ctx context.Context, scope model.ScopeVector, wanted model.ScopeMember, budget *int) (DiffMember, error) {
	view, err := r.ReadMember(ctx, scope, wanted, *budget)
	if err != nil {
		return DiffMember{}, err
	}
	if view.Capture == nil {
		return DiffMember{}, ErrDiffSnapshotMissing
	}
	manifest := view.Capture.Manifest()
	if !reflect.DeepEqual(wanted, view.Member) || manifest.WorktreeID != wanted.WorktreeID || manifest.ProfileID != wanted.ProfileID ||
		manifest.SnapshotDigest != wanted.SnapshotDigest || manifest.PolicyDigest != wanted.PolicyDigest ||
		!model.ValidDigest(manifest.ProviderDigest) || model.VerifyCaptureDigest(manifest, 64<<20) != nil {
		return DiffMember{}, ErrDiffScopeChanged
	}
	size := len(view.Symbols) + len(view.Relations) + len(view.Domain.Entities) + len(view.Domain.Fields) +
		len(view.Domain.Links) + len(view.Decisions)
	if size > *budget {
		return DiffMember{}, ErrDiffLimit
	}
	*budget -= size
	return view, nil
}

func (d *diffRun) compareMember(ctx context.Context, left, right DiffMember) error {
	if err := d.compareDecisions(ctx, left, right); err != nil {
		return err
	}
	before, after := left.Capture.Manifest().ProviderDigest, right.Capture.Manifest().ProviderDigest
	if left.Member.ProfileID != right.Member.ProfileID || before != after {
		if err := d.takeChange(); err != nil {
			return err
		}
		d.result.ProviderDeltas = append(d.result.ProviderDeltas, ProviderDelta{ServiceID: left.Member.ServiceID, WorktreeID: left.Member.WorktreeID,
			LeftProfile: left.Member.ProfileID, RightProfile: right.Member.ProfileID, LeftProvider: before, RightProvider: after})
		d.gap("DIFF_PROVIDER_CHANGED")
		return nil
	}
	l, err := d.factsFor(ctx, left)
	if err != nil {
		return err
	}
	r, err := d.factsFor(ctx, right)
	if err != nil {
		return err
	}
	if !left.Complete || !right.Complete {
		d.gap("DIFF_COVERAGE_PARTIAL")
	}
	changedLeft, changedRight := map[string]bool{}, map[string]bool{}
	for _, category := range []string{"symbol", "relation", "contract"} {
		marked, err := d.compareFacts(ctx, category, left.Member.ServiceID, l[category], r[category], left.Complete && right.Complete)
		if err != nil {
			return err
		}
		for _, change := range marked {
			if change.Before != nil {
				changedLeft[semanticKey(category, change.Before.ID)] = true
			}
			if change.After != nil {
				changedRight[semanticKey(category, change.After.ID)] = true
			}
		}
	}
	if err := d.collectConsumers(ctx, "left", left, l, changedLeft); err != nil {
		return err
	}
	return d.collectConsumers(ctx, "right", right, r, changedRight)
}

func (d *diffRun) compareDecisions(ctx context.Context, left, right DiffMember) error {
	records := slices.Clone(left.Decisions)
	slices.SortFunc(records, func(a, b model.RecordSummary) int { return strings.Compare(a.RecordID, b.RecordID) })
	seen := map[string]bool{}
	current := make(map[string]model.RecordSummary, len(right.Decisions))
	for _, record := range right.Decisions {
		if !model.ValidUUID(record.RecordID) || record.Version < 1 {
			return ErrDiffInput
		}
		if _, duplicate := current[record.RecordID]; duplicate {
			return ErrDiffInput
		}
		current[record.RecordID] = record
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.EffectiveStatus != "approved" || record.DeclaredStatus != "approved" || record.ProvenanceStatus != "retained_snapshot" {
			continue
		}
		if !model.ValidUUID(record.RecordID) || record.Version < 1 || len(record.SourceRefs) == 0 || len(record.SourceRefs) > 128 || seen[record.RecordID] {
			return ErrDiffInput
		}
		seen[record.RecordID] = true
		newer, known := current[record.RecordID]
		approvalChanged := known && (newer.Version != record.Version || newer.EffectiveStatus != "approved")
		for _, ref := range record.SourceRefs {
			if !belongs(left.Member, ref) || !hasSource(left.Capture, ref) {
				d.gap("DIFF_DECISION_SOURCE_UNAVAILABLE")
				continue
			}
			file, ok := right.Capture.File(ref.Path)
			if ok && file.ContentHash() == ref.ContentHash && !approvalChanged {
				continue
			}
			item := DecisionDrift{RecordID: record.RecordID, Version: record.Version, Before: ref, Status: "source_unavailable"}
			if ok {
				item.Status = "needs_review"
				if approvalChanged && file.ContentHash() == ref.ContentHash {
					item.Status = "approval_changed"
				}
				newRef := memberSource(right.Member, ref.Path, file.ContentHash(), model.ByteRange{EndByte: int64(file.Len())})
				item.After = &newRef
			} else {
				d.gap("DIFF_DECISION_SOURCE_UNAVAILABLE")
			}
			if known {
				item.AfterVersion, item.AfterApproval = &newer.Version, &newer.EffectiveStatus
			}
			if err := d.takeChange(); err != nil {
				return err
			}
			d.result.Decisions = append(d.result.Decisions, item)
		}
	}
	return nil
}

func (d *diffRun) takeChange() error {
	if d.changes <= 0 {
		return ErrDiffLimit
	}
	d.changes--
	return nil
}

func (d *diffRun) gap(code string) {
	for _, prior := range d.result.Gaps {
		if prior.Code == code {
			return
		}
	}
	d.result.Partial = true
	d.result.Gaps = append(d.result.Gaps, model.Gap{Code: code,
		Reason: "The difference is limited to facts with verified retained evidence.", SourceRefs: []model.SourceRef{}})
}
