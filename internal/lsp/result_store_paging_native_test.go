//go:build linux || windows

package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
		if os.Getenv("MCE4D_RESULT_NEW_INCARNATION") == "1" {
			live, _, _ := resultFixture(t, 0)
			live.Members[0].IndexIncarnation = "33333333-3333-4333-8333-333333333333"
			live.VectorDigest, err = model.ComputeScopeVectorDigest(live, 65536)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			guard := func(_ context.Context, claims contextpack.CursorBinding) error {
				called = true
				return model.ComparePinnedScope(live, claims.Scope, 65536)
			}
			if h, err := store.ValidateResultToken(ctx, codec, token, principal, guard, allowResult); err == nil || h.ResultID != "" || !called {
				t.Fatalf("validated a result after replacing its index incarnation: %v", err)
			}
			return
		}
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
	replaced := exec.CommandContext(ctxChild, os.Args[0], "-test.run=^TestResultPagesCrossProcess$")
	replaced.Env = append(cmd.Env, "MCE4D_RESULT_NEW_INCARNATION=1")
	if out, err := replaced.CombinedOutput(); err != nil {
		t.Fatalf("replacement scope accepted an old token: %v %s", err, out)
	}
}
