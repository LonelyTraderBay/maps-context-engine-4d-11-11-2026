//go:build linux

package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func resultCursorAllow(context.Context, contextpack.CursorBinding) error { return nil }

func TestResultPagesCrossProcess(t *testing.T) {
	codec, err := contextpack.NewCursorCodec([32]byte{8})
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("MCE4D_RESULT_PAGING_FIXTURE"); dir != "" {
		store, _ := makeResultStore(t, dir, 0)
		_, _, principal := resultFixture(t, 0)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cursor := os.Getenv("MCE4D_RESULT_CURSOR")
		token := os.Getenv("MCE4D_RESULT_TOKEN")
		expected, err := strconv.Atoi(os.Getenv("MCE4D_RESULT_NEXT_OFFSET"))
		if err != nil {
			t.Fatal(err)
		}
		digest := os.Getenv("MCE4D_RESULT_DIGEST")
		count := 0
		for pages := 0; ; pages++ {
			if pages > 100 {
				t.Fatal("rename cursor loop")
			}
			page, err := store.ReadResultPage(ctx, codec, token, cursor, principal, resultCursorAllow, allowResult, 128, 32768)
			if err != nil {
				t.Fatal(err)
			}
			for _, edit := range page.Data.Edits {
				if edit.SourceRef.Range.StartByte != int64(expected) {
					t.Fatal("edit missing, repeated or reordered")
				}
				expected += 2
				count++
			}
			if page.Data.Applied || !page.Data.ValidationRequired || !page.Data.ComputationComplete || page.Data.ResultDigest != digest || page.Data.PreviewComplete != (page.NextCursor == nil) {
				t.Fatal("invalid preview state")
			}
			if page.NextCursor == nil {
				break
			}
			if *page.NextCursor == cursor {
				t.Fatal("cursor did not advance")
			}
			cursor = *page.NextCursor
		}
		fmt.Printf("delivered=%d final_offset=%d\n", count, expected)
		return
	}
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 1025)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	header, err := store.Create(ctx, scope, principal, payload, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "rename-result", PrincipalDigest: principal, BindingDigest: strings.Repeat("c", 64), RequestDigest: strings.Repeat("d", 64), OrderVersion: "rename-v1", Scope: scope, ResultID: header.ResultID, ResultDigest: header.ResultDigest}
	token, err := store.IssueResultToken(ctx, codec, binding, resultCursorAllow, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ReadResultPage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, 128, 32768)
	if err != nil || first.NextCursor == nil || first.Data.PreviewComplete {
		t.Fatalf("invalid initial page: %v", err)
	}
	editBytes, _ := json.Marshal(first.Data.Edits)
	conflictBytes, _ := json.Marshal(first.Data.Conflicts)
	if len(editBytes)+len(conflictBytes) != first.SerializedItemBytes || first.SerializedItemBytes > 32768 {
		t.Fatal("wrong item byte accounting")
	}
	for i, e := range first.Data.Edits {
		if e.SourceRef.Range.StartByte != int64(i*2) {
			t.Fatal("first page order incorrect")
		}
	}
	if _, err = store.ReadResultPage(ctx, codec, token, "", strings.Repeat("f", 64), resultCursorAllow, allowResult, 128, 32768); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("wrong principal accepted: %v", err)
	}
	if _, err = store.ReadResultPage(ctx, codec, token, token, principal, resultCursorAllow, allowResult, 128, 32768); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("result token accepted as page cursor: %v", err)
	}
	if _, err = store.ReadResultPage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, 128, 4); !errors.Is(err, ErrResultTooLarge) {
		t.Fatalf("nonprogressing oversized page accepted: %v", err)
	}
	ctxChild, cancelChild := context.WithTimeout(ctx, 20*time.Second)
	defer cancelChild()
	cmd := exec.CommandContext(ctxChild, os.Args[0], "-test.run=^TestResultPagesCrossProcess$")
	cmd.Env = append(os.Environ(), "MCE4D_RESULT_PAGING_FIXTURE="+dir, "MCE4D_RESULT_TOKEN="+token, "MCE4D_RESULT_CURSOR="+*first.NextCursor, "MCE4D_RESULT_NEXT_OFFSET="+strconv.Itoa(len(first.Data.Edits)*2), "MCE4D_RESULT_DIGEST="+header.ResultDigest)
	out, err := cmd.CombinedOutput()
	want := fmt.Sprintf("delivered=%d final_offset=2050", 1025-len(first.Data.Edits))
	if err != nil || !strings.Contains(string(out), want) {
		t.Fatalf("cross-process paging failed: %v %s", err, out)
	}
}

