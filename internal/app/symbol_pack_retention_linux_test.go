//go:build linux

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func symbolRetentionFixture(t *testing.T, ttl int64) (*freshnessFixture, *SymbolPackRetention, *FreshnessPack, *runtime.Lifecycle, string) {
	t.Helper()
	f := newFreshnessFixture(t, "analysis_snapshot")
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	for _, path := range []string{state, target, filepath.Join(state, "scratch"), filepath.Join(state, "scratch", "pack")} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store, lifecycle := openFreshnessStoreFixture(t, f.ctx, state, target)
	factory := func(_ context.Context, binding contextpack.CursorBinding) (FreshnessPackRestoreInput, error) {
		if model.ComparePinnedScope(binding.Scope, f.scope, 65536) != nil {
			return FreshnessPackRestoreInput{}, ErrValidationAuthority
		}
		return freshnessRestoreFixture(t, f, "", nil), nil
	}
	retention, err := NewSymbolPackRetention(store, f.input.Codec, 4*1024*1024, ttl, factory)
	if err != nil {
		t.Fatal(err)
	}
	pack, _, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	return f, retention, pack, lifecycle, state
}

func TestSymbolRetentionSaveGetAndRevoke(t *testing.T) {
	f, retention, original, _, _ := symbolRetentionFixture(t, 60)
	load := retention.restore
	loads := 0
	retention.restore = func(ctx context.Context, binding contextpack.CursorBinding) (FreshnessPackRestoreInput, error) {
		loads++
		return load(ctx, binding)
	}
	handle, err := retention.Save(f.ctx, original, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.RequestJSON, f.input.Guard)
	if err != nil || handle.Token() == "" || loads != 1 {
		t.Fatalf("save or redundant pinned snapshot loads: loads=%d err=%v", loads, err)
	}
	first, err := handle.First(f.ctx, freshnessOptions())
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("missing first-page cursor: %v", err)
	}
	cursor := *first.Response.Pagination.NextCursor
	reopened, err := retention.Get(f.ctx, handle.Token(), f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	want, err := original.Continue(f.ctx, handle.Token(), cursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions())
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Continue(f.ctx, cursor, freshnessOptions())
	if err != nil || !reflect.DeepEqual(got.Response.Data.(model.PackData).Items, want.Response.Data.(model.PackData).Items) {
		t.Fatalf("reopened different result: %v", err)
	}
	if wrong, err := retention.Get(f.ctx, handle.Token(), f.input.Binding.PrincipalDigest, "wrong actor", f.input.Guard); err == nil || wrong != nil {
		t.Fatal("different actor restored a pack")
	}
	f.revoked = true
	if page, err := reopened.Continue(f.ctx, cursor, freshnessOptions()); err == nil || len(page.Blocks) != 0 {
		t.Fatalf("revoked continuation returned bytes: %v", err)
	}
}

