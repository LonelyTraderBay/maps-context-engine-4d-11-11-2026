package contextpack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func budgetFixture(t *testing.T) (context.Context, BudgetInput) {
	t.Helper()
	ctx, in, p, refs := taskFixture(t)
	selected, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	s, err := BuildTaskSlice(ctx, SliceInput{Selection: selected, Policy: p, TaskKind: "behavior_change", Intent: "emoji 😀", Constraints: []string{"preserve_json_keys"}, RuleAnchors: refs[:2], VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }, MaxBytes: 4 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, BudgetInput{Slice: s, RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", PackToken: "fixture-pack-token", MaxOutputBytes: 65536, MaxItems: 512, DeliveryMode: "mcp_dual", IssueContinuation: func(_ context.Context, p CursorPosition) (string, bool, error) {
		return fmt.Sprintf("fixture-cursor-%d-%d-%d", p.NextItem, p.NextCoverageGap, p.NextWarningGap), p.NextItem > 0 || p.NextCoverageGap > 0 || p.NextWarningGap > 0, nil
	}}
}

func TestBudgetSerializesWholeWireItemsAndCountsDualBytes(t *testing.T) {
	ctx, in := budgetFixture(t)
	optional := TaskEvidence{ID: "optional-fixture", Kind: "constraint_excerpt", RequiredForReview: false, Why: "optional_fixture", Plane: "annotation", Trust: "fixture", SourceRefs: []model.SourceRef{}}
	in.Slice.report.Items = append([]TaskEvidence{optional}, in.Slice.report.Items...)
	page, err := BudgetTaskSlice(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Blocks) != 2 || !bytes.Equal(page.Blocks[0], page.Blocks[1]) || !json.Valid(page.Blocks[0]) || int64(len(page.Blocks[0])+len(page.Blocks[1])) != page.Response.Budget.SerializedBytes || page.Response.Budget.SerializedBytes > in.MaxOutputBytes {
		t.Fatalf("dual accounting incorrect: %+v", page.Response.Budget)
	}
	decoded, err := model.DecodeResponseJSON(page.Blocks[0], int(in.MaxOutputBytes), 4, "context_pack", false)
	if err != nil {
		t.Fatal(err)
	}
	pack, ok := decoded.Data.(model.PackData)
	if !ok || pack.EditPreconditionValidated || decoded.Readiness != "needs_context" || decoded.Freshness.Status != "not_checked" || decoded.Pagination.Truncated {
		t.Fatalf("bad complete snapshot label: %+v", decoded)
	}
	if len(pack.Items) == 0 || pack.Items[0].WhyIncluded != "target_source" || pack.Items[0].Content == nil || !strings.Contains(*pack.Items[0].Content, "😀") {
		t.Fatal("target source or Unicode damaged")
	}
	lastRequired := true
	validation := false
	for _, item := range pack.Items {
		if item.RequiredForReview && !lastRequired {
			t.Fatal("required item after optional item")
		}
		lastRequired = item.RequiredForReview
		if item.Kind == "validation_obligation" && item.Content != nil && strings.Contains(*item.Content, `"unknown"`) {
			validation = true
		}
	}
	if !validation {
		t.Fatal("unknown validation plan omitted")
	}
}

