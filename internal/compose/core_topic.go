package compose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

type topicMaterial struct {
	index      *overview.AliasIndex
	query      string
	configPath string
	configHash string
	result     overview.AliasResult
	digest     string
}

func topicResultDigest(configPath, configHash, query string, result overview.AliasResult) (string, error) {
	raw, err := json.Marshal(struct {
		ConfigPath string               `json:"config_path"`
		ConfigHash string               `json:"config_hash"`
		Query      string               `json:"query"`
		Result     overview.AliasResult `json:"result"`
	}{configPath, configHash, query, result})
	const limit = packFactLimit + 32768
	if err != nil || len(raw) > limit {
		return "", app.ErrCoreLimit
	}
	value, err := model.DecodeIdentityJSON(raw, limit)
	if err != nil {
		return "", app.ErrCoreBackend
	}
	_, digest, err := model.EncodeCanonical("topic-result-v1", value, limit*2)
	if err != nil {
		return "", app.ErrCoreLimit
	}
	return digest, nil
}

func (m *topicMaterial) recheck(ctx context.Context) error {
	if m == nil || m.index == nil {
		return app.ErrCoreInput
	}
	result, err := m.index.LookupAll(ctx, m.query)
	if err != nil {
		return coreStoreError(err)
	}
	digest, err := topicResultDigest(m.configPath, m.configHash, m.query, result)
	if err != nil || digest != m.digest {
		return contextpack.ErrCursorInvalid
	}
	return ctx.Err()
}

// loadTopic requires the caller to hold buildGate. The owner supplies the
// config path and rechecks approval; source bytes are read only from the pinned
// snapshot through the existing overview and alias-config adapters.
func (b *CoreBackend) loadTopic(ctx context.Context, principal app.CorePrincipal, scope model.ScopeVector, query string) (*topicMaterial, error) {
	if b.topicSource == nil {
		return nil, app.ErrCoreUnsupported
	}
	observed, err := b.Reader.Recheck(ctx, principal, scope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	member := observed.Vector.Members[0]
	rawScope, err := json.Marshal(observed.Vector)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	copyScope, err := model.DecodeScopeVectorJSON(rawScope, 32768)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	config, err := b.topicSource(ctx, principal, copyScope)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if config.Path == "" || len(config.Path) > 16384 || config.Recheck == nil || config.AuthorizeConfig == nil || config.AuthorizeAlias == nil {
		return nil, app.ErrCoreInput
	}
	if err := config.Recheck(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, app.ErrValidationAuthority
	}
	maxFile := minInt64(observed.Binding.Policy.MaxFileBytes, b.limits.MaxFileBytes, packSourceLimit)
	maxSource := minInt64(observed.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot, packSourceLimit)
	if maxFile < 1 || maxSource < 1 {
		return nil, app.ErrCoreLimit
	}
	scanner, err := policy.NewScanner(int(maxInt64(maxFile, int64(b.Reader.MaxManifestBytes), packFactLimit)))
	if err != nil {
		return nil, err
	}
	if err := scanner.CheckContent([]byte(query)); err != nil {
		return nil, err
	}
	if err := scanner.CheckContent([]byte(config.Path)); err != nil {
		return nil, err
	}
	authorizeGeneration := func(ctx context.Context, worktree, profile, digest string, epoch int64) error {
		if worktree != member.WorktreeID || profile != member.ProfileID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		return b.Authority.samePrincipal(ctx, principal)
	}
	authorizePath := func(ctx context.Context, worktree, name, digest string, epoch int64) error {
		if worktree != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		if err := b.Authority.samePrincipal(ctx, principal); err != nil {
			return err
		}
		return b.Authority.path(ctx, principal, observed.Binding.Policy, name)
	}
	graph, err := b.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: observed.Vector.VectorDigest, AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000})
	if err != nil {
		return nil, coreStoreError(err)
	}
	report, err := b.buildOverview(ctx, principal, observed.Vector, graph, scanner, maxSource)
	if err != nil {
		return nil, coreStoreError(err)
	}
	expected := overview.Scope{Identity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SnapshotDigest: member.SnapshotDigest, PolicyDigest: member.PolicyDigest, PolicyEpoch: member.PolicyEpoch}
	check := func(ctx context.Context, value overview.Scope) error {
		if value != expected || config.Recheck(ctx) != nil {
			return ErrAuthority
		}
		_, err := b.Reader.Recheck(ctx, principal, observed.Vector)
		return err
	}
	aliases, err := overview.BuildAliasIndexFromConfig(ctx, overview.AliasConfigInput{Overview: report, Snapshot: graph.Snapshot, Policy: observed.Binding.Policy, ConfigPath: config.Path, Scanner: scanner, MaxBytes: packFactLimit,
		AuthorizeConfig: func(ctx context.Context, value overview.Scope, name string) error {
			if name != config.Path || check(ctx, value) != nil {
				return ErrAuthority
			}
			if err := b.Authority.path(ctx, principal, observed.Binding.Policy, name); err != nil {
				return err
			}
			if err := config.AuthorizeConfig(ctx, value, name); err != nil {
				return ErrAuthority
			}
			return check(ctx, value)
		},
		AuthorizeAlias: func(ctx context.Context, value overview.Scope, declaration overview.AliasDeclaration) error {
			if err := check(ctx, value); err != nil {
				return err
			}
			if err := b.Authority.path(ctx, principal, observed.Binding.Policy, declaration.Source.Path); err != nil {
				return err
			}
			if err := config.AuthorizeAlias(ctx, value, declaration); err != nil {
				return ErrAuthority
			}
			return check(ctx, value)
		},
	})
	if err != nil {
		return nil, coreStoreError(err)
	}
	file, ok := graph.Snapshot.File(config.Path)
	if !ok {
		return nil, app.ErrCoreBackend
	}
	result, err := aliases.LookupAll(ctx, query)
	if err != nil {
		return nil, coreStoreError(err)
	}
	digest, err := topicResultDigest(config.Path, file.ContentHash(), query, result)
	if err != nil {
		return nil, err
	}
	return &topicMaterial{index: aliases, query: query, configPath: config.Path, configHash: file.ContentHash(), result: result, digest: digest}, nil
}

