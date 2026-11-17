//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

// Installed as the protected runtime in the root-owned synthetic namespace.
// These use real OS/store/catalog/input/toolchain authorities. Keeping backend
// errors inside this fixture diagnoses failures while wire errors stay redacted.
func TestNativeRenameOwnerSetupFixture(t *testing.T) {
	ctx := nativeRuntimeFixture(t)
	reg, err := RegisterNativeRepo(ctx, "/profiles/runtime", "", "/target", 4)
	if err != nil {
		t.Fatal(err)
	}
	admin, release, err := OpenNativeAdmin(ctx, "/profiles/runtime", "", "policy", reg.WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := admin.store.ReadBinding(ctx, reg.WorktreeID, reg.ProfileID, func(ctx context.Context, _, _ string) error { return admin.state.Check(ctx) })
	if err != nil {
		t.Fatal(err)
	}
	policyConfig := binding.Policy
	for _, name := range []string{"/git", "/toolchain/bin/go", "/bin/gopls"} {
		if runtime.CheckAdministratorPath(name) != nil {
			t.Fatal("untrusted fixture binary")
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		policyConfig.AllowedExecutables = append(policyConfig.AllowedExecutables, model.AllowedExecutable{Path: name, SHA256: hex.EncodeToString(sum[:])})
	}
	policyConfig.TrustedRulePaths = []string{"AGENTS.md"}
	_, err = admin.SetPolicy(ctx, scopeJSON(t, model.PolicyAdminRequest{WorktreeID: reg.WorktreeID, ExpectedEpoch: binding.PolicyEpoch, Policy: policyConfig}))
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	proof, err := runtime.InspectGoToolchain(ctx, "/toolchain")
	if err != nil {
		t.Fatal(err)
	}
	digest := proof.Digest()
	if err := proof.Close(); err != nil {
		t.Fatal(err)
	}
	admin, release, err = OpenNativeAdmin(ctx, "/profiles/runtime", "", "profile", reg.WorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	profile := *binding.Profile
	profile.AnalysisMode = "typed_when_available"
	profile.ToolchainDigest = digest
	updated, err := admin.SetProfile(ctx, scopeJSON(t, model.ProfileAdminRequest{WorktreeID: reg.WorktreeID, ProfileName: "typed", ExpectedRevision: 0, Profile: &profile}))
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal([2]string{reg.WorktreeID, updated.ProfileID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/result/native-rename.json", raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRenameOwnerCallFixture(t *testing.T) {
	ctx := nativeRuntimeFixture(t)
	raw, err := os.ReadFile("/result/native-rename.json")
	if err != nil {
		t.Fatal(err)
	}
	var registration [2]string
	if err := json.Unmarshal(raw, &registration); err != nil {
		t.Fatal(err)
	}
	if err := RunNativeIndex(ctx, "/profiles/runtime", "", registration[0], registration[1], 4, false, false, nil); err != nil {
		t.Fatal(err)
	}
	n, err := openNativeScope(ctx, "/profiles/runtime", "", registration[0], registration[1], 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := n.Close(); err != nil {
			t.Error(err)
		}
	}()
	hash := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	id := "installation:" + n.state.InstallationID()
	p := app.CorePrincipal{ID: id, Digest: hash("MCE4D-OS-PRINCIPAL1\x00" + id), BindingDigest: hash("MCE4D-BINDING1\x00" + id + "\x00" + registration[0] + "\x00" + registration[1])}
	idx, err := n.stores.Index.Store()
	if err != nil {
		t.Fatal(err)
	}
	records, err := n.stores.Records.Store()
	if err != nil {
		t.Fatal(err)
	}
	access := func(ctx context.Context, actor app.CorePrincipal, w, profile string) error {
		if actor != p || w != registration[0] || profile != registration[1] {
			return ErrAuthority
		}
		return n.boundary(ctx)
	}
	reader := &ScopeReader{Catalog: n.catalog, Index: idx, Records: records, WorktreeRoot: n.target, CommonRoot: n.common, LinkedRelative: n.linked, WorktreeID: registration[0], ProfileID: registration[1], Access: access, MaxHeads: 100000, MaxManifestBytes: 64 << 20}
	authority := &BoundCoreAuthority{Reader: reader, AuthenticatePrincipal: func(ctx context.Context) (app.CorePrincipal, error) {
		return p, access(ctx, p, registration[0], registration[1])
	}, AuthorizePath: func(ctx context.Context, actor app.CorePrincipal, config model.PolicyConfig, name string) error {
		if err := access(ctx, actor, registration[0], registration[1]); err != nil {
			return err
		}
		if err := policy.CheckConfiguredPath(name, config.DeniedGlobs); err != nil {
			return err
		}
		file, err := n.target.OpenRegular(name)
		if err != nil {
			return err
		}
		return file.Close()
	}}
	key, err := n.state.CursorKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec(key)
	clear(key[:])
	if err != nil {
		t.Fatal(err)
	}
	cache, err := app.NewFreshnessPackCache(codec, 32, 32<<20, 900)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewCoreBackend(n.config, reader, authority, codec, cache, nativeWorkspaceVerifier(n, reader, p))
	if err != nil {
		t.Fatal(err)
	}
	backend, err = attachNativeRename(ctx, n, backend, p)
	if err != nil {
		t.Fatal(err)
	}
	if !backend.SupportsCLIRenameRetention() {
		t.Fatal("native owner was not installed")
	}
	file, err := n.target.OpenRegular("a.go")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(file, 65537))
	if err = errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	options := app.CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 24576, DeliveryMode: "cli"}
	blocks, err := backend.RenamePreview(ctx, p, &model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: hash(string(body)), ByteOffset: int64(strings.Index(string(body), "Value"))}, NewName: "Count", PreserveContracts: true}, options)
	if err != nil {
		t.Fatalf("native backend preview: %+v", err)
	}
	response, err := model.DecodeResponseJSON(blocks[0], 65536, 4, "rename_preview", false)
	if err != nil {
		t.Fatal(err)
	}
	data := response.Data.(model.RenameData)
	if !data.PreviewComplete || len(data.Edits) != 4 {
		t.Fatal("native preview lost edits")
	}
	if _, err := backend.validateRename(ctx, p, data.ResultToken, options); err != nil {
		t.Fatalf("native backend validation: %+v", err)
	}
	t.Log("NATIVE_TYPED_OWNER_PROOF_AND_PREVIEW_PASS")
}
