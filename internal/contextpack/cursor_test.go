package contextpack

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func cursorFixture(t *testing.T) (context.Context, *CursorCodec, CursorBinding, SignedPosition, *time.Time) {
	t.Helper()
	ctx, in := fixture(t)
	key := [32]byte{1, 2, 3, 4}
	codec, err := NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	codec.now = func() time.Time { return now }
	binding := CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "v1", Scope: in.Scope}
	position := SignedPosition{NextItem: 1, NextCoverageGap: 2, NextWarningGap: 3, NextByteOffset: 4, LastKey: []string{"a", "b"}}
	return ctx, codec, binding, position, &now
}

func allowCursor(context.Context, CursorBinding) error { return nil }

func TestCursorAuthenticatesFullBindingAndExpires(t *testing.T) {
	ctx, codec, binding, position, now := cursorFixture(t)
	guardCalls := 0
	guard := func(_ context.Context, b CursorBinding) error {
		guardCalls++
		// The guard cannot mutate the returned claims through shared pointers.
		b.Scope.Members[0].PolicyEpoch++
		return nil
	}
	token, err := codec.Issue(ctx, binding, position, guard)
	if err != nil || guardCalls != 2 {
		t.Fatalf("issue guard calls=%d err=%v", guardCalls, err)
	}
	claims, err := codec.Verify(ctx, token, binding, guard)
	if err != nil || claims.Position.NextItem != 1 || claims.Binding.Scope.Members[0].PolicyEpoch != binding.Scope.Members[0].PolicyEpoch {
		t.Fatalf("verify failed or guard mutated claims: %v", err)
	}
	if err = codec.Recheck(ctx, claims, guard); err != nil {
		t.Fatal(err)
	}
	if _, err = codec.VerifyPrincipal(ctx, token, "list", binding.PrincipalDigest, guard); err != nil {
		t.Fatal(err)
	}
	if _, err = codec.VerifyPrincipal(ctx, token, "list", strings.Repeat("f", 64), guard); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("wrong authenticated principal accepted: %v", err)
	}
	if _, err = codec.VerifyPrincipal(ctx, token, "pack", binding.PrincipalDigest, guard); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("wrong purpose accepted: %v", err)
	}
	modified := claims
	modified.Position.NextItem++
	if err = codec.Recheck(ctx, modified, guard); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("modified verified claims accepted: %v", err)
	}
	forged := SignedCursor{Version: 1, KeyID: codec.keyID, Binding: binding, Position: position, IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	if err = codec.Recheck(ctx, forged, guard); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("unverified claims accepted: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*CursorBinding)
	}{
		{"purpose", func(b *CursorBinding) { b.Purpose = "pack" }},
		{"principal", func(b *CursorBinding) { b.PrincipalDigest = strings.Repeat("d", 64) }},
		{"allowed binding", func(b *CursorBinding) { b.BindingDigest = strings.Repeat("d", 64) }},
		{"request", func(b *CursorBinding) { b.RequestDigest = strings.Repeat("d", 64) }},
		{"order", func(b *CursorBinding) { b.OrderVersion = "v2" }},
		{"policy revision", func(b *CursorBinding) {
			b.Scope.Members = append([]model.ScopeMember{}, b.Scope.Members...)
			b.Scope.Members[0].PolicyEpoch++
			b.Scope.VectorDigest, _ = model.ComputeScopeVectorDigest(b.Scope, 65536)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := binding
			tc.change(&changed)
			if _, err := codec.Verify(ctx, token, changed, allowCursor); !errors.Is(err, ErrCursorInvalid) {
				t.Fatalf("cross-binding token accepted: %v", err)
			}
		})
	}
	parts := strings.Split(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)/2] ^= 1
	tampered := base64.RawURLEncoding.EncodeToString(payload) + "." + parts[1]
	if _, err := codec.Verify(ctx, tampered, binding, allowCursor); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("tampered token accepted: %v", err)
	}
	validJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	changedPosition := strings.Replace(string(validJSON), `"next_item":1`, `"next_item":2`, 1)
	if changedPosition == string(validJSON) {
		t.Fatal("fixture position was not found")
	}
	validTamper := base64.RawURLEncoding.EncodeToString([]byte(changedPosition)) + "." + parts[1]
	if _, err := codec.Verify(ctx, validTamper, binding, allowCursor); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("valid JSON with a forged position was accepted: %v", err)
	}
	other, _ := NewCursorCodec([32]byte{9})
	other.now = codec.now
	if _, err := other.Verify(ctx, token, binding, allowCursor); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("old key accepted: %v", err)
	}
	*now = now.Add(15 * time.Minute)
	if _, err := codec.Verify(ctx, token, binding, allowCursor); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired token accepted: %v", err)
	}
	if err := codec.Recheck(ctx, claims, allowCursor); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired page accepted: %v", err)
	}
}

