//go:build linux

package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

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
