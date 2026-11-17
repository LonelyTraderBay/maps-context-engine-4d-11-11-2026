package contextpack

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func planMaterializationFixture(t *testing.T) (context.Context, *CursorCodec, CursorBinding, string, string, *time.Time) {
	t.Helper()
	ctx, codec, binding, _, now := cursorFixture(t)
	binding.OrderVersion = "pack-v1"
	binding.ResultID = "22222222-2222-4222-8222-222222222222"
	binding.ResultDigest = strings.Repeat("d", 64)
	list, err := codec.Issue(ctx, binding, SignedPosition{LastKey: []string{}}, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	binding.Purpose = "pack"
	pack, err := codec.Issue(ctx, binding, SignedPosition{LastKey: []string{}}, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, codec, binding, pack, list, now
}

func TestPlanMaterializationBindsTokensAuthorityAndLifetime(t *testing.T) {
	ctx, codec, binding, pack, list, now := planMaterializationFixture(t)
	payload := []byte(`{"plan":"fixture only"}`)
	raw, err := codec.SealPlanMaterialization(ctx, pack, list, binding.PrincipalDigest, payload, 5, 0, 65536, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := codec.OpenPlanMaterialization(ctx, raw, pack, binding.PrincipalDigest, 65536, allowCursor)
	if err != nil || !bytes.Equal(opened.Payload, payload) || opened.ListToken != list || opened.ExpiresAt != now.Unix()+5 {
		t.Fatalf("archive did not preserve materialization: %v", err)
	}
	if err := opened.Recheck(ctx, allowCursor); err != nil {
		t.Fatal(err)
	}
	forged := opened
	forged.ExpiresAt++
	if err := forged.Recheck(ctx, allowCursor); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("modified expiry was accepted: %v", err)
	}
	opened.Payload[0] ^= 1
	again, err := codec.OpenPlanMaterialization(ctx, raw, pack, binding.PrincipalDigest, 65536, allowCursor)
	if err != nil || !bytes.Equal(again.Payload, payload) {
		t.Fatal("opened bytes alias retained materialization")
	}
	shortened, err := codec.SealPlanMaterialization(ctx, pack, list, binding.PrincipalDigest, payload, 900, opened.ExpiresAt, 65536, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	inspect, err := codec.InspectPlanMaterialization(shortened, 65536)
	if err != nil || inspect.ResultID != binding.ResultID || inspect.Expired || inspect.ExpiresAt != opened.ExpiresAt {
		t.Fatalf("rearchive extended lifetime: %+v %v", inspect, err)
	}
	if _, err := codec.OpenPlanMaterialization(ctx, raw, pack, strings.Repeat("f", 64), 65536, allowCursor); err == nil {
		t.Fatal("other principal opened materialization")
	}
	if _, err := codec.OpenPlanMaterialization(ctx, raw, list, binding.PrincipalDigest, 65536, allowCursor); err == nil {
		t.Fatal("wrong pack token opened materialization")
	}
	denied := func(context.Context, CursorBinding) error { return errors.New("private authorization detail") }
	if got, err := codec.OpenPlanMaterialization(ctx, raw, pack, binding.PrincipalDigest, 65536, denied); err == nil || len(got.Payload) != 0 {
		t.Fatal("revoked authority received plan bytes")
	}
	tampered := bytes.Clone(raw)
	tampered[len(tampered)-2] ^= 1
	if _, err := codec.InspectPlanMaterialization(tampered, 65536); err == nil {
		t.Fatal("tampered archive could prove expiry")
	}
	if _, err := codec.OpenPlanMaterialization(ctx, tampered, pack, binding.PrincipalDigest, 65536, allowCursor); err == nil {
		t.Fatal("tampered archive was opened")
	}
	other, _ := NewCursorCodec([32]byte{9})
	other.now = codec.now
	if _, err := other.InspectPlanMaterialization(raw, 65536); err == nil {
		t.Fatal("rotated key proved ownership")
	}
	if _, err := codec.InspectPackArchive(raw, 65536); err == nil {
		t.Fatal("plan materialization was accepted as a symbol pack")
	}
	*now = now.Add(6 * time.Second)
	if err := opened.Recheck(ctx, allowCursor); !errors.Is(err, ErrPackExpired) {
		t.Fatalf("old retained handle remained usable: %v", err)
	}
	if got, err := codec.OpenPlanMaterialization(ctx, raw, pack, binding.PrincipalDigest, 65536, allowCursor); !errors.Is(err, ErrPackExpired) || len(got.Payload) != 0 {
		t.Fatalf("expired archive was returned: %v", err)
	}
	inspect, err = codec.InspectPlanMaterialization(raw, 65536)
	if err != nil || !inspect.Expired {
		t.Fatalf("expired signed archive could not be proved: %v", err)
	}
	*now = now.Add(16 * time.Minute)
	if inspect, err = codec.InspectPlanMaterialization(raw, 65536); err != nil || !inspect.Expired {
		t.Fatalf("expired token could not prove GC ownership: %v", err)
	}
}

func TestPlanMaterializationRejectsMismatchedAndNonAnchorCursors(t *testing.T) {
	ctx, codec, binding, pack, list, _ := planMaterializationFixture(t)
	other := binding
	other.Purpose = "list"
	other.RequestDigest = strings.Repeat("e", 64)
	foreign, err := codec.Issue(ctx, other, SignedPosition{LastKey: []string{}}, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.SealPlanMaterialization(ctx, pack, foreign, binding.PrincipalDigest, []byte("x"), 30, 0, 65536, allowCursor); err == nil {
		t.Fatal("different request was paired with pack token")
	}
	other.RequestDigest = binding.RequestDigest
	nonAnchor, err := codec.Issue(ctx, other, SignedPosition{NextItem: 1, LastKey: []string{}}, allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.SealPlanMaterialization(ctx, pack, nonAnchor, binding.PrincipalDigest, []byte("x"), 30, 0, 65536, allowCursor); err == nil {
		t.Fatal("continuation cursor was accepted as initial anchor")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := codec.SealPlanMaterialization(canceled, pack, list, binding.PrincipalDigest, []byte("x"), 30, 0, 65536, allowCursor); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
}