func (b *CoreBackend) locateTopic(ctx context.Context, principal app.CorePrincipal, request model.LocateRequest, options app.CoreCallOptions, observation ScopeObservation, binding contextpack.CursorBinding, guard contextpack.CursorGuard, base search.Request) ([][]byte, error) {
	if b.topicSource == nil || b.buildGate == nil || request.Query == nil || request.Position != nil || len(request.Kinds) != 0 || request.Package != nil {
		return nil, app.ErrCoreUnsupported
	}
	if strings.TrimSpace(*request.Query) == "" {
		return nil, app.ErrCoreInput
	}
	for _, r := range *request.Query {
		if unicode.IsControl(r) {
			return nil, app.ErrCoreInput
		}
	}
	if err := base.Scanner.CheckContent([]byte(*request.Query)); err != nil {
		return nil, err
	}
	select {
	case b.buildGate <- struct{}{}:
		defer func() { <-b.buildGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	material, err := b.loadTopic(ctx, principal, observation.Vector, *request.Query)
	if err != nil {
		return nil, coreStoreError(err)
	}
	blocks, err := topicLocatePage(ctx, b.Cursor, binding, guard, principal.ID, request, options, material)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if err := material.recheck(ctx); err != nil {
		return nil, err
	}
	return blocks, nil
}

func newTopicResultID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func topicKey(id string) ([]string, error) {
	if id == "" || len(id) > 768 {
		return nil, app.ErrCoreInput
	}
	encoded := hex.EncodeToString([]byte(id))
	parts := []string{}
	for len(encoded) > 512 {
		parts = append(parts, encoded[:512])
		encoded = encoded[512:]
	}
	parts = append(parts, encoded)
	return parts, nil
}

func topicPosition(position contextpack.SignedPosition, result overview.AliasResult, continuation bool) (int, error) {
	if position.NextCoverageGap != 0 || position.NextWarningGap != 0 || position.NextByteOffset != 0 || position.NextItem < 0 || position.NextItem > int64(len(result.Candidates)) {
		return 0, contextpack.ErrCursorInvalid
	}
	start := int(position.NextItem)
	if !continuation {
		if start != 0 || len(position.LastKey) != 0 {
			return 0, contextpack.ErrCursorInvalid
		}
		return 0, nil
	}
	if start == 0 || start >= len(result.Candidates) {
		return 0, contextpack.ErrCursorInvalid
	}
	key, err := topicKey(result.Candidates[start-1].Entity.EntityID)
	if err != nil || !reflect.DeepEqual(key, position.LastKey) {
		return 0, contextpack.ErrCursorInvalid
	}
	return start, nil
}

func topicCandidate(query string, value overview.AliasCandidate) model.Candidate {
	ref := value.Entity
	return model.Candidate{Ref: &ref, Name: query, QualifiedName: ref.EntityID, SourceRefs: append([]model.SourceRef{}, value.SourceRefs...), EvidenceRefs: []model.EvidenceRef{}}
}

func topicLocatePage(ctx context.Context, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard, principalID string, request model.LocateRequest, options app.CoreCallOptions, material *topicMaterial) ([][]byte, error) {
	if ctx == nil || codec == nil || guard == nil || material == nil || request.Mode != "topic" || request.Query == nil || *request.Query != material.query || request.Position != nil || len(request.Kinds) != 0 || request.Package != nil || request.Limit < 1 || request.Limit > 100 || binding.Purpose != "list" || binding.OrderVersion != "locate-id-v1" || model.VerifyScopeVectorDigest(binding.Scope, 32768) != nil || binding.Scope.Kind != "single" || len(binding.Scope.Members) != 1 || len(binding.Scope.UnavailableMembers) != 0 {
		return nil, app.ErrCoreInput
	}
	member := binding.Scope.Members[0]
	if requested := request.Scope; requested != nil && (requested.GroupID != nil || requested.WorktreeID != nil && *requested.WorktreeID != member.WorktreeID || requested.ProfileID != nil && *requested.ProfileID != member.ProfileID || requested.GenerationID != nil && *requested.GenerationID != member.GenerationID || requested.PinnedVector != nil && model.ComparePinnedScope(*requested.PinnedVector, binding.Scope, 32768) != nil) {
		return nil, contextpack.ErrCursorInvalid
	}
	continuation, limit := request.Cursor, int(request.Limit)
	request.Cursor, request.Limit = nil, 20
	digest, err := model.RequestDigest("locate", principalID, "LocateRequest", request, 32768, 4)
	if err != nil {
		return nil, app.ErrCoreInput
	}
	if binding.RequestDigest != "" && binding.RequestDigest != digest || binding.ResultDigest != "" && binding.ResultDigest != material.digest {
		return nil, contextpack.ErrCursorInvalid
	}
	binding.RequestDigest, binding.ResultDigest = digest, material.digest
	var claims contextpack.SignedCursor
	if continuation == nil {
		if binding.ResultID != "" {
			return nil, contextpack.ErrCursorInvalid
		}
		binding.ResultID, err = newTopicResultID()
		if err != nil {
			return nil, err
		}
		anchor, err := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
		if err != nil {
			return nil, err
		}
		claims, err = codec.Verify(ctx, anchor, binding, guard)
	} else {
		if binding.ResultID == "" {
			return nil, contextpack.ErrCursorInvalid
		}
		claims, err = codec.Verify(ctx, *continuation, binding, guard)
	}
	if err != nil {
		return nil, err
	}
	start, err := topicPosition(claims.Position, material.result, continuation != nil)
	if err != nil {
		return nil, err
	}
	for ; limit >= 1; limit-- {
		end := start + limit
		if end > len(material.result.Candidates) {
			end = len(material.result.Candidates)
		}
		candidates := make([]model.Candidate, 0, end-start)
		for _, candidate := range material.result.Candidates[start:end] {
			candidates = append(candidates, topicCandidate(material.query, candidate))
		}
		response := baseResponse(options, binding.Scope)
		response.Data = model.LocateData{Operation: "locate", Candidates: candidates}
		response.Pagination.ReturnedCount = int64(len(candidates))
		if material.result.Ambiguous {
			response.Status = "partial"
			response.Warnings = append(response.Warnings, model.Gap{Code: "AMBIGUOUS_TOPIC", Reason: "The declared alias has multiple candidate entities.", SourceRefs: []model.SourceRef{}})
		}
		if end < len(material.result.Candidates) {
			key, err := topicKey(material.result.Candidates[end-1].Entity.EntityID)
			if err != nil {
				return nil, err
			}
			next := contextpack.SignedPosition{NextItem: int64(end), LastKey: key}
			if !contextpack.CursorAdvances(claims.Position, next) {
				return nil, contextpack.ErrCursorInvalid
			}
			token, err := codec.Issue(ctx, binding, next, guard)
			if err != nil {
				return nil, err
			}
			remaining, reason := int64(len(material.result.Candidates)-end), "limit"
			response.Status = "partial"
			response.Pagination.Truncated, response.Pagination.OptionalPending, response.Pagination.NextCursor, response.Pagination.LimitReason = true, &remaining, &token, &reason
		}
		blocks, err := encodeResponse(response)
		if errors.Is(err, contextpack.ErrBudgetTooSmall) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := codec.Recheck(ctx, claims, guard); err != nil {
			return nil, err
		}
		return blocks, nil
	}
	return nil, contextpack.ErrBudgetTooSmall
}

// verifyTopicDelivery rebuilds from the same pinned, approved config so a
// revoked or changed alias set cannot escape after response serialization.
func (b *CoreBackend) verifyTopicDelivery(ctx context.Context, principal app.CorePrincipal, request model.LocateRequest, response model.Response) error {
	if b.topicSource == nil || b.buildGate == nil || response.Scope == nil || request.Query == nil || request.Position != nil || len(request.Kinds) != 0 || request.Package != nil {
		return app.ErrCoreInput
	}
	select {
	case b.buildGate <- struct{}{}:
		defer func() { <-b.buildGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	material, err := b.loadTopic(ctx, principal, *response.Scope, *request.Query)
	if err != nil {
		return coreStoreError(err)
	}
	guard := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if binding.PrincipalDigest != principal.Digest || binding.BindingDigest != principal.BindingDigest || binding.OrderVersion != "locate-id-v1" {
			return ErrAuthority
		}
		_, err := b.Reader.Recheck(ctx, principal, binding.Scope)
		return err
	}
	if err := verifyTopicPage(ctx, b.Cursor, guard, principal, request, response, material); err != nil {
		return err
	}
	return material.recheck(ctx)
}

func verifyTopicPage(ctx context.Context, codec *contextpack.CursorCodec, guard contextpack.CursorGuard, principal app.CorePrincipal, request model.LocateRequest, response model.Response, material *topicMaterial) error {
	if ctx == nil || codec == nil || guard == nil || material == nil || response.Scope == nil || request.Mode != "topic" || request.Query == nil || *request.Query != material.query || request.Limit < 1 || request.Limit > 100 || model.VerifyScopeVectorDigest(*response.Scope, 32768) != nil || response.Scope.Kind != "single" || len(response.Scope.Members) != 1 || len(response.Scope.UnavailableMembers) != 0 {
		return app.ErrCoreInput
	}
	member := response.Scope.Members[0]
	if requested := request.Scope; requested != nil && (requested.GroupID != nil || requested.WorktreeID != nil && *requested.WorktreeID != member.WorktreeID || requested.ProfileID != nil && *requested.ProfileID != member.ProfileID || requested.GenerationID != nil && *requested.GenerationID != member.GenerationID || requested.PinnedVector != nil && model.ComparePinnedScope(*requested.PinnedVector, *response.Scope, 32768) != nil) {
		return contextpack.ErrCursorInvalid
	}
	continuation := request.Cursor
	limit := request.Limit
	request.Cursor, request.Limit = nil, 20
	digest, err := model.RequestDigest("locate", principal.ID, "LocateRequest", request, 32768, 4)
	if err != nil {
		return app.ErrCoreInput
	}
	checkClaims := func(claims contextpack.SignedCursor) bool {
		return claims.Binding.RequestDigest == digest && claims.Binding.ResultDigest == material.digest && claims.Binding.ResultID != "" && model.ComparePinnedScope(*response.Scope, claims.Binding.Scope, 32768) == nil
	}
	start := 0
	previousID := ""
	if continuation != nil {
		previous, err := codec.VerifyPrincipal(ctx, *continuation, "list", principal.Digest, guard)
		if err != nil {
			return coreStoreError(err)
		}
		if !checkClaims(previous) {
			return contextpack.ErrCursorInvalid
		}
		start, err = topicPosition(previous.Position, material.result, true)
		if err != nil {
			return err
		}
		previousID = previous.Binding.ResultID
	}
	data, ok := response.Data.(model.LocateData)
	if !ok || int64(len(data.Candidates)) > limit || start+len(data.Candidates) > len(material.result.Candidates) {
		return app.ErrCoreBackend
	}
	for i, candidate := range data.Candidates {
		if !reflect.DeepEqual(candidate, topicCandidate(material.query, material.result.Candidates[start+i])) {
			return contextpack.ErrCursorInvalid
		}
	}
	end := start + len(data.Candidates)
	remaining := int64(len(material.result.Candidates) - end)
	if !response.Pagination.EnumerationComplete || response.Pagination.ReturnedCount != int64(len(data.Candidates)) || response.Pagination.RequiredPending == nil || *response.Pagination.RequiredPending != 0 || response.Pagination.OptionalPending == nil || *response.Pagination.OptionalPending != remaining || response.Pagination.Truncated != (remaining > 0) {
		return app.ErrCoreBackend
	}
	if response.Pagination.NextCursor != nil {
		next, err := codec.VerifyPrincipal(ctx, *response.Pagination.NextCursor, "list", principal.Digest, guard)
		if err != nil {
			return coreStoreError(err)
		}
		if !checkClaims(next) || previousID != "" && next.Binding.ResultID != previousID || end == start || end >= len(material.result.Candidates) || next.Position.NextItem != int64(end) {
			return contextpack.ErrCursorInvalid
		}
		if nextStart, err := topicPosition(next.Position, material.result, true); err != nil || nextStart != end {
			return contextpack.ErrCursorInvalid
		}
	} else if remaining != 0 {
		return contextpack.ErrCursorInvalid
	}
	found := false
	for _, warning := range response.Warnings {
		if warning.Code == "AMBIGUOUS_TOPIC" {
			found = true
		}
	}
	if found != material.result.Ambiguous || (material.result.Ambiguous || remaining > 0) && response.Status != "partial" || !material.result.Ambiguous && remaining == 0 && response.Status != "ok" {
		return app.ErrCoreBackend
	}
	return ctx.Err()
}
