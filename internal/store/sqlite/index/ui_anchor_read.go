package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const uiLinkColumns = `CASE WHEN length(CAST(l.link_id AS BLOB))=64 THEN l.link_id END,
 l.anchor_id,
 CASE WHEN length(CAST(l.kind AS BLOB))<=64 THEN l.kind END,
 CASE WHEN length(CAST(l.resolution_status AS BLOB))<=64 THEN l.resolution_status END,
 CASE WHEN length(CAST(l.target_ref_json AS BLOB))<=8192 THEN l.target_ref_json END,
 CASE WHEN length(CAST(l.evidence_json AS BLOB))<=16384 THEN l.evidence_json END,
 CASE WHEN length(CAST(a.path AS BLOB))<=4096 THEN a.path END,a.source_hash,a.start_byte,a.end_byte`

const uiLinkScope = ` FROM ui_anchor_links l
 JOIN ui_build_bindings b ON b.binding_id=l.binding_id AND b.build_map_id=l.build_map_id
 JOIN ui_source_anchors a ON a.build_map_id=l.build_map_id AND a.anchor_id=l.anchor_id
 WHERE b.binding_id=? AND b.build_map_id=? AND b.profile_id=? AND b.source_generation_id=?
 AND b.policy_digest=? AND b.resolver_digest=?`

func (s *Store) uiBindingArgs(ctx context.Context, r PublishedGraphRequest, binding UIBinding) ([]any, error) {
	m := r.Member
	if ctx == nil || !s.readable() || s.schema != uiAddonSchema || r.AuthorizeGeneration == nil ||
		m.WorktreeID != s.identity.WorktreeID || !model.ValidDigest(binding.BindingID) ||
		!model.ValidDigest(binding.BuildMapID) || !model.ValidDigest(binding.ResolverDigest) ||
		binding.SourceGenerationID != m.GenerationID {
		return nil, ErrInvalidStore
	}
	return []any{binding.BindingID, binding.BuildMapID, m.ProfileID, m.GenerationID, m.PolicyDigest, binding.ResolverDigest}, nil
}

// ReadUIAnchorMatch reads the resolver's immutable evidence for one anchor.
// The caller independently verifies current policy, source and preview state.
func (s *Store) ReadUIAnchorMatch(ctx context.Context, r PublishedGraphRequest, binding UIBinding, anchor string) (*model.MappingMatch, error) {
	args, err := s.uiBindingArgs(ctx, r, binding)
	if err != nil || !model.ValidDigest(anchor) {
		return nil, ErrInvalidStore
	}
	var match *model.MappingMatch
	err = s.graphTransaction(ctx, r, func(tx *sql.Tx, _ graphHeader) error {
		row := tx.QueryRowContext(ctx, "SELECT "+uiLinkColumns+uiLinkScope+" AND l.anchor_id=? AND l.link_id=?", append(args, anchor, anchor)...)
		found, err := scanUIAnchorMatch(row, r.Member, binding)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		match = &found
		return nil
	})
	if err != nil {
		return nil, err
	}
	return match, nil
}

// ReadUITargetMatches uses the immutable link index in CT-05 order. Exact
// target equality fixes target_ref_digest and each V4 anchor owns one link.
// Count and page are read under the same authorized generation transaction.
func (s *Store) ReadUITargetMatches(ctx context.Context, r PublishedGraphRequest, binding UIBinding, target model.GraphRef,
	after string, limit int) ([]model.MappingMatch, int64, error) {
	args, err := s.uiBindingArgs(ctx, r, binding)
	if err != nil || after != "" && !model.ValidDigest(after) || limit < 1 || limit > 100 {
		return nil, 0, ErrInvalidStore
	}
	switch ref := target.(type) {
	case *model.FileRef:
		if ref == nil || ref.ContentHash == nil {
			return nil, 0, ErrInvalidStore
		}
	case *model.SymbolRef:
		if ref == nil {
			return nil, 0, ErrInvalidStore
		}
	default:
		return nil, 0, ErrInvalidStore
	}
	raw, err := json.Marshal(target)
	if err != nil || len(raw) > 8192 {
		return nil, 0, ErrInvalidStore
	}
	args = append(args, string(raw))
	const byTarget = uiLinkScope + " AND l.target_ref_json=?"
	var total int64
	matches := make([]model.MappingMatch, 0, limit)
	err = s.graphTransaction(ctx, r, func(tx *sql.Tx, _ graphHeader) error {
		if err := tx.QueryRowContext(ctx, "SELECT count(*)"+byTarget, args...).Scan(&total); err != nil {
			return err
		}
		if total > 8192 {
			return ErrUIConflict
		}
		rows, err := tx.QueryContext(ctx, "SELECT "+uiLinkColumns+byTarget+" AND l.anchor_id>? ORDER BY l.anchor_id,l.link_id LIMIT ?", append(args, after, limit)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			match, err := scanUIAnchorMatch(rows, r.Member, binding)
			if err != nil {
				return err
			}
			if match.AnchorRef.AnchorID <= after {
				return ErrUIConflict
			}
			after = match.AnchorRef.AnchorID
			matches = append(matches, match)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return matches, total, nil
}

func scanUIAnchorMatch(row interface{ Scan(...any) error }, member model.ScopeMember, binding UIBinding) (model.MappingMatch, error) {
	var matchID, anchor, kind, resolution, path, contentHash string
	var target, evidence []byte
	var start, end int64
	if err := row.Scan(&matchID, &anchor, &kind, &resolution, &target, &evidence, &path, &contentHash, &start, &end); err != nil {
		return model.MappingMatch{}, err
	}
	ref, err := model.DecodeGraphRefJSON(target, 8192)
	if err != nil {
		return model.MappingMatch{}, ErrUIConflict
	}
	proof, err := model.DecodeEvidenceRefJSON(evidence, 16384)
	if err != nil || !model.ValidDigest(anchor) || len(proof.SourceRefs) != 1 || proof.Origin != "build_instrumentation" || proof.Kind != "build_anchored" ||
		proof.EvidenceID != anchor || resolution != "exact_in_supported_scope" || matchID != anchor {
		return model.MappingMatch{}, ErrUIConflict
	}
	id := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	source := proof.SourceRefs[0]
	if source.WorktreeID != id.WorktreeID || source.ProfileID != id.ProfileID || source.GenerationID != id.GenerationID ||
		source.Path != path || source.ContentHash != contentHash || source.Range.StartByte != start || source.Range.EndByte != end {
		return model.MappingMatch{}, ErrUIConflict
	}
	switch value := ref.(type) {
	case *model.FileRef:
		if kind != "source_site" || value.GraphIdentity != id || value.Path != path || value.ContentHash == nil || *value.ContentHash != contentHash {
			return model.MappingMatch{}, ErrUIConflict
		}
	case *model.SymbolRef:
		if kind != "component_definition" || value.GraphIdentity != id {
			return model.MappingMatch{}, ErrUIConflict
		}
	default:
		return model.MappingMatch{}, ErrUIConflict
	}
	return model.MappingMatch{MatchID: matchID, AnchorRef: model.AnchorRef{Kind: "source_anchor", GraphIdentity: id,
		BuildMapID: binding.BuildMapID, AnchorID: anchor, BindingID: binding.BindingID}, TargetRef: ref,
		SourceRefs: proof.SourceRefs, EvidenceRefs: []model.EvidenceRef{proof}, MappingStatus: resolution}, nil
}