func TestSymbolRetentionDoesNotReturnTokenWhenQuotaIsFull(t *testing.T) {
	f, retention, original, _, _ := symbolRetentionFixture(t, 60)
	store, ok := retention.store.(*packstore.Store)
	if !ok {
		t.Fatal("fixture store type")
	}
	for _, id := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"} {
		if err := store.Create(f.ctx, id, []byte("fixture"), func(context.Context, []byte) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	handle, err := retention.Save(f.ctx, original, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.RequestJSON, f.input.Guard)
	if !errors.Is(err, packstore.ErrCapacity) || handle != nil {
		t.Fatalf("full quota returned a handle: %v", err)
	}
}

func TestSymbolRetentionRefusesDeliveryAfterEviction(t *testing.T) {
	f, retention, original, _, state := symbolRetentionFixture(t, 60)
	handle, err := retention.Save(f.ctx, original, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.RequestJSON, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.input.Codec.VerifyPrincipal(f.ctx, handle.Token(), "pack", f.input.Binding.PrincipalDigest, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "scratch", "pack", claims.Binding.ResultID+".pack")
	var once sync.Once
	var removeErr error
	options := freshnessOptions()
	options.DeliveryMode = "mcp_structured_verified"
	options.VerifyStructuredHost = func(context.Context) error {
		once.Do(func() { removeErr = os.Remove(path) })
		return removeErr
	}
	page, err := handle.First(f.ctx, options)
	if removeErr != nil || !errors.Is(err, packstore.ErrMissing) || len(page.Blocks) != 0 {
		t.Fatalf("evicted result delivered: remove=%v err=%v", removeErr, err)
	}
}

func TestSymbolRetentionExpiryAndAuthorizedGC(t *testing.T) {
	f, retention, original, lifecycle, state := symbolRetentionFixture(t, 2)
	handle, err := retention.Save(f.ctx, original, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.RequestJSON, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.input.Codec.VerifyPrincipal(f.ctx, handle.Token(), "pack", f.input.Binding.PrincipalDigest, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	job := "44444444-4444-4444-8444-444444444444"
	writer, err := runtime.AcquireWriter(f.ctx, lifecycle, job, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if count, err := retention.GCExpired(f.ctx, writer, job); err != nil || count != 0 {
		t.Fatalf("live result collected: count=%d err=%v", count, err)
	}
	if wait := time.Until(time.Unix(claims.IssuedAt+2, 0)) + 20*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	// Expiry may be observed by the token guard before storage is opened, or
	// by the storage verifier. Both must refuse pre-edit validation.
	if result, err := handle.Validate(f.ctx); (!errors.Is(err, packstore.ErrMissing) && !errors.Is(err, contextpack.ErrPackExpired)) || result.Data.EditPreconditionValidated {
		t.Fatalf("expired retained validation succeeded: %v", err)
	}
	info, err := os.Stat(filepath.Join(state, "scratch", "pack", claims.Binding.ResultID+".pack"))
	if err != nil || info.Size() <= 1024 {
		t.Fatalf("fixture does not exceed tightened result limit: %v", err)
	}
	retention.max = 1024
	if count, err := retention.GCExpired(f.ctx, writer, job); err != nil || count != 1 {
		t.Fatalf("expired result not collected: count=%d err=%v", count, err)
	}
}

type retentionBoundaryFixture struct {
	SymbolPackStorage
	afterCreate func()
	afterRead   func()
}

func (s *retentionBoundaryFixture) Create(ctx context.Context, id string, raw []byte, verify func(context.Context, []byte) error) error {
	err := s.SymbolPackStorage.Create(ctx, id, raw, verify)
	if err == nil && s.afterCreate != nil {
		s.afterCreate()
	}
	return err
}

func (s *retentionBoundaryFixture) Read(ctx context.Context, id string, verify func(context.Context, []byte) error) ([]byte, error) {
	raw, err := s.SymbolPackStorage.Read(ctx, id, verify)
	if err == nil && s.afterRead != nil {
		s.afterRead()
	}
	return raw, err
}

func TestSymbolRetentionRechecksPathsAfterVerifiedStorageBytes(t *testing.T) {
	for _, boundary := range []string{"publication", "readback"} {
		t.Run(boundary, func(t *testing.T) {
			f, retention, original, _, _ := symbolRetentionFixture(t, 60)
			storage := &retentionBoundaryFixture{SymbolPackStorage: retention.store}
			retention.store = storage
			if boundary == "publication" {
				storage.afterCreate = func() { f.denied = true }
			}
			handle, err := retention.Save(f.ctx, original, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.RequestJSON, f.input.Guard)
			if boundary == "publication" {
				if err == nil || handle != nil {
					t.Fatal("revoked path received a usable saved handle")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// Token/scope guards remain valid; only the source path is revoked
			// after the store's final byte-verifier callback has succeeded.
			storage.afterRead = func() { f.denied = true }
			if page, err := handle.First(f.ctx, freshnessOptions()); err == nil || len(page.Blocks) != 0 {
				t.Fatal("path revoked after storage verification received a page")
			}
		})
	}
}
