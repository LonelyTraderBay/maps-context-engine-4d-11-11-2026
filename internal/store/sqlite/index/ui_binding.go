package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type UIBindRequest struct {
	Authority           UIWriteAuthority
	BuildMapID          string
	Member              model.ScopeMember
	Resolver            model.ResolverIdentity
	AuthorizeGeneration GenerationAuthorizer
	AuthorizePath       CaptureAuthorizer
	Scanner             *policy.Scanner
	MaxBytes            int64
}

type UIBinding struct {
	BindingID          string
	BuildMapID         string
	SourceGenerationID string
	ResolverDigest     string
	EvidenceDigest     string
	Anchors            int
}

type uiAnchorLink struct {
	AnchorID   string            `json:"anchor_id"`
	Target     model.GraphRef    `json:"target_ref"`
	Kind       string            `json:"kind"`
	Resolution string            `json:"resolution_status"`
	Evidence   model.EvidenceRef `json:"evidence"`
}

// BindBuildMap separately verifies a published generation and writes immutable
// mapping facts. It never rewrites (or publishes) semantic generation rows.
func (s *UIStore) BindBuildMap(ctx context.Context, request UIBindRequest) (UIBinding, error) {
	if !model.ValidDigest(request.BuildMapID) || request.AuthorizeGeneration == nil || request.AuthorizePath == nil ||
		request.Scanner == nil || request.MaxBytes < 1 || request.MaxBytes > 512<<20 {
		return UIBinding{}, ErrInvalidStore
	}
	memberBytes, err := json.Marshal(request.Member)
	if err != nil {
		return UIBinding{}, ErrInvalidStore
	}
	member, err := model.DecodeScopeMemberJSON(memberBytes, 8192)
	if err != nil {
		return UIBinding{}, ErrInvalidStore
	}
	resolver, err := model.ResolverDigest(request.Resolver, 8192)
	if err != nil {
		return UIBinding{}, ErrInvalidStore
	}
	bindingID, err := (model.BindingIdentity{BuildMapID: request.BuildMapID, SourceGenerationID: member.GenerationID,
		ProfileID: member.ProfileID, PolicyDigest: member.PolicyDigest, ResolverDigest: resolver}).Digest()
	if err != nil {
		return UIBinding{}, ErrInvalidStore
	}
	out := UIBinding{BindingID: bindingID, BuildMapID: request.BuildMapID, SourceGenerationID: member.GenerationID, ResolverDigest: resolver}
	_, err = s.transact(ctx, request.Authority, ports.SharedLease, false, func(tx *sql.Tx, _ int, identity Identity) error {
		if member.WorktreeID != identity.WorktreeID || member.IndexIncarnation != identity.Incarnation {
			return ErrStoreChanged
		}
		if err := request.AuthorizeGeneration(ctx, member.WorktreeID, member.ProfileID, member.PolicyDigest, member.PolicyEpoch); err != nil {
			return err
		}
		raw, artifact, err := readUIBuild(ctx, tx, request.BuildMapID)
		if err != nil {
			return err
		}
		build, err := model.DecodeBuildManifestJSON(raw, maxUIManifest)
		if err != nil || len(build.Anchors) > 8192 || build.BuildMapID != request.BuildMapID || build.ProfileID != member.ProfileID ||
			build.WorktreeID != member.WorktreeID {
			return ErrCaptureMismatch
		}
		if err := verifyUIAnchors(ctx, tx, build); err != nil {
			return err
		}
		files, err := loadUIBindingFiles(ctx, tx, request, member, build)
		if err != nil {
			return err
		}
		if err := model.VerifyBuildManifestCaptured(build, files, maxUIManifest, request.MaxBytes); err != nil {
			return ErrCaptureMismatch
		}
		links, err := deriveUILinks(ctx, tx, build, member, artifact)
		if err != nil {
			return err
		}
		out.Anchors = len(links)
		data, err := json.Marshal(links)
		if err != nil || len(data) > maxUIManifest {
			return ErrCaptureLimit
		}
		value, err := model.DecodeIdentityJSON(data, maxUIManifest)
		if err != nil {
			return ErrCaptureMismatch
		}
		_, out.EvidenceDigest, err = model.EncodeCanonical("ui-binding-evidence-v1", value, maxUIManifest)
		if err != nil {
			return err
		}
		if err := persistUIBinding(ctx, tx, out, links, member); err != nil {
			return err
		}
		for _, file := range build.Sources {
			if err := request.AuthorizePath(ctx, member.WorktreeID, file.Path, member.PolicyDigest, member.PolicyEpoch); err != nil {
				return err
			}
		}
		return request.AuthorizeGeneration(ctx, member.WorktreeID, member.ProfileID, member.PolicyDigest, member.PolicyEpoch)
	})
	if err != nil {
		return UIBinding{}, err
	}
	return out, nil
}

