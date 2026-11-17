package overview

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestAliasCursorPagesAmbiguityWithoutLoss(t *testing.T) {
	ctx, in := aliasFixture(t)
	base := in.Declarations[0]
	in.Declarations = nil
	for i := range 101 {
		entry := base
		entry.Entity.EntityID = fmt.Sprintf("entity-%03d", i)
		in.Declarations = append(in.Declarations, entry)
	}
	valid := true
	in.VerifyAliasSet = func(context.Context, Scope, string) error {
		if !valid {
			return ErrAuthority
		}
		return nil
	}
	index, err := BuildAliasIndex(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	m := in.Snapshot.Manifest()
	member := model.ScopeMember{WorktreeID: worktree, ProfileID: m.ProfileID, GenerationID: generation, SnapshotDigest: m.SnapshotDigest, PolicyDigest: m.PolicyDigest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: generation, RecordsIncarnation: generation}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: generation, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	var key [32]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	binding := contextpack.CursorBinding{Purpose: "list", PrincipalDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), RequestDigest: strings.Repeat("c", 64), OrderVersion: "aliases-v1", Scope: scope}
	allow := func(context.Context, contextpack.CursorBinding) error { return nil }
	pager, err := NewAliasPager(ctx, index, "PaymentService", codec, binding, allow)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(pager.Anchor(), ".")[0])
	if err != nil || strings.Contains(string(raw), "PaymentService") {
		t.Fatal("query leaked into token")
	}
	cursor := ""
	seen := map[string]bool{}
	pages := 0
	var firstToken string
	for {
		page, err := pager.Page(ctx, cursor, binding.PrincipalDigest, allow, 20, 8192)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(page)
		if len(encoded) > 8192 || !page.Ambiguous || len(page.Candidates) == 0 {
			t.Fatal("budget, ambiguity or progress failed")
		}
		for _, candidate := range page.Candidates {
			if seen[candidate.Entity.EntityID] {
				t.Fatal("duplicate")
			}
			seen[candidate.Entity.EntityID] = true
		}
		pages++
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
		if firstToken == "" {
			firstToken = cursor
		}
		if pages > 101 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 101 || pages < 2 {
		t.Fatalf("lost candidates: %d", len(seen))
	}
	valid = false
	if page, err := pager.Page(ctx, firstToken, binding.PrincipalDigest, allow, 20, 8192); err == nil || len(page.Candidates) != 0 {
		t.Fatal("revoked set leaked")
	}
}
