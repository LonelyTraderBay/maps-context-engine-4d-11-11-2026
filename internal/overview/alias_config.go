package overview

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// AliasConfigInput reads one explicitly trusted captured JSON file; it never
// discovers or executes a project configuration. The callback checks current
// actor access and approval for this exact path and pinned policy epoch.
type AliasConfigInput struct {
	Overview        *Report
	Snapshot        *snapshot.Snapshot
	Policy          model.PolicyConfig
	ConfigPath      string
	Scanner         *policy.Scanner
	AuthorizeConfig func(context.Context, Scope, string) error
	AuthorizeAlias  func(context.Context, Scope, AliasDeclaration) error
	MaxBytes        int
}

func exactFields(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(names) {
		return nil, ErrInput
	}
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrInput
		}
	}
	return fields, nil
}

// Alias config v1 is a closed local data format:
// {"schema_version":1,"entries":[{"alias":"...","entity_id":"...",
// "path":"...","range":{"start_byte":0,"end_byte":3}}]}.
// Each range must resolve exactly to alias bytes in the same captured scope.
func BuildAliasIndexFromConfig(ctx context.Context, in AliasConfigInput) (*AliasIndex, error) {
	if ctx == nil || in.Overview == nil || in.Snapshot == nil || in.Scanner == nil || in.AuthorizeConfig == nil || in.AuthorizeAlias == nil || !boundedPlanPolicy(in.Policy) || len(in.ConfigPath) > 16384 || in.MaxBytes < 4096 || in.MaxBytes > 16*1024*1024 {
		return nil, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrInput
	}
	scope := in.Overview.scope
	manifest := in.Snapshot.Manifest()
	if manifest.WorktreeID != scope.Identity.WorktreeID || manifest.ProfileID != scope.Identity.ProfileID || manifest.SnapshotDigest != scope.SnapshotDigest || manifest.PolicyDigest != scope.PolicyDigest || model.VerifyCaptureDigest(manifest, 64*1024*1024) != nil {
		return nil, ErrInput
	}
	rawPolicy, err := json.Marshal(in.Policy)
	if err != nil || len(rawPolicy) > 1024*1024 {
		return nil, ErrInput
	}
	approved, err := model.DecodePolicyConfigJSON(rawPolicy, 1024*1024)
	if err != nil {
		return nil, ErrInput
	}
	digest, err := model.PolicyDigest(approved, 1024*1024)
	if err != nil || digest != scope.PolicyDigest {
		return nil, ErrInput
	}
	trusted := false
	for _, name := range approved.TrustedRulePaths {
		if name == in.ConfigPath {
			trusted = true
		}
	}
	if !trusted || policy.CheckPath(in.ConfigPath) != nil || in.Scanner.CheckContent([]byte(in.ConfigPath)) != nil {
		return nil, ErrAuthority
	}
	checkConfig := func(ctx context.Context) error {
		if err := in.Overview.authorize(ctx, []string{in.ConfigPath}); err != nil {
			return err
		}
		if err := in.AuthorizeConfig(ctx, scope, in.ConfigPath); err != nil {
			return authorityError(ctx)
		}
		return ctx.Err()
	}
	if err = checkConfig(ctx); err != nil {
		return nil, err
	}
	file, ok := in.Snapshot.File(in.ConfigPath)
	if !ok || file.Len() > 1024*1024 || file.Len() > in.MaxBytes || in.Scanner.CheckContent(file.Bytes()) != nil {
		return nil, ErrInput
	}
	raw := file.Bytes()
	if _, err = model.DecodeIdentityJSON(raw, 1024*1024); err != nil {
		return nil, ErrInput
	}
	root, err := exactFields(raw, "schema_version", "entries")
	if err != nil {
		return nil, err
	}
	var version int64
	if json.Unmarshal(root["schema_version"], &version) != nil || version != 1 {
		return nil, ErrInput
	}
	var entries []json.RawMessage
	if json.Unmarshal(root["entries"], &entries) != nil || entries == nil || len(entries) > 10000 {
		return nil, ErrInput
	}
	declarations := make([]AliasDeclaration, 0, len(entries))
	for _, encoded := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		fields, e := exactFields(encoded, "alias", "entity_id", "path", "range")
		if e != nil {
			return nil, e
		}
		if _, e = exactFields(fields["range"], "start_byte", "end_byte"); e != nil {
			return nil, e
		}
		var alias, entityID, path string
		var bounds model.ByteRange
		if json.Unmarshal(fields["alias"], &alias) != nil || json.Unmarshal(fields["entity_id"], &entityID) != nil || json.Unmarshal(fields["path"], &path) != nil || json.Unmarshal(fields["range"], &bounds) != nil || len(alias) > 4096 || len(entityID) > 768 || len(path) > 16384 || policy.CheckPath(path) != nil || in.Scanner.CheckContent([]byte(path)) != nil {
			return nil, ErrInput
		}
		if err = in.Overview.authorize(ctx, []string{path}); err != nil {
			return nil, err
		}
		source, ok := in.Snapshot.File(path)
		if !ok {
			return nil, ErrInput
		}
		ref := model.SourceRef{WorktreeID: scope.Identity.WorktreeID, ProfileID: scope.Identity.ProfileID, GenerationID: scope.Identity.GenerationID, Path: path, ContentHash: source.ContentHash(), Range: bounds}
		declarations = append(declarations, AliasDeclaration{Alias: alias, Entity: model.EntityRef{Kind: "entity", GraphIdentity: scope.Identity, EntityID: entityID}, Source: ref})
	}
	_, expected, err := normalizeAliasSet(declarations, in.MaxBytes)
	if err != nil {
		return nil, err
	}
	verify := func(ctx context.Context, observed Scope, actual string) error {
		if observed != scope || actual != expected {
			return ErrAuthority
		}
		return checkConfig(ctx)
	}
	index, err := BuildAliasIndex(ctx, AliasInput{Overview: in.Overview, Snapshot: in.Snapshot, Scanner: in.Scanner, Declarations: declarations, VerifyAliasSet: verify, AuthorizeAlias: in.AuthorizeAlias, MaxBytes: in.MaxBytes})
	if err != nil {
		return nil, err
	}
	if err = checkConfig(ctx); err != nil {
		return nil, err
	}
	return index, nil
}
