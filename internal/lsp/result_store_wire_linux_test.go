//go:build linux

package lsp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestRenameWirePagesCountDualPayload(t *testing.T) {
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 100)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	opts := RenameWireOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, EffectiveMaxOutput: 65536, DeliveryMode: "mcp_dual"}
	cursor := ""
	seen := 0
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("wire paging did not terminate")
		}
		response, blocks, err := store.ReadResultWirePage(ctx, codec, token, cursor, principal, resultCursorAllow, allowResult, opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(blocks) != 2 || !bytes.Equal(blocks[0], blocks[1]) || model.ValidateDeliveredBytes(response.Budget, blocks) != nil || response.Budget.TokenCount != nil {
			t.Fatal("wrong wire byte accounting")
		}
		decoded, err := model.DecodeResponseJSON(blocks[0], int(opts.MaxOutputBytes), 4, "rename_preview", false)
		if err != nil {
			t.Fatal(err)
		}
		page := decoded.Data.(model.RenameData)
		for _, edit := range page.Edits {
			if edit.SourceRef.Range.StartByte != int64(seen*2) {
				t.Fatal("wire page lost or repeated edit")
			}
			seen++
		}
		if page.Applied || !page.ValidationRequired || page.PreviewComplete != (response.Pagination.NextCursor == nil) {
			t.Fatal("unsafe preview completion")
		}
		if response.Pagination.NextCursor == nil {
			break
		}
		if *response.Pagination.NextCursor == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = *response.Pagination.NextCursor
	}
	if seen != 100 {
		t.Fatalf("lost edits: %d", seen)
	}
	opts.MaxOutputBytes = 4096
	if _, _, err = store.ReadResultWirePage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, opts); !errors.Is(err, contextpack.ErrBudgetTooSmall) {
		t.Fatalf("tiny envelope accepted: %v", err)
	}
	opts.MaxOutputBytes = 16384
	opts.DeliveryMode = "mcp_structured_verified"
	if _, _, err = store.ReadResultWirePage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, opts); !errors.Is(err, ErrResultInvalid) {
		t.Fatalf("unverified host accepted: %v", err)
	}
	calls := 0
	revoked := errors.New("host revoked")
	opts.VerifyStructuredHost = func(context.Context) error {
		calls++
		if calls == 2 {
			return revoked
		}
		return nil
	}
	if _, _, err = store.ReadResultWirePage(ctx, codec, token, "", principal, resultCursorAllow, allowResult, opts); !errors.Is(err, revoked) {
		t.Fatalf("host revocation ignored: %v", err)
	}
}
