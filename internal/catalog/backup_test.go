package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBackupRevisionsObserveCurrentHeadsWithoutMutation(t *testing.T) {
	ctx, store, db, profile, _ := bindingFixture(t)
	var before int64
	if err := db.QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	all, err := store.ReadBackupRevisions(ctx, "", func(context.Context) error { return nil })
	if err != nil || len(all.Worktrees) != 1 || len(all.Worktrees[0].Profiles) != 1 || all.Worktrees[0].Profiles[0].ProfileID != profile || all.Worktrees[0].PolicyDigest == nil || all.Worktrees[0].PolicyEpoch == nil {
		t.Fatalf("metadata: %+v %v", all, err)
	}
	one, err := store.ReadBackupRevisions(ctx, fixtureWorktree, func(context.Context) error { return nil })
	if err != nil || !reflect.DeepEqual(one, all) {
		t.Fatalf("scope metadata: %+v %v", one, err)
	}
	denied := errors.New("revoked backup scope")
	calls := 0
	got, err := store.ReadBackupRevisions(ctx, fixtureWorktree, func(context.Context) error {
		calls++
		if calls > 1 {
			return denied
		}
		return nil
	})
	if !errors.Is(err, denied) || got.CatalogIncarnation != "" || len(got.Worktrees) != 0 {
		t.Fatalf("revocation returned metadata: %+v %v", got, err)
	}
	var after int64
	if err := db.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil || before != after {
		t.Fatalf("read mutated catalog: %d -> %d %v", before, after, err)
	}
}
