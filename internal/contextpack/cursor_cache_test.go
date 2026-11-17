package contextpack

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPackCacheBoundsExpiryAndCurrentAuthority(t *testing.T) {
	ctx, slice, _, _ := fragmentFixture(t)
	codec, _ := NewCursorCodec([32]byte{9})
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	codec.now = func() time.Time { return now }
	cache, err := NewPackCache(1, 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	cache.now = func() time.Time { return now }
	pager, token, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Admit(ctx, pager); err != nil {
		t.Fatal(err)
	}
	if err = cache.Admit(ctx, pager); !errors.Is(err, ErrInput) {
		t.Fatalf("duplicate cache ID accepted: %v", err)
	}
	other, _, err := NewPagedPack(ctx, slice, codec, packCursorBinding(slice), allowCursor)
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Admit(ctx, other); !errors.Is(err, ErrLimit) {
		t.Fatalf("entry limit ignored: %v", err)
	}
	tooSmall, _ := NewPackCache(1, 1)
	if err = tooSmall.Admit(ctx, other); !errors.Is(err, ErrLimit) {
		t.Fatalf("materialization budget ignored: %v", err)
	}
	options := PackPageOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, MaxItems: 1, DeliveryMode: "mcp_dual"}
	first, err := pager.First(ctx, options)
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("missing cursor: %v", err)
	}
	cursor := *first.Response.Pagination.NextCursor
	if _, err = cache.Continue(ctx, pager.ID(), token, cursor, strings.Repeat("f", 64), allowCursor, options); !errors.Is(err, ErrAuthority) {
		t.Fatalf("wrong principal accepted: %v", err)
	}
	if _, err = cache.Continue(ctx, pager.ID(), token, cursor, packCursorBinding(slice).PrincipalDigest, allowCursor, options); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * time.Minute)
	if _, err = cache.Continue(ctx, pager.ID(), token, cursor, packCursorBinding(slice).PrincipalDigest, allowCursor, options); !errors.Is(err, ErrPackExpired) {
		t.Fatalf("expired materialization returned: %v", err)
	}
	if len(cache.entries) != 0 || cache.used != 0 {
		t.Fatal("expired cache entry retained")
	}
	if err = tooSmall.Admit(ctx, other); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expired token admitted: %v", err)
	}
}
