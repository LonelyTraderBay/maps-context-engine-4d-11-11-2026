//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

// Only this test executable dispatches fixture cases; production exposes only
// the fixed protected worker roles from the shared implementation.
func TestMain(m *testing.M) {
	if handled, code := sqliteowner.RunProtectedWorker(context.Background(), os.Args[1:], os.Stdout); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func nativeRuntimeFixture(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("MCE4D_NATIVE_RUNTIME_FIXTURE") != "synthetic-v1" {
		t.Skip("requires root-owned synthetic runtime profile")
	}
	marker, err := os.ReadFile("/fixture.owner")
	if err != nil || string(marker) != "mce4d-native-runtime-fixture-v1\n" {
		t.Fatal("invalid runtime fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestNativeRuntimeRegisterFixture(t *testing.T) {
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
	raw, err := os.ReadFile("/git")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	policy := binding.Policy
	policy.AllowedExecutables = []model.AllowedExecutable{{Path: "/git", SHA256: hex.EncodeToString(sum[:])}}
	req, err := json.Marshal(model.PolicyAdminRequest{WorktreeID: reg.WorktreeID, ExpectedEpoch: binding.PolicyEpoch, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.SetPolicy(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/result/registration.json", data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRuntimeIndexFixture(t *testing.T) {
	ctx := nativeRuntimeFixture(t)
	raw, err := os.ReadFile("/result/registration.json")
	if err != nil {
		t.Fatal(err)
	}
	var reg catalog.Registration
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	if err := RunNativeIndex(ctx, "/profiles/runtime", "", reg.WorktreeID, reg.ProfileID, 4, false, false, func(result NativeIndexProgress) error { t.Logf("index result: %+v", result); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRuntimeQueryFixture(t *testing.T) {
	ctx := nativeRuntimeFixture(t)
	raw, err := os.ReadFile("/result/registration.json")
	if err != nil {
		t.Fatal(err)
	}
	var reg catalog.Registration
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	service, release, err := OpenNativeRetainedCore(ctx, "/profiles/runtime", "", reg.WorktreeID, reg.ProfileID, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	body, err := os.ReadFile("/target/main.go")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	request, err := json.Marshal(map[string]any{"scope": map[string]any{"worktree_id": reg.WorktreeID, "profile_id": reg.ProfileID}, "selector": map[string]any{"kind": "source_position", "path": "main.go", "content_hash": hex.EncodeToString(sum[:]), "byte_offset": strings.Index(string(body), "Canary")}, "task_kind": "explore", "freshness_mode": "analysis_snapshot", "max_output_bytes": 4096})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	result, err := service.Call(ctx, "mce4d_context_pack", request, app.CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 4096, DeliveryMode: "cli"})
	t.Logf("query elapsed %s", time.Since(start))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/result/query.json", raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("query result: %s", raw)
	if result.IsError {
		t.Fatal("query returned an error envelope; see owned result artifact")
	}
}