func TestBudgetReportsRequiredPendingWithAdvancingCursor(t *testing.T) {
	ctx, in := budgetFixture(t)
	in.MaxOutputBytes = 8192
	in.MaxItems = 1
	page, err := BudgetTaskSlice(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Response.Pagination.Truncated || page.Response.Pagination.ReturnedCount != 1 || page.Response.Pagination.RequiredPending == nil || *page.Response.Pagination.RequiredPending < 1 || page.Response.Pagination.NextCursor == nil || page.Next.NextItem != 1 {
		t.Fatalf("pending work hidden: %+v", page.Response.Pagination)
	}
	if model.ValidateDeliveredBytes(page.Response.Budget, page.Blocks) != nil {
		t.Fatal("byte cap not verified")
	}
	in.IssueContinuation = nil
	if _, err := BudgetTaskSlice(ctx, in); !errors.Is(err, ErrContinuationUnavailable) {
		t.Fatalf("unsigned continuation emitted: %v", err)
	}
	in.IssueContinuation = func(context.Context, CursorPosition) (string, bool, error) { return "non-advancing", false, nil }
	if _, err := BudgetTaskSlice(ctx, in); !errors.Is(err, ErrContinuationUnavailable) {
		t.Fatalf("non-advancing continuation emitted: %v", err)
	}
}

func TestBudgetGroupsSafetyGapsWithoutDroppingDetails(t *testing.T) {
	ctx, in := budgetFixture(t)
	in.MaxOutputBytes = 8192
	in.MaxItems = 1
	for i := 0; i < 75; i++ {
		in.Slice.report.Gaps = append(in.Slice.report.Gaps, model.Gap{Code: fmt.Sprintf("FIXTURE_GAP_%02d", i), Reason: strings.Repeat("safety ", 80), SourceRefs: []model.SourceRef{}})
	}
	total := len(in.Slice.report.Gaps)
	page, err := BudgetTaskSlice(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.PendingWarningGaps) == 0 || len(page.Response.Warnings) > 64 || page.Next.NextWarningGap+len(page.PendingWarningGaps) != total {
		t.Fatalf("gap detail count lost: %+v", page.Next)
	}
	found := false
	for _, gap := range page.Response.Warnings {
		if gap.Code == "GAP_DETAILS_PENDING" && gap.Count != nil && *gap.Count == int64(len(page.PendingWarningGaps)) {
			found = true
		}
	}
	if !found || page.Response.Pagination.RequiredPending == nil || *page.Response.Pagination.RequiredPending < int64(len(page.PendingWarningGaps)) {
		t.Fatal("grouped safety details not pending")
	}
	if page.Response.Pagination.ReturnedCount != 1 {
		t.Fatal("metadata grouping did not leave space for target")
	}
}

func TestBudgetHonorsPolicyAndStructuredHostProof(t *testing.T) {
	ctx, in := budgetFixture(t)
	in.MaxOutputBytes = 65537
	if _, err := BudgetTaskSlice(ctx, in); !errors.Is(err, ErrInput) {
		t.Fatalf("policy output cap bypassed: %v", err)
	}
	in.MaxOutputBytes = 65536
	in.DeliveryMode = "mcp_structured_verified"
	if _, err := BudgetTaskSlice(ctx, in); !errors.Is(err, ErrAuthority) {
		t.Fatalf("unverified structured host accepted: %v", err)
	}
	checks := 0
	in.VerifyStructuredHost = func(context.Context) error {
		checks++
		if checks > 1 {
			return errors.New("revoked")
		}
		return nil
	}
	if _, err := BudgetTaskSlice(ctx, in); !errors.Is(err, ErrAuthority) {
		t.Fatalf("revoked host accepted: %v", err)
	}
	in.VerifyStructuredHost = func(context.Context) error { return nil }
	page, err := BudgetTaskSlice(ctx, in)
	if err != nil || len(page.Blocks) != 1 || int64(len(page.Blocks[0])) != page.Response.Budget.SerializedBytes {
		t.Fatalf("verified structured accounting: %v", err)
	}
}

func TestBudgetUsesCompatibilityDefaults(t *testing.T) {
	ctx, in := budgetFixture(t)
	in.MaxOutputBytes = 0
	in.MaxItems = 0
	in.DeliveryMode = ""
	page, err := BudgetTaskSlice(ctx, in)
	if err != nil || page.Response.Budget.MaxOutputBytes != 24576 || page.Response.Budget.DeliveryMode != "mcp_dual" || len(page.Blocks) != 2 {
		t.Fatalf("incorrect defaults: %+v %v", page.Response.Budget, err)
	}
}

func TestBudgetDoesNotEmitSecretInScopeMetadata(t *testing.T) {
	ctx, in, p, refs := taskFixture(t)
	secret := "password=" + strings.Repeat("x", 20)
	in.Scope.Members[0].ServiceID = &secret
	var err error
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	slice, err := BuildTaskSlice(ctx, SliceInput{Selection: selected, Policy: p, TaskKind: "explore", RuleAnchors: refs[:2], MaxBytes: 4 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	_, err = BudgetTaskSlice(ctx, BudgetInput{Slice: slice, RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", PackToken: "fixture", MaxOutputBytes: 65536, MaxItems: 1, DeliveryMode: "mcp_dual", IssueContinuation: func(context.Context, CursorPosition) (string, bool, error) { return "fixture", true, nil }})
	if !errors.Is(err, policy.ErrSecretBlocked) {
		t.Fatalf("secret scope metadata was emitted: %v", err)
	}
}

func TestBudgetRejectsOversizedEnvelopeAndUnknownTraversal(t *testing.T) {
	ctx, in := budgetFixture(t)
	in.MaxOutputBytes = 4096
	for i := 0; i < 128; i++ {
		in.Slice.selection.report.Coverage.Capabilities = append(in.Slice.selection.report.Coverage.Capabilities, model.Capability{Name: fmt.Sprintf("cap_%03d", i), ProviderID: "provider", ProviderVersion: strings.Repeat("v", 128), Status: "partial"})
	}
	if _, err := BudgetTaskSlice(ctx, in); !errors.Is(err, ErrBudgetTooSmall) {
		t.Fatalf("oversized envelope accepted: %v", err)
	}
	errorResponse, blocks, err := BudgetErrorEnvelope(ctx, in, ErrBudgetTooSmall)
	if err != nil || errorResponse.Error == nil || errorResponse.Error.Code != "BUDGET_TOO_SMALL" || errorResponse.Data != nil || errorResponse.Scope != nil || model.ValidateDeliveredBytes(errorResponse.Budget, blocks) != nil {
		t.Fatalf("invalid bounded budget error: %+v %v", errorResponse, err)
	}
	ctx, in = budgetFixture(t)
	in.Slice.report.EnumerationComplete = false
	in.IssueContinuation = func(context.Context, CursorPosition) (string, bool, error) { return "cannot-progress", false, nil }
	if _, err := BudgetTaskSlice(ctx, in); !errors.Is(err, ErrContinuationUnavailable) {
		t.Fatalf("unknown traversal appeared complete: %v", err)
	}
	in.IssueContinuation = func(context.Context, CursorPosition) (string, bool, error) { return "fixture-more", true, nil }
	page, err := BudgetTaskSlice(ctx, in)
	if err != nil || page.Response.Pagination.RequiredPending != nil || page.Response.Pagination.OptionalPending != nil || page.Response.Pagination.NextCursor == nil {
		t.Fatalf("unknown pending count was fabricated: %+v %v", page.Response.Pagination, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := BudgetTaskSlice(cancelled, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestBudgetReturnsDescriptorForSourceAboveMaximum(t *testing.T) {
	ctx, in, p, refs := taskFixture(t)
	manifest := in.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	files := []snapshot.File{}
	for i := range manifest.Files {
		entry := &manifest.Files[i]
		file, ok := in.Snapshot.File(entry.Path)
		if !ok {
			t.Fatal("missing file")
		}
		if entry.Path == "a.go" {
			body := append(file.Bytes(), []byte("\n// "+strings.Repeat("😀", 10000))...)
			var err error
			file, err = snapshot.NewFile(entry.Path, body, 65536)
			if err != nil {
				t.Fatal(err)
			}
			hash, size := file.ContentHash(), int64(file.Len())
			entry.ContentHash = &hash
			entry.ByteLength = &size
		}
		files = append(files, file)
	}
	var err error
	in.Snapshot, err = snapshot.Freeze(manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	in.Scope.Members[0].SnapshotDigest = in.Snapshot.Manifest().SnapshotDigest
	for i := range in.Packages {
		in.Packages[i].InputDigest = in.Scope.Members[0].SnapshotDigest
	}
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	slice, err := BuildTaskSlice(ctx, SliceInput{Selection: selected, Policy: p, TaskKind: "behavior_change", RuleAnchors: refs[:2], VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }, MaxBytes: 4 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	_, err = BudgetTaskSlice(ctx, BudgetInput{Slice: slice, RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", PackToken: "fixture", MaxOutputBytes: 65536, MaxItems: 512, DeliveryMode: "mcp_dual", IssueContinuation: func(context.Context, CursorPosition) (string, bool, error) { return "fixture", true, nil }})
	var oversized *OversizedItemError
	if !errors.Is(err, ErrItemTooLarge) || !errors.As(err, &oversized) || len(oversized.Sources) != 1 || oversized.Sources[0].Path != "a.go" {
		t.Fatalf("missing safe oversized descriptor: %v", err)
	}
	file, _ := in.Snapshot.File("a.go")
	if oversized.Sources[0].Range.EndByte != int64(file.Len()) || file.CheckRange(int(oversized.Sources[0].Range.StartByte), int(oversized.Sources[0].Range.EndByte)) != nil {
		t.Fatal("oversized source descriptor cut a UTF-8 boundary")
	}
	budget := BudgetInput{Slice: slice, RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 65536, DeliveryMode: "mcp_dual"}
	errorResponse, blocks, e := BudgetErrorEnvelope(ctx, budget, err)
	if e != nil || errorResponse.Scope == nil || errorResponse.Error == nil || errorResponse.Error.Code != "RESULT_TOO_LARGE" || len(errorResponse.Error.Details) != 1 || len(errorResponse.Error.Details[0].SourceRefs) != 1 || model.ValidateDeliveredBytes(errorResponse.Budget, blocks) != nil {
		t.Fatalf("oversized error envelope: %+v %v", errorResponse, e)
	}
	if _, e := model.MatchPinnedSource(*errorResponse.Scope, errorResponse.Error.Details[0].SourceRefs[0], nil, 65536); e != nil {
		t.Fatalf("oversized descriptor escaped scope: %v", e)
	}
	forged := &OversizedItemError{ItemID: oversized.ItemID, Sources: append([]model.SourceRef{}, oversized.Sources...)}
	forged.Sources[0].Path = "RULES.md"
	if _, _, e := BudgetErrorEnvelope(ctx, budget, forged); !errors.Is(e, ErrInput) {
		t.Fatalf("forged descriptor accepted: %v", e)
	}
}

func TestBudgetHighFanoutKeepsRequiredPendingCount(t *testing.T) {
	ctx, in, p, refs := taskFixture(t)
	var body strings.Builder
	body.WriteString("package a\nfunc Fan() {\n")
	start := body.Len()
	for i := 0; i < 3001; i++ {
		body.WriteString("Same()\n")
	}
	body.WriteString("}\n")
	fan, err := snapshot.NewFile("fan.go", []byte(body.String()), 65536)
	if err != nil {
		t.Fatal(err)
	}
	manifest := in.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, size := fan.ContentHash(), int64(fan.Len())
	manifest.Files = append(manifest.Files, model.ManifestFile{Path: "fan.go", Status: "captured", ContentHash: &hash, ByteLength: &size})
	files := []snapshot.File{fan}
	for _, entry := range manifest.Files {
		if entry.Path != "fan.go" {
			f, ok := in.Snapshot.File(entry.Path)
			if !ok {
				t.Fatal("missing source")
			}
			files = append(files, f)
		}
	}
	in.Snapshot, err = snapshot.Freeze(manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	in.Scope.Members[0].SnapshotDigest = in.Snapshot.Manifest().SnapshotDigest
	for i := range in.Packages {
		in.Packages[i].InputDigest = in.Scope.Members[0].SnapshotDigest
	}
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	path, pkg := "fan.go", "pkg-a.go"
	at := int64(strings.Index(body.String(), "Fan"))
	in.Symbols = append(in.Symbols, model.SymbolFact{SymbolID: "fan", Path: &path, PackageKey: &pkg, Kind: "function", Name: "Fan", QualifiedName: "fixture.Fan", ScopeKey: "scope", Range: &model.ByteRange{StartByte: at, EndByte: at + 3}})
	for i := 0; i < 3001; i++ {
		target := "target"
		offset := int64(start + i*len("Same()\n"))
		in.Relations = append(in.Relations, model.RelationFact{RelationID: fmt.Sprintf("fan-%04d", i), SourceSymbolID: "fan", TargetSymbolID: &target, Kind: "REFERS_TO", EvidenceKind: "type_resolved", SitePath: "fan.go", Range: model.ByteRange{StartByte: offset, EndByte: offset + 4}})
	}
	in.MaxEdges = 4000
	in.MaxOutputBytes = 16 * 1024 * 1024
	selected, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	slice, err := BuildTaskSlice(ctx, SliceInput{Selection: selected, Policy: p, TaskKind: "behavior_change", RuleAnchors: refs[:2], VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }, MaxBytes: 16 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	input := BudgetInput{Slice: slice, RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", PackToken: "fixture", MaxOutputBytes: 65536, MaxItems: 512, DeliveryMode: "mcp_dual", IssueContinuation: func(_ context.Context, p CursorPosition) (string, bool, error) {
		return fmt.Sprintf("next-%d", p.NextItem), p.NextItem > 0, nil
	}}
	page, err := BudgetTaskSlice(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Response.Pagination.Truncated || !page.Response.Pagination.EnumerationComplete || page.Response.Pagination.RequiredPending == nil || *page.Response.Pagination.RequiredPending < 2900 || page.Response.Pagination.NextCursor == nil || page.Next.NextItem != int(page.Response.Pagination.ReturnedCount) {
		t.Fatalf("high fanout falsely complete: %+v", page.Response.Pagination)
	}
	if err := model.ValidateDeliveredBytes(page.Response.Budget, page.Blocks); err != nil {
		t.Fatal(err)
	}
	again, err := BudgetTaskSlice(ctx, input)
	if err != nil || !bytes.Equal(page.Blocks[0], again.Blocks[0]) {
		t.Fatal("budget result not deterministic")
	}
}