func TestResultPageDoesNotExtendExpiryAndRechecksPermission(t *testing.T) {
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	header, err := store.Create(ctx, scope, principal, payload, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := contextpack.NewCursorCodec([32]byte{8})
	binding := contextpack.CursorBinding{Purpose: "rename-result", PrincipalDigest: principal, BindingDigest: strings.Repeat("c", 64), RequestDigest: strings.Repeat("d", 64), OrderVersion: "rename-v1", Scope: scope, ResultID: header.ResultID, ResultDigest: header.ResultDigest}
	token, err := store.IssueResultToken(ctx, codec, binding, resultCursorAllow, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ReadResultPage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, 1, 32768)
	if err != nil || first.NextCursor == nil {
		t.Fatalf("missing page: %v", err)
	}
	revoked := errors.New("revoked")
	if _, err = store.ReadResultPage(ctx, codec, token, *first.NextCursor, principal, func(context.Context, contextpack.CursorBinding) error { return revoked }, allowResult, 1, 32768); !errors.Is(err, revoked) {
		t.Fatalf("revocation ignored: %v", err)
	}
	store.now = func() time.Time { return time.Unix(header.ExpiresAt, 0) }
	if _, err = store.ReadResultPage(ctx, codec, token, *first.NextCursor, principal, resultCursorAllow, allowResult, 1, 32768); !errors.Is(err, ErrResultExpired) {
		t.Fatalf("spool TTL extended: %v", err)
	}
}

func TestRenamePreviewRemainsIncompleteUntilConflictsDelivered(t *testing.T) {
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 1)
	for i := 0; i < 130; i++ {
		payload.Conflicts = append(payload.Conflicts, model.Gap{Code: fmt.Sprintf("CONFLICT_%03d", i), Reason: "Review conflict", SourceRefs: []model.SourceRef{}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	header, err := store.Create(ctx, scope, principal, payload, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := contextpack.NewCursorCodec([32]byte{8})
	binding := contextpack.CursorBinding{Purpose: "rename-result", PrincipalDigest: principal, BindingDigest: strings.Repeat("c", 64), RequestDigest: strings.Repeat("d", 64), OrderVersion: "rename-v1", Scope: scope, ResultID: header.ResultID, ResultDigest: header.ResultDigest}
	token, err := store.IssueResultToken(ctx, codec, binding, resultCursorAllow, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ReadResultPage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, 512, 131072)
	if err != nil || first.RemainingEdits != 0 || first.RemainingConflicts != 2 || first.Data.PreviewComplete || first.NextCursor == nil {
		t.Fatalf("pending conflicts were hidden: %v", err)
	}
	last, err := store.ReadResultPage(ctx, codec, token, *first.NextCursor, principal, resultCursorAllow, allowResult, 512, 131072)
	if err != nil || !last.Data.PreviewComplete || last.NextCursor != nil || len(last.Data.Conflicts) != 2 {
		t.Fatalf("final conflict page incorrect: %v", err)
	}
}

func TestEvictedResultDoesNotRecompute(t *testing.T) {
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	header, err := store.Create(ctx, scope, principal, payload, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := contextpack.NewCursorCodec([32]byte{8})
	binding := contextpack.CursorBinding{Purpose: "rename-result", PrincipalDigest: principal, BindingDigest: strings.Repeat("c", 64), RequestDigest: strings.Repeat("d", 64), OrderVersion: "rename-v1", Scope: scope, ResultID: header.ResultID, ResultDigest: header.ResultDigest}
	token, err := store.IssueResultToken(ctx, codec, binding, resultCursorAllow, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ReadResultPage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, 1, 32768)
	if err != nil || first.NextCursor == nil {
		t.Fatalf("missing first cursor: %v", err)
	}
	file := filepath.Join(dir, "scratch", "rename", header.ResultID+".json")
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReadResultPage(ctx, codec, token, *first.NextCursor, principal, resultCursorAllow, allowResult, 1, 32768); !errors.Is(err, ErrResultExpired) {
		t.Fatalf("evicted result resumed: %v", err)
	}
	if _, err = os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("evicted result was recreated")
	}
}

func TestResultPageRechecksCursorAfterResultGuard(t *testing.T) {
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	header, err := store.Create(ctx, scope, principal, payload, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := contextpack.NewCursorCodec([32]byte{8})
	binding := contextpack.CursorBinding{Purpose: "rename-result", PrincipalDigest: principal, BindingDigest: strings.Repeat("c", 64), RequestDigest: strings.Repeat("d", 64), OrderVersion: "rename-v1", Scope: scope, ResultID: header.ResultID, ResultDigest: header.ResultDigest}
	token, err := store.IssueResultToken(ctx, codec, binding, resultCursorAllow, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	revoked := false
	denied := errors.New("cursor authority revoked")
	calls := 0
	cursorGuard := func(context.Context, contextpack.CursorBinding) error {
		if revoked {
			return denied
		}
		return nil
	}
	resultGuard := func(context.Context, RenameStoredResult) error {
		calls++
		if calls == 3 {
			revoked = true
		}
		return nil
	}
	if _, err = store.ReadResultPage(ctx, codec, token, "", principal, cursorGuard, resultGuard, 1, 32768); !errors.Is(err, denied) || calls != 3 {
		t.Fatalf("late cursor revocation ignored: calls=%d err=%v", calls, err)
	}
}
