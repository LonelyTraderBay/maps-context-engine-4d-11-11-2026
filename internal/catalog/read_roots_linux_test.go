//go:build linux

package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestBindingVerifiesActualGitRootsAndLinkedPointers(t *testing.T) {
	repo, linked := gitFixture(t, true)
	root, linkedRoot := openFixtureRoot(t, repo), openFixtureRoot(t, linked)
	common, err := fsruntime.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := leasedCatalogDB(t)
	base, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	profile, policy := body(t, profileFixture()), body(t, policyFixture(filepath.Dir(repo)))
	mainReg, err := base.Register(ctx, root, common, "", "default", policy, profile)
	if err != nil {
		t.Fatal(err)
	}
	linkedReg, err := base.Register(ctx, linkedRoot, common, "worktrees/linked", "default", policy, profile)
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL: %q %v", mode, err)
	}
	store, err := OpenExistingLeased(ctx, db, &lifecycleFixture{mode: ports.SharedLease})
	if err != nil {
		t.Fatal(err)
	}
	main, err := store.ReadBinding(ctx, mainReg.WorktreeID, mainReg.ProfileID, bindingAllowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := main.VerifyRoots(root, common, ""); err != nil {
		t.Fatal(err)
	}
	if err := main.VerifyRoots(linkedRoot, common, "worktrees/linked"); !errors.Is(err, ErrRootRebindRequired) {
		t.Fatalf("different root accepted: %v", err)
	}
	linkedBinding, err := store.ReadBinding(ctx, linkedReg.WorktreeID, linkedReg.ProfileID, bindingAllowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := linkedBinding.VerifyRoots(linkedRoot, common, "worktrees/linked"); err != nil {
		t.Fatal(err)
	}
	if err := linkedBinding.VerifyRoots(linkedRoot, common, "worktrees/not-linked"); err == nil {
		t.Fatal("wrong linked metadata accepted")
	}
	linkedBinding.Policy.MaxFileBytes++
	if err := linkedBinding.VerifyRoots(linkedRoot, common, "worktrees/linked"); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("mutated policy accepted: %v", err)
	}
}
