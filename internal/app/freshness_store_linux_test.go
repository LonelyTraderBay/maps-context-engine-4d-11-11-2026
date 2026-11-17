//go:build linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type packBudgetFixture struct{}

func (packBudgetFixture) Check(context.Context) error { return nil }
func (packBudgetFixture) Close() error                { return nil }

func openFreshnessStoreFixture(t *testing.T, ctx context.Context, statePath, targetPath string) (*packstore.Store, *runtime.Lifecycle) {
	t.Helper()
	root, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	target, err := paths.OpenRoot(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := runtime.AcquireLifecycle(ctx, root, "11111111-1111-4111-8111-111111111111", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(); _ = target.Close(); _ = root.Close() })
	store, err := packstore.New(root, []*paths.Root{target}, lease, "11111111-1111-4111-8111-111111111111", 4*1024*1024, 8*1024*1024, 4,
		func(context.Context) error { return nil }, func(context.Context, int64) (packstore.BudgetReservation, error) { return packBudgetFixture{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	return store, lease
}

func TestFreshnessStoreCrossProcessContinuation(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	for _, name := range []string{state, target, filepath.Join(state, "scratch"), filepath.Join(state, "scratch", "pack")} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	canary := filepath.Join(target, "AGENTS.md")
	if err := os.WriteFile(canary, []byte("fixture canary"), 0600); err != nil {
		t.Fatal(err)
	}
	store, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	factory := func(_ context.Context, binding contextpack.CursorBinding) (FreshnessPackRestoreInput, error) {
		if model.ComparePinnedScope(binding.Scope, f.scope, 65536) != nil {
			return FreshnessPackRestoreInput{}, ErrValidationAuthority
		}
		return freshnessRestoreFixture(t, f, "", nil), nil
	}
	retention, err := NewSymbolPackRetention(store, f.input.Codec, 4*1024*1024, 60, factory)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := retention.Save(f.ctx, pack, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.RequestJSON, f.input.Guard)
	if err != nil || handle.Token() != token {
		t.Fatalf("persist before first page: %v", err)
	}
	first, err := handle.First(f.ctx, freshnessOptions())
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("missing continuation: %v", err)
	}
	cursor := *first.Response.Pagination.NextCursor
	expected, err := handle.Continue(f.ctx, cursor, freshnessOptions())
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(expected.Response.Data.(model.PackData).Items)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.input.Codec.VerifyPrincipal(f.ctx, token, "pack", f.input.Binding.PrincipalDigest, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(ctx context.Context, raw []byte) error {
		in := freshnessRestoreFixture(t, f, token, nil)
		restored, err := RestoreFreshnessEnvelope(ctx, raw, in, 4*1024*1024)
		if err != nil {
			return err
		}
		return restored.RecheckRetained(ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard)
	}
	cmd := exec.CommandContext(f.ctx, os.Args[0], "-test.run=^TestFreshnessStoreChild$")
	cmd.Env = append(os.Environ(), "MCE4D_FRESH_STORE_STATE="+state, "MCE4D_FRESH_STORE_TARGET="+target,
		"MCE4D_FRESH_STORE_TOKEN="+token, "MCE4D_FRESH_STORE_CURSOR="+cursor, "MCE4D_FRESH_STORE_ID="+claims.Binding.ResultID)
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Equal(out, want) {
		t.Fatalf("stored pack failed in another process: %v %q", err, out)
	}
	f.revoked = true
	if raw, err := store.Read(f.ctx, claims.Binding.ResultID, verify); !errors.Is(err, packstore.ErrUnsafe) || raw != nil {
		t.Fatalf("revoked pack returned by store: %v", err)
	}
	after, err := os.ReadFile(canary)
	if err != nil || string(after) != "fixture canary" {
		t.Fatal("target fixture was modified")
	}
}

func TestFreshnessStoreChild(t *testing.T) {
	state, target := os.Getenv("MCE4D_FRESH_STORE_STATE"), os.Getenv("MCE4D_FRESH_STORE_TARGET")
	if state == "" || target == "" {
		return
	}
	f := newFreshnessFixture(t, "checked_workspace")
	store, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
	token, cursor, id := os.Getenv("MCE4D_FRESH_STORE_TOKEN"), os.Getenv("MCE4D_FRESH_STORE_CURSOR"), os.Getenv("MCE4D_FRESH_STORE_ID")
	factory := func(_ context.Context, binding contextpack.CursorBinding) (FreshnessPackRestoreInput, error) {
		if model.ComparePinnedScope(binding.Scope, f.scope, 65536) != nil {
			return FreshnessPackRestoreInput{}, ErrValidationAuthority
		}
		return freshnessRestoreFixture(t, f, "", nil), nil
	}
	retention, err := NewSymbolPackRetention(store, f.input.Codec, 4*1024*1024, 60, factory)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.input.Codec.VerifyPrincipal(f.ctx, token, "pack", f.input.Binding.PrincipalDigest, f.input.Guard)
	if err != nil || claims.Binding.ResultID != id {
		t.Fatalf("result id mismatch: %v", err)
	}
	restored, err := retention.Get(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	page, err := restored.Continue(f.ctx, cursor,
		contextpack.PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "mcp_dual"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page.Response.Data.(model.PackData).Items)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.Write(raw); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestFreshnessStoreGCAuthenticatesExpiredEnvelope(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	for _, name := range []string{state, target, filepath.Join(state, "scratch"), filepath.Join(state, "scratch", "pack")} {
		if err := os.Mkdir(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store, lifecycle := openFreshnessStoreFixture(t, f.ctx, state, target)
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := pack.ArchiveEnvelope(f.ctx, f.input.PrincipalID, f.input.RequestJSON, 2, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.input.Codec.VerifyPrincipal(f.ctx, token, "pack", envelope.PrincipalDigest, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	id := claims.Binding.ResultID
	if expired, err := ProveExpiredPackEnvelope(f.ctx, id, envelope.Bytes, f.input.Codec, 4*1024*1024); err != nil || expired {
		t.Fatalf("live result was considered expired: %v", err)
	}
	if _, err := ProveExpiredPackEnvelope(f.ctx, "22222222-2222-4222-8222-222222222222", envelope.Bytes, f.input.Codec, 4*1024*1024); !errors.Is(err, ErrValidationAuthority) {
		t.Fatalf("wrong filename accepted as ownership: %v", err)
	}
	var modified packEnvelope
	if err := json.Unmarshal(envelope.Bytes, &modified); err != nil {
		t.Fatal(err)
	}
	modified.Request = bytes.Replace(modified.Request, []byte("analysis_snapshot"), []byte("checked_workspace"), 1)
	changed, _ := json.Marshal(modified)
	if _, err := ProveExpiredPackEnvelope(f.ctx, id, changed, f.input.Codec, 4*1024*1024); !errors.Is(err, ErrValidationAuthority) {
		t.Fatalf("unbound request accepted as owned: %v", err)
	}
	verify := func(ctx context.Context, raw []byte) error {
		in := freshnessRestoreFixture(t, f, token, nil)
		_, err := RestoreFreshnessEnvelope(ctx, raw, in, 4*1024*1024)
		return err
	}
	if err := store.Create(f.ctx, id, envelope.Bytes, verify); err != nil {
		t.Fatal(err)
	}
	job := "44444444-4444-4444-8444-444444444444"
	writer, err := runtime.AcquireWriter(f.ctx, lifecycle, job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	proof := func(ctx context.Context, id string, raw []byte) (bool, error) {
		return ProveExpiredPackEnvelope(ctx, id, raw, f.input.Codec, 4*1024*1024)
	}
	if count, err := store.GCExpired(f.ctx, writer, job, proof); err != nil || count != 0 {
		t.Fatalf("live result removed: count=%d err=%v", count, err)
	}
	if wait := time.Until(time.Unix(claims.IssuedAt+2, 0)) + 20*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if count, err := store.GCExpired(f.ctx, writer, job, proof); err != nil || count != 1 {
		t.Fatalf("signed expired result not removed: count=%d err=%v", count, err)
	}
	if raw, err := store.Read(f.ctx, id, verify); !errors.Is(err, packstore.ErrMissing) || raw != nil {
		t.Fatalf("removed result restored: %v", err)
	}
}
