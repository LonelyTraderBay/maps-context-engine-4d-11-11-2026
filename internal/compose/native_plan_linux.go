//go:build linux

package compose

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// R1 declarations come only from the current policy's explicit trusted-rule
// paths. Arbitrary Markdown and AGENTS files never become approved objectives
// or constraints. Approved record bodies remain unavailable until their owner
// is integrated; a nonempty approved record set therefore refuses this plan.
func nativePlanSource(n *nativeScope, reader *ScopeReader, principal app.CorePrincipal) PlanDeclarationProvider {
	return func(ctx context.Context, actor app.CorePrincipal, scope model.ScopeVector, _ model.PackRequest) (PlanDeclarations, error) {
		if actor != principal || !boundedSingleScope(scope) {
			return PlanDeclarations{}, ErrAuthority
		}
		current, err := reader.Recheck(ctx, actor, scope)
		if err != nil {
			return PlanDeclarations{}, err
		}
		if current.Records.ApprovedHeads != 0 {
			return PlanDeclarations{}, app.ErrCoreUnsupported
		}
		member := scope.Members[0]
		if member.PolicyDigest != n.binding.PolicyDigest || member.PolicyEpoch != n.binding.PolicyEpoch || member.BindingRevision != n.binding.BindingRevision || scope.CatalogIncarnation != n.binding.CatalogIncarnation {
			return PlanDeclarations{}, ErrScopeChanged
		}
		// This independent declaration registry is the catalog policy head.
		// The enclosing plan separately rechecks full scope and record absence;
		// do not nest those index/records reads in each registry callback.
		recheck := func(ctx context.Context) error {
			if err := n.boundary(ctx); err != nil {
				return err
			}
			// This registry revision is a live catalog head, not its document
			// content. The enclosing Reader/Plan still validates complete
			// binding, policy JSON, records and source/fact authority.
			if err := n.catalog.CheckBindingStamp(ctx, n.binding, func(ctx context.Context, w, p string) error {
				if w != n.binding.WorktreeID || p != n.binding.ProfileID {
					return ErrAuthority
				}
				return n.state.Check(ctx)
			}); err != nil {
				return err
			}
			return n.binding.VerifyRoots(n.target, n.common, n.linked)
		}
		revision, err := DeclarationRevisionDigest("installation:"+n.state.InstallationID()+":policy-rules:"+member.WorktreeID,
			fmt.Sprintf("r1-policy-rules-v1:%s:%d:%d:%s", scope.CatalogIncarnation, member.BindingRevision, member.PolicyEpoch, member.PolicyDigest))
		if err != nil {
			return PlanDeclarations{}, err
		}
		scanner, err := policy.NewScanner(64 << 20)
		if err != nil {
			return PlanDeclarations{}, err
		}
		items := make([]overview.DocumentDeclaration, 0, len(current.Binding.Policy.TrustedRulePaths))
		allowed := map[string]bool{}
		retained := int64(65536) // bounded single scope, actor, revision and closure bookkeeping
		for _, name := range current.Binding.Policy.TrustedRulePaths {
			file, err := reader.Index.ReadPublishedFile(ctx, index.PublishedFileRequest{ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: name, Scanner: scanner, MaxManifestBytes: 64 << 20, MaxFileBytes: int(minInt64(current.Binding.Policy.MaxFileBytes, n.effective.Core.MaxFileBytes, 2<<20)),
				AuthorizeGeneration: func(ctx context.Context, w, p, d string, e int64) error {
					if w != member.WorktreeID || p != member.ProfileID || d != member.PolicyDigest || e != member.PolicyEpoch {
						return ErrScopeChanged
					}
					return reader.Access(ctx, actor, w, p)
				}, AuthorizePath: func(ctx context.Context, w, path, d string, e int64) error {
					if w != member.WorktreeID || path != name || d != member.PolicyDigest || e != member.PolicyEpoch {
						return ErrScopeChanged
					}
					if err := policy.CheckConfiguredPath(path, current.Binding.Policy.DeniedGlobs); err != nil {
						return err
					}
					return reader.Access(ctx, actor, w, member.ProfileID)
				}})
			if err != nil {
				return PlanDeclarations{}, err
			}
			if file.IndexIncarnation != member.IndexIncarnation || file.SnapshotDigest != member.SnapshotDigest {
				return PlanDeclarations{}, ErrScopeChanged
			}
			if len(file.Bytes) == 0 {
				return PlanDeclarations{}, app.ErrCoreUnsupported
			}
			item := overview.DocumentDeclaration{Kind: "constraint", Source: model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: name, ContentHash: file.ContentHash, Range: model.ByteRange{StartByte: 0, EndByte: int64(len(file.Bytes))}}}
			raw, err := json.Marshal(item)
			if err != nil {
				return PlanDeclarations{}, err
			}
			allowed[string(raw)] = true
			retained += int64(len(raw))*2 + 128
			items = append(items, item)
		}
		if err := recheck(ctx); err != nil {
			return PlanDeclarations{}, err
		}
		expected := overview.Scope{Identity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}, SnapshotDigest: member.SnapshotDigest, PolicyDigest: member.PolicyDigest, PolicyEpoch: member.PolicyEpoch}
		return PlanDeclarations{Items: items, Recheck: recheck, RetainedBytes: retained, RevisionDigest: revision,
			AuthorizeDocument: func(ctx context.Context, asked overview.Scope, item overview.DocumentDeclaration) error {
				if asked != expected {
					return ErrScopeChanged
				}
				raw, err := json.Marshal(item)
				if err != nil || !allowed[string(raw)] {
					return ErrAuthority
				}
				return recheck(ctx)
			}}, nil
	}
}
