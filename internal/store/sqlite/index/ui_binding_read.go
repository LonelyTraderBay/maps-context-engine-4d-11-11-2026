package index

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// ReadUIBinding observes one immutable build binding against an exact
// published member and returns only the source identities needed to recheck it.
func (s *Store) ReadUIBinding(ctx context.Context, request PublishedGraphRequest, build, resolver string) (UIBinding, []model.ManifestSource, error) {
	var binding UIBinding
	if s == nil || ctx == nil || !s.readable() || s.schema != uiAddonSchema || request.AuthorizeGeneration == nil ||
		request.Member.WorktreeID != s.identity.WorktreeID || !model.ValidDigest(build) || !model.ValidDigest(resolver) {
		return binding, nil, ErrInvalidStore
	}
	member := request.Member
	id, err := (model.BindingIdentity{BuildMapID: build, SourceGenerationID: member.GenerationID,
		ProfileID: member.ProfileID, PolicyDigest: member.PolicyDigest, ResolverDigest: resolver}).Digest()
	if err != nil {
		return binding, nil, ErrInvalidStore
	}
	var sources []model.ManifestSource
	err = s.graphTransaction(ctx, request, func(tx *sql.Tx, _ graphHeader) error {
		var stored, evidence string
		err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(binding_id AS BLOB))=64 THEN binding_id END,
 CASE WHEN length(CAST(evidence_digest AS BLOB))=64 THEN evidence_digest END FROM ui_build_bindings
 WHERE build_map_id=? AND profile_id=? AND source_generation_id=? AND policy_digest=? AND resolver_digest=?`,
			build, member.ProfileID, member.GenerationID, member.PolicyDigest, resolver).Scan(&stored, &evidence)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSnapshotUnavailable
		}
		if err != nil {
			return err
		}
		if stored != id || !model.ValidDigest(evidence) {
			return ErrUIConflict
		}
		raw, _, err := readUIBuild(ctx, tx, build)
		if err != nil {
			return err
		}
		manifest, err := model.DecodeBuildManifestJSON(raw, maxUIManifest)
		if err != nil || manifest.BuildMapID != build || manifest.WorktreeID != member.WorktreeID ||
			manifest.ProfileID != member.ProfileID || len(manifest.Sources) > 8192 || model.VerifyBuildMapDigest(manifest, maxUIManifest) != nil {
			return ErrUIConflict
		}
		binding = UIBinding{BindingID: id, BuildMapID: build, SourceGenerationID: member.GenerationID,
			ResolverDigest: resolver, EvidenceDigest: evidence, Anchors: len(manifest.Anchors)}
		sources = manifest.Sources
		return nil
	})
	if err != nil {
		return UIBinding{}, nil, err
	}
	return binding, sources, nil
}
