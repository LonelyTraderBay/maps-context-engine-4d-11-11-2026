//go:build linux

package lsp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
)

func TestValidateResultTokenRechecksAndDoesNotExtendExpiry(t *testing.T) {
	store, _ := makeResultStore(t, resultFixtureDir(t), 0)
	scope, payload, principal := resultFixture(t, 3)
	now := time.Now()
	store.now = func() time.Time { return now }
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	now = now.Add(10 * time.Minute)
	verified, err := store.ValidateResultToken(ctx, codec, token, principal, resultCursorAllow, allowResult)
	if err != nil || verified.ExpiresAt != header.ExpiresAt {
		t.Fatalf("validation changed expiry: %+v %v", verified, err)
	}
	now = now.Add(5 * time.Minute)
	if _, err := store.ValidateResultToken(ctx, codec, token, principal, resultCursorAllow, allowResult); !errors.Is(err, ErrResultExpired) {
		t.Fatalf("expired result accepted: %v", err)
	}
}

func TestValidateResultTokenCatchesLateRevoke(t *testing.T) {
	store, _ := makeResultStore(t, resultFixtureDir(t), 0)
	scope, payload, principal := resultFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	allowed := true
	checks := 0
	cursorGuard := func(context.Context, contextpack.CursorBinding) error {
		checks++
		if checks == 2 {
			allowed = false
		}
		return nil
	}
	resultGuard := func(context.Context, RenameStoredResult) error {
		if !allowed {
			return ErrLSPAuthority
		}
		return nil
	}
	if _, err := store.ValidateResultToken(ctx, codec, token, principal, cursorGuard, resultGuard); !errors.Is(err, ErrLSPAuthority) {
		t.Fatalf("late revoke accepted: %v", err)
	}
}

func TestValidateResultTokenRechecksCursorAfterResultGuard(t *testing.T) {
	store, _ := makeResultStore(t, resultFixtureDir(t), 0)
	scope, payload, principal := resultFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	allowed, checks := true, 0
	resultGuard := func(context.Context, RenameStoredResult) error {
		checks++
		if checks == 3 {
			allowed = false
		}
		return nil
	}
	cursorGuard := func(context.Context, contextpack.CursorBinding) error {
		if !allowed {
			return ErrLSPAuthority
		}
		return nil
	}
	if _, err := store.ValidateResultToken(ctx, codec, token, principal, cursorGuard, resultGuard); !errors.Is(err, ErrLSPAuthority) {
		t.Fatalf("late cursor revoke accepted: %v", err)
	}
}
