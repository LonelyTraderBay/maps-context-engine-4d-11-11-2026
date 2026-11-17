package overview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func aliasFixture(t *testing.T) (context.Context, AliasInput) {
	t.Helper()
	ctx, plan := planFixture(t, false)
	file, _ := plan.Snapshot.File("spec.md")
	text := string(file.Bytes())
	entry := func(alias, id string) AliasDeclaration {
		start := strings.Index(text, alias)
		return AliasDeclaration{Alias: alias, Entity: model.EntityRef{Kind: "entity", GraphIdentity: plan.Overview.scope.Identity, EntityID: id}, Source: model.SourceRef{WorktreeID: worktree, ProfileID: plan.Overview.scope.Identity.ProfileID, GenerationID: generation, Path: "spec.md", ContentHash: file.ContentHash(), Range: model.ByteRange{StartByte: int64(start), EndByte: int64(start + len(alias))}}}
	}
	return ctx, AliasInput{Overview: plan.Overview, Snapshot: plan.Snapshot, Scanner: plan.Scanner, Declarations: []AliasDeclaration{entry("PaymentService", "billing"), entry("PaymentService", "orders"), entry("số dư khả dụng", "balance"), entry("ຍອດເງິນ", "balance")}, VerifyAliasSet: func(context.Context, Scope, string) error { return nil }, AuthorizeAlias: func(context.Context, Scope, AliasDeclaration) error { return nil }, MaxBytes: 65536}
}

func TestAliasesKeepAmbiguityAndSourceAnchors(t *testing.T) {
	ctx, in := aliasFixture(t)
	index, err := BuildAliasIndex(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	result, err := index.Lookup(ctx, "paymentservice")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ambiguous || len(result.Candidates) != 2 || result.Candidates[0].Entity.EntityID != "billing" || result.Candidates[1].Entity.EntityID != "orders" {
		t.Fatalf("ambiguity lost: %+v", result)
	}
	for _, candidate := range result.Candidates {
		if len(candidate.SourceRefs) != 1 || candidate.SourceRefs[0].Path != "spec.md" {
			t.Fatal("alias source missing")
		}
	}
	for _, alias := range []string{"số dư khả dụng", "ຍອດເງິນ"} {
		result, err = index.Lookup(ctx, alias)
		if err != nil || result.Ambiguous || len(result.Candidates) != 1 || result.Candidates[0].Entity.EntityID != "balance" {
			t.Fatalf("multilingual alias: %+v %v", result, err)
		}
	}
	result, err = index.Lookup(ctx, "unknown")
	if err != nil || result.Ambiguous || len(result.Candidates) != 0 {
		t.Fatal("invented candidate")
	}
}

func TestAliasesRejectChangedApprovalAndMismatchedEvidence(t *testing.T) {
	ctx, in := aliasFixture(t)
	approved := true
	in.VerifyAliasSet = func(context.Context, Scope, string) error {
		if !approved {
			return errors.New("revoked private")
		}
		return nil
	}
	index, err := BuildAliasIndex(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	approved = false
	if result, err := index.Lookup(ctx, "PaymentService"); !errors.Is(err, ErrAuthority) || len(result.Candidates) != 0 {
		t.Fatal("revoked alias leaked")
	}
	approved = true
	bad := in
	bad.Declarations = append([]AliasDeclaration{}, in.Declarations...)
	bad.Declarations[0].Alias = "InventedService"
	if _, err = BuildAliasIndex(ctx, bad); err == nil {
		t.Fatal("alias without source accepted")
	}
	bad = in
	bad.Declarations = append([]AliasDeclaration{}, in.Declarations...)
	bad.Declarations[0].Entity.GenerationID = "22222222-2222-4222-8222-222222222222"
	if _, err = BuildAliasIndex(ctx, bad); err == nil {
		t.Fatal("cross-generation entity accepted")
	}
	bad = in
	bad.Declarations = append([]AliasDeclaration{}, in.Declarations...)
	bad.Declarations[0].Source.ContentHash = strings.Repeat("f", 64)
	if _, err = BuildAliasIndex(ctx, bad); err == nil {
		t.Fatal("stale alias source accepted")
	}
}

func TestAliasesNeverSilentlyTruncateAmbiguity(t *testing.T) {
	ctx, in := aliasFixture(t)
	base := in.Declarations[0]
	in.Declarations = nil
	for i := range 101 {
		entry := base
		entry.Entity.EntityID = fmt.Sprintf("entity-%03d", i)
		in.Declarations = append(in.Declarations, entry)
	}
	index, err := BuildAliasIndex(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := index.Lookup(ctx, "PaymentService"); !errors.Is(err, ErrLimit) || len(result.Candidates) != 0 {
		t.Fatal("oversized ambiguous result was truncated")
	}
}

func TestAliasesAuthorizeBeforeReadingSource(t *testing.T) {
	ctx, in := aliasFixture(t)
	in.Declarations = append([]AliasDeclaration{}, in.Declarations...)
	in.Declarations[0].Source.ContentHash = strings.Repeat("f", 64)
	in.AuthorizeAlias = func(context.Context, Scope, AliasDeclaration) error { return errors.New("denied") }
	if _, err := BuildAliasIndex(ctx, in); !errors.Is(err, ErrAuthority) {
		t.Fatalf("source was checked before approval: %v", err)
	}
}
