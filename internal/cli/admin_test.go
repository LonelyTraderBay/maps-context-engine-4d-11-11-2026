package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const adminWorktree = "01234567-89ab-cdef-0123-456789abcdef"

type adminFixture struct {
	calls int
	err   error
}

func (a *adminFixture) SetProfile(_ context.Context, raw []byte) (model.ProfileAdminData, error) {
	a.calls++
	if a.err != nil {
		return model.ProfileAdminData{}, a.err
	}
	r, err := model.DecodeProfileAdminRequestJSON(raw, 32768)
	if err != nil {
		return model.ProfileAdminData{}, err
	}
	digest, err := model.ProfileDigest(r.Profile, 32768)
	return model.ProfileAdminData{Operation: "profile_admin", WorktreeID: r.WorktreeID, ProfileName: r.ProfileName, ProfileID: digest, Revision: r.ExpectedRevision + 1}, err
}
func (a *adminFixture) SetPolicy(_ context.Context, raw []byte) (model.PolicyAdminData, error) {
	a.calls++
	if a.err != nil {
		return model.PolicyAdminData{}, a.err
	}
	r, err := model.DecodePolicyAdminRequestJSON(raw, 32768)
	if err != nil {
		return model.PolicyAdminData{}, err
	}
	digest, err := model.PolicyDigest(r.Policy, 32768)
	return model.PolicyAdminData{Operation: "policy_admin", WorktreeID: r.WorktreeID, PolicyDigest: digest, PolicyEpoch: r.ExpectedEpoch + 1}, err
}

func adminRequests(t *testing.T) ([]byte, []byte) {
	t.Helper()
	hash := strings.Repeat("a", 64)
	profile := &model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: hash, ProviderDigest: hash, ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	policy := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{"/fixture"}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 1024, MaxSourceBytes: 8192, MaxOutputBytes: 4096, SecretRulesVersion: "v1"}
	p, err := json.Marshal(model.ProfileAdminRequest{WorktreeID: adminWorktree, ProfileName: "default", ExpectedRevision: 1, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	q, err := json.Marshal(model.PolicyAdminRequest{WorktreeID: adminWorktree, ExpectedEpoch: 1, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	return p, q
}

func decodeAdmin(t *testing.T, name string, result Result) model.Response {
	t.Helper()
	registry, _ := contracts.LoadRegistry()
	response, err := registry.DecodeResponse(contracts.CLI, "CLI "+name+" set", result.JSON, 131072, 4)
	if err != nil || model.ValidateDeliveredBytes(response.Budget, [][]byte{result.JSON}) != nil {
		t.Fatalf("admin response: %v", err)
	}
	return response
}

func TestAdminBindsScopeAndChecksCommittedDigest(t *testing.T) {
	profile, policy := adminRequests(t)
	owner := &adminFixture{}
	opened := 0
	open := func(ctx context.Context, config, kind, worktree string) (AdminOwner, func() error, error) {
		opened++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > administrativeTimeout {
			t.Fatal("administration owner has no bounded deadline")
		}
		if config != "/fixture/runtime.json" || worktree != adminWorktree || kind != "profile" && kind != "policy" {
			t.Fatal("incorrect administrative binding")
		}
		return owner, func() error { return nil }, nil
	}
	result, err := ExecuteAdmin(context.Background(), "profile", "11111111-1111-4111-8111-111111111111", profile, 4, "/fixture/runtime.json", open)
	if err != nil || result.ExitCode != 2 || opened != 0 || owner.calls != 0 {
		t.Fatal("mismatched worktree opened the owner")
	}
	for _, item := range []struct {
		kind, flag string
		raw        []byte
	}{{"profile", adminWorktree, profile}, {"policy", "", policy}} {
		result, err = ExecuteAdmin(context.Background(), item.kind, item.flag, item.raw, 4, "/fixture/runtime.json", open)
		if err != nil || result.ExitCode != 0 || decodeAdmin(t, item.kind, result).Error != nil {
			t.Fatalf("%s mutation: %v", item.kind, err)
		}
	}
	if opened != 2 || owner.calls != 2 {
		t.Fatal("unexpected administrative dispatch count")
	}
}

func TestAdminConservativelyReportsUnknownOutcome(t *testing.T) {
	profile, _ := adminRequests(t)
	for _, test := range []struct {
		err      error
		wantCode string
	}{{ErrAdminConflict, "PROFILE_MISMATCH"}, {ErrAdminDenied, "ACCESS_DENIED"}, {errors.New("private storage failure"), "STORE_RECOVERY_REQUIRED"}} {
		owner := &adminFixture{err: test.err}
		result, err := ExecuteAdmin(context.Background(), "profile", adminWorktree, profile, 4, "", func(context.Context, string, string, string) (AdminOwner, func() error, error) {
			return owner, func() error { return nil }, nil
		})
		if err != nil || result.ExitCode != 3 || decodeAdmin(t, "profile", result).Error.Code != test.wantCode || bytes.Contains(result.JSON, []byte("private storage failure")) {
			t.Fatalf("unsafe outcome for %v: %v", test.err, err)
		}
	}
	owner := &adminFixture{}
	result, err := ExecuteAdmin(context.Background(), "profile", adminWorktree, profile, 4, "", func(context.Context, string, string, string) (AdminOwner, func() error, error) {
		return owner, func() error { return errors.New("private cleanup failure") }, nil
	})
	if err != nil || result.ExitCode != 3 || decodeAdmin(t, "profile", result).Error.Code != "STORE_RECOVERY_REQUIRED" || bytes.Contains(result.JSON, []byte("private cleanup failure")) {
		t.Fatal("cleanup failure concealed a possibly applied write")
	}
}

func TestRunProfileAdminMatchesFlagAndBody(t *testing.T) {
	profile, _ := adminRequests(t)
	owner := &adminFixture{}
	deps := Dependencies{CPUs: 4, OpenAdmin: func(_ context.Context, _, kind, worktree string) (AdminOwner, func() error, error) {
		if kind != "profile" || worktree != adminWorktree {
			return nil, nil, ErrAdminDenied
		}
		return owner, func() error { return nil }, nil
	}}
	var out, errOut bytes.Buffer
	args := []string{"profile", "set", "--worktree-id", adminWorktree, "--input-json", "-"}
	code := Run(context.Background(), args, io.NopCloser(bytes.NewReader(profile)), &out, &errOut, deps)
	if code != 0 || errOut.Len() != 0 || owner.calls != 1 || decodeAdmin(t, "profile", Result{JSON: bytes.TrimSpace(out.Bytes()), ExitCode: code}).Error != nil {
		t.Fatal("CLI profile administration failed")
	}
}