func TestCursorGuardAndProgressFailClosed(t *testing.T) {
	ctx, codec, binding, position, _ := cursorFixture(t)
	if _, err := codec.Issue(ctx, binding, position, nil); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("missing authority guard accepted: %v", err)
	}
	revoked := errors.New("permission revoked")
	calls := 0
	if _, err := codec.Issue(ctx, binding, position, func(context.Context, CursorBinding) error {
		calls++
		if calls == 2 {
			return revoked
		}
		return nil
	}); !errors.Is(err, revoked) {
		t.Fatalf("post-sign revocation ignored: %v", err)
	}
	token, err := codec.Issue(ctx, binding, position, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Verify(ctx, token, binding, func(context.Context, CursorBinding) error { return revoked }); !errors.Is(err, revoked) {
		t.Fatalf("between-page revocation ignored: %v", err)
	}
	if _, err := codec.Verify(context.Background(), token, binding, allowCursor); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("unbounded guard accepted: %v", err)
	}
	prev := SignedPosition{NextItem: 2, NextByteOffset: 10, NextCoverageGap: 1, LastKey: []string{"a"}}
	if CursorAdvances(prev, prev) {
		t.Fatal("repeated cursor accepted")
	}
	next := prev
	next.NextByteOffset++
	if !CursorAdvances(prev, next) {
		t.Fatal("fragment progress rejected")
	}
	next.NextItem++
	next.NextByteOffset = 0
	if !CursorAdvances(prev, next) {
		t.Fatal("next item offset reset rejected")
	}
	next.NextCoverageGap = 0
	if CursorAdvances(prev, next) {
		t.Fatal("regressing gap accepted")
	}
	next = prev
	next.LastKey = []string{"0"}
	next.NextItem++
	if CursorAdvances(prev, next) {
		t.Fatal("regressing keyset accepted")
	}
	invalid := position
	invalid.LastKey = nil
	if _, err := codec.Issue(ctx, binding, invalid, allowCursor); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("invalid position accepted: %v", err)
	}
}

func TestAuthenticatedPreparationStillRequiresCurrentAuthorityAndTime(t *testing.T) {
	ctx, codec, binding, position, now := cursorFixture(t)
	token, err := codec.Issue(ctx, binding, position, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := codec.AuthenticateBound(ctx, token, binding)
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("authority revoked during private preparation")
	if err := codec.Recheck(ctx, claims, func(context.Context, CursorBinding) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("authenticated claims bypassed current authority")
	}
	modified := claims
	modified.Position.NextItem++
	if err := codec.Recheck(ctx, modified, allowCursor); !errors.Is(err, ErrCursorInvalid) {
		t.Fatal("prepared claims could be edited")
	}
	*now = now.Add(15 * time.Minute)
	if err := codec.Recheck(ctx, claims, allowCursor); !errors.Is(err, ErrCursorExpired) {
		t.Fatal("preparation extended the token lifetime")
	}
}