func loadUIBindingFiles(ctx context.Context, tx *sql.Tx, request UIBindRequest, member model.ScopeMember, build model.BuildManifest) (map[string][]byte, error) {
	var raw []byte
	var policyDigest, snapshotDigest, provider string
	var policyEpoch int64
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(manifest_json AS BLOB))<=? THEN manifest_json END,
		policy_digest,snapshot_digest,provider_fingerprint,policy_epoch FROM generations
		WHERE generation_id=? AND profile_id=? AND state IN ('READY','READY_PARTIAL')`, maxUIManifest, member.GenerationID, member.ProfileID).
		Scan(&raw, &policyDigest, &snapshotDigest, &provider, &policyEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSnapshotUnavailable
	}
	if err != nil {
		return nil, err
	}
	if policyDigest != member.PolicyDigest || policyEpoch != member.PolicyEpoch || snapshotDigest != member.SnapshotDigest {
		return nil, ErrCaptureMismatch
	}
	capture, err := model.DecodeCaptureManifestJSON(raw, maxUIManifest)
	if err != nil || model.VerifyCaptureDigest(capture, maxUIManifest) != nil || capture.SnapshotDigest != snapshotDigest ||
		capture.PolicyDigest != policyDigest || capture.ProviderDigest != provider || capture.ProfileID != member.ProfileID || capture.WorktreeID != member.WorktreeID {
		return nil, ErrCaptureMismatch
	}
	manifestFiles := make(map[string]model.ManifestFile, len(capture.Files))
	for _, file := range capture.Files {
		manifestFiles[file.Path] = file
	}
	files := make(map[string][]byte, len(build.Sources))
	remaining := request.MaxBytes
	for _, file := range build.Sources {
		if err := request.AuthorizePath(ctx, member.WorktreeID, file.Path, member.PolicyDigest, member.PolicyEpoch); err != nil {
			return nil, err
		}
		expected := manifestFiles[file.Path]
		if expected.Status != "captured" || expected.ContentHash == nil || expected.ByteLength == nil || *expected.ContentHash != file.ContentHash {
			return nil, ErrCaptureMismatch
		}
		size := *expected.ByteLength
		if size < 0 || size > remaining {
			return nil, ErrCaptureLimit
		}
		var bytes []byte
		var actualHash, status string
		err := tx.QueryRowContext(ctx, `SELECT CASE WHEN f.byte_length=? AND length(b.bytes)=? THEN b.bytes END,f.content_hash,f.status
			FROM files f JOIN content_blobs b ON b.content_hash=f.content_hash WHERE f.generation_id=? AND f.path=?`,
			size, size, member.GenerationID, file.Path).Scan(&bytes, &actualHash, &status)
		if err != nil {
			return nil, err
		}
		if int64(len(bytes)) != size || actualHash != file.ContentHash || status != "indexed" && status != "parse_error" {
			return nil, ErrCaptureMismatch
		}
		if err := request.Scanner.CheckContent(bytes); err != nil {
			return nil, err
		}
		files[file.Path] = bytes
		remaining -= size
	}
	return files, nil
}

func deriveUILinks(ctx context.Context, tx *sql.Tx, build model.BuildManifest, member model.ScopeMember, artifact string) ([]uiAnchorLink, error) {
	identity := model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}
	links := make([]uiAnchorLink, 0, len(build.Anchors))
	for _, anchor := range build.Anchors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var target model.GraphRef = &model.FileRef{Kind: "file", GraphIdentity: identity, Path: anchor.Path, ContentHash: &anchor.ContentHash}
		kind := "source_site"
		if anchor.Kind == "component_definition" {
			ids, err := uiSymbolCandidates(ctx, tx, member.GenerationID, anchor)
			if err != nil {
				return nil, err
			}
			if len(ids) == 1 {
				target = &model.SymbolRef{Kind: "symbol", GraphIdentity: identity, SymbolID: ids[0]}
				kind = "component_definition"
			}
		}
		if _, err := model.GraphRefDigest(target, 8192); err != nil {
			return nil, ErrCaptureMismatch
		}
		source := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
			Path: anchor.Path, ContentHash: anchor.ContentHash, Range: anchor.Range}
		evidence := model.EvidenceRef{EvidenceID: anchor.AnchorID, Origin: "build_instrumentation", Kind: "build_anchored",
			SourceRefs: []model.SourceRef{source}, ArtifactID: &artifact}
		links = append(links, uiAnchorLink{AnchorID: anchor.AnchorID, Target: target, Kind: kind,
			Resolution: "exact_in_supported_scope", Evidence: evidence})
	}
	sort.Slice(links, func(i, j int) bool { return links[i].AnchorID < links[j].AnchorID })
	return links, nil
}

func uiSymbolCandidates(ctx context.Context, tx *sql.Tx, generation string, anchor model.AnchorEntry) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(symbol_id AS BLOB))<=768 THEN symbol_id END
		FROM symbols WHERE generation_id=? AND path=? AND start_byte=? AND end_byte=? AND kind IN ('function','variable','method')
		ORDER BY symbol_id LIMIT 2`, generation, anchor.Path, anchor.Range.StartByte, anchor.Range.EndByte)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func persistUIBinding(ctx context.Context, tx *sql.Tx, binding UIBinding, links []uiAnchorLink, member model.ScopeMember) error {
	var storedID, storedEvidence string
	err := tx.QueryRowContext(ctx, `SELECT binding_id,evidence_digest FROM ui_build_bindings
		WHERE build_map_id=? AND profile_id=? AND source_generation_id=? AND policy_digest=? AND resolver_digest=?`,
		binding.BuildMapID, member.ProfileID, binding.SourceGenerationID, member.PolicyDigest, binding.ResolverDigest).
		Scan(&storedID, &storedEvidence)
	existing := err == nil
	if existing && (storedID != binding.BindingID || storedEvidence != binding.EvidenceDigest) {
		return ErrUIConflict
	}
	if !existing && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !existing {
		if _, err := tx.ExecContext(ctx, `INSERT INTO ui_build_bindings(binding_id,build_map_id,profile_id,source_generation_id,
			policy_digest,resolver_digest,evidence_digest,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			binding.BindingID, binding.BuildMapID, member.ProfileID, binding.SourceGenerationID, member.PolicyDigest,
			binding.ResolverDigest, binding.EvidenceDigest, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	for _, link := range links {
		target, err := json.Marshal(link.Target)
		if err != nil {
			return err
		}
		evidence, err := json.Marshal(link.Evidence)
		if err != nil {
			return err
		}
		if existing {
			var match int
			err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ui_anchor_links WHERE binding_id=? AND link_id=? AND anchor_id=?
				AND build_map_id=? AND kind=? AND resolution_status=? AND target_ref_json=? AND evidence_json=?`,
				binding.BindingID, link.AnchorID, link.AnchorID, binding.BuildMapID, link.Kind, link.Resolution, string(target), string(evidence)).Scan(&match)
			if err != nil || match != 1 {
				return errors.Join(ErrUIConflict, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ui_anchor_links(binding_id,build_map_id,link_id,anchor_id,target_ref_json,
			kind,resolution_status,evidence_json) VALUES(?,?,?,?,?,?,?,?)`, binding.BindingID, binding.BuildMapID, link.AnchorID,
			link.AnchorID, string(target), link.Kind, link.Resolution, string(evidence)); err != nil {
			return err
		}
	}
	if existing {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM ui_anchor_links WHERE binding_id=?`, binding.BindingID).Scan(&count); err != nil || count != len(links) {
			return errors.Join(ErrUIConflict, err)
		}
	}
	return nil
}
