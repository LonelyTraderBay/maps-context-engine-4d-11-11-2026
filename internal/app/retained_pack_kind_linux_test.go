//go:build linux

package app

import (
	"context"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// faultyRetentionStore models broken storage-owner callback behavior, not a
// production storage implementation.
type faultyRetentionStore struct {
	SymbolPackStorage
	skipReadVerification bool
	changeReturnedBytes  bool
	skipCreate           bool
}

func (s faultyRetentionStore) Read(ctx context.Context, id string, verify func(context.Context, []byte) error) ([]byte, error) {
	raw, err := s.SymbolPackStorage.Read(ctx, id, func(ctx context.Context, data []byte) error {
		if s.skipReadVerification {
			return nil
		}
		return verify(ctx, data)
	})
	if err == nil && s.changeReturnedBytes {
		raw = append(raw, 0)
	}
	return raw, err
}

func (s faultyRetentionStore) Create(ctx context.Context, id string, raw []byte, verify func(context.Context, []byte) error) error {
	if s.skipCreate {
		return nil
	}
	return s.SymbolPackStorage.Create(ctx, id, raw, verify)
}

func TestIdentifyRetainedSymbolAndStorageVerification(t *testing.T) {
	f, retention, original, _, _ := symbolRetentionFixture(t, 60)
	handle, err := retention.Save(f.ctx, original, f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.RequestJSON, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	store := retention.store
	identify := func(s SymbolPackStorage, token, principal, actor string) (string, error) {
		return IdentifyRetainedPack(f.ctx, s, f.input.Codec, token, principal, actor, f.input.Guard, 4*1024*1024)
	}
	if kind, err := identify(store, handle.Token(), f.input.Binding.PrincipalDigest, f.input.PrincipalID); err != nil || kind != "symbol" {
		t.Fatalf("symbol classification: kind=%q err=%v", kind, err)
	}
	for _, tc := range []struct{ token, principal, actor string }{
		{handle.Token() + "x", f.input.Binding.PrincipalDigest, f.input.PrincipalID},
		{handle.Token(), "wrong principal", f.input.PrincipalID},
		{handle.Token(), f.input.Binding.PrincipalDigest, "wrong actor"},
	} {
		if kind, err := identify(store, tc.token, tc.principal, tc.actor); err == nil || kind != "" {
			t.Fatal("invalid token, principal or actor was classified")
		}
	}
	for _, faulty := range []faultyRetentionStore{
		{SymbolPackStorage: store, skipReadVerification: true},
		{SymbolPackStorage: store, changeReturnedBytes: true},
	} {
		if kind, err := identify(faulty, handle.Token(), f.input.Binding.PrincipalDigest, f.input.PrincipalID); err == nil || kind != "" {
			t.Fatal("classification trusted inconsistent store bytes")
		}
		retention.store = faulty
		if restored, err := retention.Get(f.ctx, handle.Token(), f.input.Binding.PrincipalDigest, f.input.PrincipalID, f.input.Guard); err == nil || restored != nil {
			t.Fatal("restore trusted inconsistent store bytes")
		}
		if err := handle.Recheck(f.ctx); err == nil {
			t.Fatal("delivery trusted inconsistent store bytes")
		}
	}
	retention.store = store
}

func TestIdentifyRetainedPlanAndStorageVerification(t *testing.T) {
	f := newPlanRetentionFixture(t)
	state, target := planRetentionDirs(t)
	store, _ := openFreshnessStoreFixture(t, f.ctx, state, target)
	loads := 0
	factory := func(ctx context.Context, binding contextpack.CursorBinding, request model.PackRequest, revision string) (FreshnessPlanRestoreInput, error) {
		loads++
		return f.factory(ctx, binding, request, revision)
	}
	retention, err := NewPlanPackRetention(store, f.codec, 4*1024*1024, 60, factory)
	if err != nil {
		t.Fatal(err)
	}
	retention.store = faultyRetentionStore{SymbolPackStorage: store, skipCreate: true}
	if handle, err := retention.Save(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, f.guard); err == nil || handle != nil {
		t.Fatal("unverified publication returned a handle")
	}
	retention.store = store
	handle, err := retention.Save(f.ctx, f.pack, f.binding.PrincipalDigest, "fixture-actor", f.request, f.guard)
	if err != nil || loads != 1 {
		t.Fatalf("save must restore once: loads=%d err=%v", loads, err)
	}
	identify := func(s SymbolPackStorage, token, principal string) (string, error) {
		return IdentifyRetainedPack(f.ctx, s, f.codec, token, principal, "fixture-actor", f.guard, 4*1024*1024)
	}
	if kind, err := identify(store, handle.Token(), f.binding.PrincipalDigest); err != nil || kind != "plan" {
		t.Fatalf("plan classification: kind=%q err=%v", kind, err)
	}
	if kind, err := identify(store, handle.Token()+"x", f.binding.PrincipalDigest); err == nil || kind != "" {
		t.Fatal("invalid plan token was classified")
	}
	if kind, err := identify(store, handle.Token(), "wrong principal"); err == nil || kind != "" {
		t.Fatal("different principal was classified")
	}
	if loads != 1 {
		t.Fatalf("classification loaded a graph: %d", loads)
	}
	for _, faulty := range []faultyRetentionStore{
		{SymbolPackStorage: store, skipReadVerification: true},
		{SymbolPackStorage: store, changeReturnedBytes: true},
	} {
		if kind, err := identify(faulty, handle.Token(), f.binding.PrincipalDigest); err == nil || kind != "" {
			t.Fatal("classification trusted inconsistent store bytes")
		}
		retention.store = faulty
		if restored, err := retention.Get(f.ctx, handle.Token(), f.binding.PrincipalDigest, "fixture-actor", f.guard); err == nil || restored != nil {
			t.Fatal("restore trusted inconsistent store bytes")
		}
		if err := handle.Recheck(f.ctx); err == nil {
			t.Fatal("delivery trusted inconsistent store bytes")
		}
	}
	retention.store = store
	before := loads
	if restored, err := retention.Get(f.ctx, handle.Token(), f.binding.PrincipalDigest, "wrong actor", f.guard); err == nil || restored != nil {
		t.Fatal("different actor restored retained plan")
	}
	if loads != before {
		t.Fatalf("invalid restore loaded a graph: before=%d after=%d", before, loads)
	}
}
