package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/overview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestFreshnessPlanArchivePreservesModeAndRechecksAuthority(t *testing.T) {
	for _, mode := range []string{"analysis_snapshot", "checked_workspace"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			const worktree = "11111111-1111-4111-8111-111111111111"
			const generation = "22222222-2222-4222-8222-222222222222"
			profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: strings.Repeat("b", 64), ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
			profileID, err := model.ProfileDigest(profile, 65536)
			if err != nil {
				t.Fatal(err)
			}
			config := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/fixture"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{"spec.md"}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 65536, MaxSourceBytes: 1048576, MaxOutputBytes: 65536, SecretRulesVersion: "v1"}
			policyID, err := model.PolicyDigest(config, 65536)
			if err != nil {
				t.Fatal(err)
			}
			files := []snapshot.File{}
			manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profileID, PolicyDigest: policyID, ProviderDigest: profile.ProviderDigest, Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
			for path, body := range map[string]string{"go.mod": "module example.invalid/fixture\n\ngo 1.27\n", "spec.md": "Objective: preserve the contract.\n"} {
				file, err := snapshot.NewFile(path, []byte(body), 65536)
				if err != nil {
					t.Fatal(err)
				}
				hash, size := file.ContentHash(), int64(file.Len())
				manifest.Files = append(manifest.Files, model.ManifestFile{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &size})
				files = append(files, file)
			}
			captured, err := snapshot.Freeze(manifest, files, 65536, 1048576)
			if err != nil {
				t.Fatal(err)
			}
			scanner, err := policy.NewScanner(65536)
			if err != nil {
				t.Fatal(err)
			}
			allowed, changed := true, false
			scopeAuth := func(context.Context, overview.Scope) error {
				if !allowed {
					return errors.New("revoked")
				}
				return nil
			}
			pathAuth := func(ctx context.Context, scope overview.Scope, _ string) error { return scopeAuth(ctx, scope) }
			factsAuth := func(ctx context.Context, scope overview.Scope, _ string) error { return scopeAuth(ctx, scope) }
			docAuth := func(ctx context.Context, scope overview.Scope, _ overview.DocumentDeclaration) error {
				return scopeAuth(ctx, scope)
			}
			view, err := overview.Build(ctx, overview.Input{Snapshot: captured, Profile: profile, GenerationID: generation, PolicyEpoch: 1, Scanner: scanner, MaxManifestBytes: 65536, MaxSourceBytes: 1048576, MaxFacts: 10000, MaxOutputBytes: 1048576, AuthorizeScope: scopeAuth, AuthorizePath: pathAuth, VerifyFacts: factsAuth})
			if err != nil {
				t.Fatal(err)
			}
			doc, _ := captured.File("spec.md")
			declaration := overview.DocumentDeclaration{Kind: "objective", Source: model.SourceRef{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, Path: "spec.md", ContentHash: doc.ContentHash(), Range: model.ByteRange{StartByte: 0, EndByte: int64(doc.Len())}}}
			plan, err := overview.BuildPlan(ctx, overview.PlanInput{Overview: view, Snapshot: captured, Policy: config, Selector: &model.WorkspaceSelector{Kind: "workspace"}, Declarations: []overview.DocumentDeclaration{declaration}, Scanner: scanner, TaskKind: "plan", AuthorizeDocument: docAuth, MaxBytes: 65536})
			if err != nil {
				t.Fatal(err)
			}
			member := model.ScopeMember{WorktreeID: worktree, ProfileID: profileID, GenerationID: generation, SnapshotDigest: captured.Manifest().SnapshotDigest, PolicyDigest: policyID, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
			scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
			scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
			if err != nil {
				t.Fatal(err)
			}
			raw := []byte(`{"selector":{"kind":"workspace"},"task_kind":"plan","freshness_mode":"` + mode + `","max_output_bytes":16384}`)
			request, err := model.DecodePackRequestJSON(raw, 65536)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := model.RequestDigest("context_pack", "fixture-actor", "PackRequest", request, 65536, 4)
			if err != nil {
				t.Fatal(err)
			}
			codec, err := contextpack.NewCursorCodec([32]byte{1, 2, 3})
			if err != nil {
				t.Fatal(err)
			}
			guard := func(context.Context, contextpack.CursorBinding) error {
				if !allowed {
					return errors.New("revoked")
				}
				return nil
			}
			binding := contextpack.CursorBinding{Purpose: "pack", PrincipalDigest: strings.Repeat("1", 64), BindingDigest: strings.Repeat("2", 64), RequestDigest: digest, OrderVersion: "pack-v1", Scope: scope}
			validation := ValidateCapturedInput{Scope: scope, MaxInputBytes: 65536, MaxManifestBytes: 65536,
				CurrentScope:   func(context.Context) (model.ScopeVector, error) { return scope, nil },
				ResolveCapture: func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error) { return captured, nil },
				AuthorizeSource: func(context.Context, model.ScopeMember, model.SourceRef) error {
					if !allowed {
						return errors.New("revoked")
					}
					return nil
				},
				VerifyWorkspace: func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
					if changed {
						return WorkspaceChanged, nil
					}
					return WorkspaceObservedCurrent, nil
				}}
			capture := func(_ context.Context, _ model.ScopeMember, path string) (snapshot.File, error) {
				if changed && path == "spec.md" {
					return snapshot.NewFile(path, []byte("changed"), 65536)
				}
				file, ok := captured.File(path)
				if !ok {
					return snapshot.File{}, errors.New("missing")
				}
				return file, nil
			}
			coverage := model.Coverage{Status: "partial", Capabilities: []model.Capability{}, Gaps: []model.Gap{}, ScopeDescription: "fixture"}
			revision := strings.Repeat("d", 64)
			pack, token, err := NewFreshnessPlan(ctx, FreshnessPlanInput{Plan: plan, Codec: codec, Binding: binding, Guard: guard, RequestJSON: raw, PrincipalID: "fixture-actor", Coverage: coverage, Scanner: scanner, Validation: validation, CaptureLive: capture, RequestLimit: 65536, DeclarationRevisionDigest: revision})
			if err != nil {
				t.Fatal(err)
			}
			options := contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "cli"}
			first, err := pack.First(ctx, options)
			if err != nil || first.Response.Pagination.NextCursor == nil {
				t.Fatalf("fixture missing continuation: %v", err)
			}
			envelope, err := pack.ArchiveEnvelope(ctx, "fixture-actor", raw, 30, 1024*1024)
			if err != nil || envelope.Token != token {
				t.Fatalf("archive failed: %v", err)
			}
			pack.declarationRevisionDigest = ""
			if _, err := pack.ArchiveEnvelope(ctx, "fixture-actor", raw, 30, 1024*1024); !errors.Is(err, ErrValidationAuthority) {
				t.Fatalf("plan without revision proof was persisted: %v", err)
			}
			cancelled, cancelNow := context.WithCancel(ctx)
			cancelNow()
			if result, err := pack.ArchiveEnvelope(cancelled, "fixture-actor", raw, 30, 1024*1024); !errors.Is(err, context.Canceled) || len(result.Bytes) != 0 {
				t.Fatalf("cancelled archive returned data or hid cancellation: %v", err)
			}
			pack.declarationRevisionDigest = revision
			verifyDeclarations := func(ctx context.Context, s overview.Scope, declarations []overview.DocumentDeclaration) error {
				if len(declarations) != 1 || declarations[0] != declaration {
					return errors.New("declaration set changed")
				}
				return scopeAuth(ctx, s)
			}
			input := FreshnessPlanRestoreInput{Plan: overview.PlanRestoreInput{Codec: codec, PackToken: token, PrincipalDigest: binding.PrincipalDigest, Guard: guard, Snapshot: captured, AuthorizeScope: scopeAuth, AuthorizePath: pathAuth, VerifyFacts: factsAuth, AuthorizeDocument: docAuth, VerifyDeclarations: verifyDeclarations, MaxArchiveBytes: 1024 * 1024}, PrincipalID: "fixture-actor", Scanner: scanner, Validation: validation, CaptureLive: capture, RequestLimit: 65536, DeclarationRevisionDigest: revision}
			restored, err := RestoreFreshnessPlanEnvelope(ctx, envelope.Bytes, input)
			if err != nil {
				t.Fatalf("restore failed: %v", err)
			}
			before, err := pack.Continue(ctx, token, *first.Response.Pagination.NextCursor, binding.PrincipalDigest, guard, options)
			if err != nil {
				t.Fatal(err)
			}
			after, err := restored.Continue(ctx, token, *first.Response.Pagination.NextCursor, binding.PrincipalDigest, guard, options)
			if err != nil || !reflect.DeepEqual(before.Response.Data, after.Response.Data) || before.Response.Freshness.Status != after.Response.Freshness.Status {
				t.Fatalf("restored plan changed continuation or freshness: %v", err)
			}
			wrong := input
			wrong.PrincipalID = "other-actor"
			if _, err := RestoreFreshnessPlanEnvelope(ctx, envelope.Bytes, wrong); err == nil {
				t.Fatal("wrong request actor accepted")
			}
			wrong = input
			wrong.DeclarationRevisionDigest = strings.Repeat("e", 64)
			if _, err := RestoreFreshnessPlanEnvelope(ctx, envelope.Bytes, wrong); err == nil {
				t.Fatal("changed declaration revision accepted")
			}
			changedRequest, _ := json.Marshal(model.PackRequest{TaskKind: "explore", Selector: &model.WorkspaceSelector{Kind: "workspace"}})
			if _, err := pack.ArchiveEnvelope(ctx, "fixture-actor", changedRequest, 30, 1024*1024); err == nil {
				t.Fatal("changed request archived under old token")
			}
			if mode == "checked_workspace" {
				result, err := restored.Validate(ctx, token, binding.PrincipalDigest, guard)
				if err != nil || !result.Data.EditPreconditionValidated {
					t.Fatalf("restored validation failed: %v", err)
				}
				changed = true
				if result, err := restored.Validate(ctx, token, binding.PrincipalDigest, guard); err == nil || result.Data.EditPreconditionValidated {
					t.Fatal("changed workspace was accepted")
				}
				changed = false
			}
			allowed = false
			if page, err := restored.Continue(ctx, token, *first.Response.Pagination.NextCursor, binding.PrincipalDigest, guard, options); err == nil || len(page.Blocks) != 0 {
				t.Fatal("revoked plan returned data")
			}
		})
	}
}
