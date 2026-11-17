package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func oversizedFixture(t *testing.T) (*freshnessFixture, *FreshnessPack, *oversizedEvidenceError, *contextpack.OversizedItemError) {
	t.Helper()
	f := newFreshnessFixture(t, "analysis_snapshot")
	pack, _, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	report, err := f.input.Slice.Read(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range report.Items {
		if len(candidate.SourceRefs) == 0 {
			continue
		}
		item := &contextpack.OversizedItemError{ItemID: candidate.ID, Sources: append([]model.SourceRef{}, candidate.SourceRefs...)}
		result := pack.describeOversized(f.ctx, item, "", f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions(), true)
		var proof *oversizedEvidenceError
		if errors.As(result, &proof) {
			return f, pack, proof, item
		}
	}
	t.Fatal("fixture has no source-backed first evidence item")
	return nil, nil, nil, nil
}

func TestOversizedDescriptorDeliveryAndBinding(t *testing.T) {
	f, _, proof, item := oversizedFixture(t)
	service, authority, backend, _ := coreServiceFixture(t)
	backend.err = proof
	result, err := service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError {
		t.Fatalf("verified descriptor was not delivered: %v", err)
	}
	response := decodeCoreResult(t, "mce4d_context_pack", result)
	if response.Error == nil || response.Error.Code != "RESULT_TOO_LARGE" || len(response.Error.Details) != 1 || response.Error.Details[0].Code != "EVIDENCE_ITEM_TOO_LARGE" || len(response.Error.Details[0].SourceRefs) != len(item.Sources) || response.Error.Details[0].SourceRefs[0] != item.Sources[0] || !strings.Contains(response.Error.Details[0].Reason, item.ItemID) || response.Scope == nil || response.Scope.VectorDigest != f.scope.VectorDigest || response.Scope.Members[0].RecordEpoch != f.scope.Members[0].RecordEpoch || response.Pagination.NextCursor != nil || !response.Pagination.Truncated {
		t.Fatal("verified descriptor lost identity, scope, or pending state")
	}
	if authority.responseChecks != 2 {
		t.Fatal("descriptor did not receive final authority checks")
	}

	request, err := model.DecodePackRequestJSON(f.input.RequestJSON, 65536)
	if err != nil {
		t.Fatal(err)
	}
	changed := "different request"
	request.Intent = &changed
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	result, err = service.Call(f.ctx, "mce4d_context_pack", raw, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "ACCESS_DENIED" || strings.Contains(string(result.Blocks[0]), item.Sources[0].Path) {
		t.Fatalf("descriptor replayed for another request: %v", err)
	}

	forged := *proof
	forged.sources = append([]model.SourceRef{}, proof.sources...)
	forged.sources[0].ContentHash = strings.Repeat("f", 64)
	backend.err = &forged
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "ACCESS_DENIED" || strings.Contains(string(result.Blocks[0]), item.Sources[0].Path) {
		t.Fatalf("mutated descriptor was delivered: %v", err)
	}

	backend.err = item // An arbitrary backend error cannot mint the sealed proof.
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || len(decodeCoreResult(t, "mce4d_context_pack", result).Error.Details) != 0 || strings.Contains(string(result.Blocks[0]), item.Sources[0].Path) {
		t.Fatalf("unverified backend descriptor leaked: %v", err)
	}
}

func TestOversizedDescriptorRevocationExpiryAndBudget(t *testing.T) {
	f, _, proof, item := oversizedFixture(t)
	service, authority, backend, _ := coreServiceFixture(t)
	backend.err = proof
	f.revoked = true
	result, err := service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "ACCESS_DENIED" || strings.Contains(string(result.Blocks[0]), item.Sources[0].Path) {
		t.Fatalf("revoked descriptor escaped: %v", err)
	}
	f.revoked = false
	backend.err, _ = GuardOversizedError(proof, func(context.Context) error { return contextpack.ErrPackExpired }, nil)
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "CURSOR_EXPIRED" || strings.Contains(string(result.Blocks[0]), item.Sources[0].Path) {
		t.Fatalf("expired descriptor escaped: %v", err)
	}
	backend.err = proof
	authority.revokeResponseAt = authority.responseChecks + 2
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "ACCESS_DENIED" || strings.Contains(string(result.Blocks[0]), item.Sources[0].Path) {
		t.Fatalf("late authority revocation leaked a descriptor: %v", err)
	}
	authority.revokeResponseAt = 0
	longItem := *item
	longItem.Sources = append([]model.SourceRef{}, item.Sources...)
	longItem.Sources[0].Path = strings.Repeat("x", 2400) + ".go"
	backend.err = newOversizedEvidence(f.ctx, &longItem, f.scope, proof.principal, proof.binding, proof.request, proof.token, "", true, 4096, 65536, func(context.Context) error { return nil })
	options := coreTestOptions()
	options.MaxOutputBytes = 4096
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, options)
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "BUDGET_TOO_SMALL" || strings.Contains(string(result.Blocks[0]), longItem.Sources[0].Path) {
		t.Fatalf("oversized descriptor escaped the budget: %v", err)
	}
}

func TestOversizedDescriptorContinuationBindsCursor(t *testing.T) {
	f, pack, _, _ := oversizedFixture(t)
	page, err := pack.First(f.ctx, freshnessOptions())
	if err != nil || page.Response.Pagination.NextCursor == nil {
		t.Fatalf("fixture needs a continuation: %v", err)
	}
	cursor := *page.Response.Pagination.NextCursor
	report, err := f.input.Slice.Read(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var proof *oversizedEvidenceError
	for _, candidate := range report.Items {
		item := &contextpack.OversizedItemError{ItemID: candidate.ID, Sources: append([]model.SourceRef{}, candidate.SourceRefs...)}
		result := pack.describeOversized(f.ctx, item, cursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions(), false)
		if errors.As(result, &proof) {
			break
		}
	}
	if proof == nil {
		t.Fatal("fixture did not expose its next complete evidence item")
	}
	service, _, backend, _ := coreServiceFixture(t)
	backend.err = proof
	request := model.ExpandRequest{PackToken: proof.token, NextCursor: cursor}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Call(f.ctx, "mce4d_expand_pack", raw, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_expand_pack", result).Error.Code != "RESULT_TOO_LARGE" {
		t.Fatalf("valid continuation descriptor failed: %v", err)
	}
	request.NextCursor += "x"
	raw, _ = json.Marshal(request)
	result, err = service.Call(f.ctx, "mce4d_expand_pack", raw, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_expand_pack", result).Error.Code != "ACCESS_DENIED" {
		t.Fatalf("descriptor was replayed with another cursor: %v", err)
	}
}

func TestOversizedDescriptorReleasesRetainedResult(t *testing.T) {
	f, _, proof, _ := oversizedFixture(t)
	service, _, backend, _ := coreServiceFixture(t)
	releases := 0
	backend.err, _ = GuardOversizedError(proof, func(context.Context) error { return nil }, func() { releases++ })
	result, err := service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "RESULT_TOO_LARGE" || releases != 1 {
		t.Fatalf("retained descriptor was not released: releases=%d err=%v", releases, err)
	}
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || releases != 1 {
		t.Fatalf("release was not idempotent: releases=%d err=%v", releases, err)
	}
	backend.err, _ = GuardOversizedError(proof, func(context.Context) error { return contextpack.ErrPackExpired }, func() { releases++ })
	result, err = service.Call(f.ctx, "mce4d_context_pack", f.input.RequestJSON, coreTestOptions())
	if err != nil || !result.IsError || decodeCoreResult(t, "mce4d_context_pack", result).Error.Code != "CURSOR_EXPIRED" || releases != 2 {
		t.Fatalf("failed delivery did not release retained result: releases=%d err=%v", releases, err)
	}
}
