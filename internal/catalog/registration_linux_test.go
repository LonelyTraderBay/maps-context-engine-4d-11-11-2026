//go:build linux

package catalog

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func gitFixture(t *testing.T, createLinked bool) (string, string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	template := filepath.Join(base, "empty-template")
	for _, path := range []string{repo, template} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, output)
		}
	}
	run("init", "--template="+template, repo)
	if err := os.WriteFile(filepath.Join(repo, "file.go"), []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := ""
	if createLinked {
		run("-C", repo, "add", "file.go")
		run("-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
		linked = filepath.Join(base, "linked")
		run("-C", repo, "worktree", "add", "-b", "linked", linked)
	}
	// The canary is repository data; registration must never execute it.
	marker := filepath.Join(base, "must-not-execute")
	canary := filepath.Join(base, "fsmonitor.sh")
	if err := os.WriteFile(canary, []byte("#!/bin/sh\nprintf executed > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	run("-C", repo, "config", "core.fsmonitor", canary)
	t.Cleanup(func() {
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("fixture fsmonitor ran: %v", err)
		}
	})
	return repo, linked
}

func openFixtureRoot(t *testing.T, path string) *paths.Root {
	t.Helper()
	root, err := paths.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func TestRegisterEmptyAndLinkedWorktreesAtomically(t *testing.T) {
	repo, linked := gitFixture(t, true)
	root := openFixtureRoot(t, repo)
	common, err := fsruntime.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	linkedRoot := openFixtureRoot(t, linked)
	db := testDB(t)
	store, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	profile := body(t, profileFixture())
	policy := body(t, policyFixture(filepath.Dir(repo)))
	first, err := store.Register(context.Background(), root, common, "", "default", policy, profile)
	if err != nil || !validUUID(first.RepoID) || !validUUID(first.WorktreeID) || first.BindingRevision != 1 || first.PolicyEpoch != 1 || first.ProfileRevision != 1 {
		t.Fatalf("main registration: %+v %v", first, err)
	}
	second, err := store.Register(context.Background(), linkedRoot, common, "worktrees/linked", "default", policy, profile)
	if err != nil || second.RepoID != first.RepoID || second.WorktreeID == first.WorktreeID {
		t.Fatalf("linked registration: %+v %v", second, err)
	}
	if _, err := store.Register(context.Background(), root, common, "", "default", policy, profile); !errors.Is(err, ErrRegistrationExists) {
		t.Fatalf("duplicate registration accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE worktrees SET filesystem_identity='different-fixture-identity' WHERE worktree_id=?`, first.WorktreeID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Register(context.Background(), root, common, "", "default", policy, profile); !errors.Is(err, ErrRootRebindRequired) {
		t.Fatalf("changed worktree identity was not flagged: %v", err)
	}
	for table, want := range map[string]int{"repositories": 1, "worktrees": 2, "policies": 2, "profiles": 2, "worktree_policy_heads": 2, "profile_heads": 2} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s: %d %v", table, count, err)
		}
	}
	if revision, err := store.Remove(context.Background(), first.WorktreeID, 1, true); err != nil || revision != 2 {
		t.Fatalf("remove registered fixture: %d %v", revision, err)
	}
	for _, path := range []string{filepath.Join(repo, "file.go"), filepath.Join(linked, "file.go")} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "package fixture\n" {
			t.Fatalf("source changed: %v", err)
		}
	}
}

func TestRegisterUncommittedRepoAndRejectInvalidInputs(t *testing.T) {
	repo, _ := gitFixture(t, false)
	other, _ := gitFixture(t, false)
	root := openFixtureRoot(t, repo)
	otherRoot := openFixtureRoot(t, other)
	common, err := fsruntime.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	otherCommon, err := fsruntime.OpenLocalGitMetadata(otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer otherCommon.Close()
	db := testDB(t)
	store, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	profile := body(t, profileFixture())
	policy := body(t, policyFixture(repo))
	if _, err := store.Register(context.Background(), root, otherCommon, "", "default", policy, profile); !errors.Is(err, ErrInvalidGitMetadata) {
		t.Fatalf("unrelated metadata accepted: %v", err)
	}
	if _, err := db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Register(context.Background(), root, otherCommon, "", "default", policy, profile); !errors.Is(err, ErrInvalidGitMetadata) {
		t.Fatalf("unrelated metadata was not rejected before writes: %v", err)
	}
	if _, err := db.Exec(`PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Register(context.Background(), root, common, "", "default", body(t, policyFixture(other)), profile); !errors.Is(err, ErrRootNotAllowed) {
		t.Fatalf("outside policy accepted: %v", err)
	}
	if _, err := store.Register(context.Background(), root, common, "", "default", policy, []byte(`{"language":"go"}`)); err == nil {
		t.Fatal("invalid profile accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM repositories`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed registration left repository: %d %v", count, err)
	}
	if _, err := store.Register(context.Background(), root, common, "", "default", policy, profile); err != nil {
		t.Fatalf("uncommitted repository rejected: %v", err)
	}
}

func TestRebindMovedWorktreeWithoutChangingSource(t *testing.T) {
	repo, _ := gitFixture(t, false)
	root := openFixtureRoot(t, repo)
	common, err := fsruntime.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	db := testDB(t)
	store, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	policy := body(t, policyFixture(filepath.Dir(repo)))
	registration, err := store.Register(context.Background(), root, common, "", "default", policy, body(t, profileFixture()))
	if err != nil {
		t.Fatal(err)
	}
	// Release the old handles before moving the fixture. DrvFS can expose
	// transient rename behavior while directory handles remain open.
	if err := common.Close(); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(filepath.Dir(repo), "moved")
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	movedRoot := openFixtureRoot(t, moved)
	movedCommon, err := fsruntime.OpenLocalGitMetadata(movedRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer movedCommon.Close()
	if _, err := store.Rebind(context.Background(), registration.WorktreeID, 1, false, movedRoot, movedCommon, ""); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed rebind accepted: %v", err)
	}
	if _, err := store.Rebind(context.Background(), registration.WorktreeID, 2, true, movedRoot, movedCommon, ""); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale rebind accepted: %v", err)
	}
	other, _ := gitFixture(t, false)
	otherRoot := openFixtureRoot(t, other)
	otherCommon, err := fsruntime.OpenLocalGitMetadata(otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer otherCommon.Close()
	if _, err := store.Rebind(context.Background(), registration.WorktreeID, 1, true, otherRoot, otherCommon, ""); !errors.Is(err, ErrRootNotAllowed) {
		t.Fatalf("outside-policy clone was accepted: %v", err)
	}
	revision, err := store.Rebind(context.Background(), registration.WorktreeID, 1, true, movedRoot, movedCommon, "")
	if err != nil || revision != 2 {
		t.Fatalf("rebind failed: %d %v", revision, err)
	}
	var storedRoot, storedCommon, repoID string
	var storedRevision int64
	if err := db.QueryRow(`SELECT w.canonical_root,w.binding_revision,r.common_dir,w.repo_id FROM worktrees w JOIN repositories r ON r.repo_id=w.repo_id WHERE w.worktree_id=?`, registration.WorktreeID).Scan(&storedRoot, &storedRevision, &storedCommon, &repoID); err != nil {
		t.Fatal(err)
	}
	if storedRoot != moved || storedCommon != filepath.Join(moved, ".git") || storedRevision != 2 || repoID != registration.RepoID {
		t.Fatalf("incorrect rebind state: %q %q %d", storedRoot, storedCommon, storedRevision)
	}
	if data, err := os.ReadFile(filepath.Join(moved, "file.go")); err != nil || string(data) != "package fixture\n" {
		t.Fatalf("source changed: %v", err)
	}
}

func TestRebindExplicitlyUsesDistinctRepoIdentity(t *testing.T) {
	repo, _ := gitFixture(t, false)
	other, _ := gitFixture(t, false)
	root := openFixtureRoot(t, repo)
	otherRoot := openFixtureRoot(t, other)
	common, err := fsruntime.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	otherCommon, err := fsruntime.OpenLocalGitMetadata(otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer otherCommon.Close()
	db := testDB(t)
	store, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	allowed := policyFixture(filepath.Dir(repo))
	allowed.AllowedRoots = []string{filepath.Dir(repo), filepath.Dir(other)}
	registration, err := store.Register(context.Background(), root, common, "", "default", body(t, allowed), body(t, profileFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rebind(context.Background(), registration.WorktreeID, 1, false, otherRoot, otherCommon, ""); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed clone rebind accepted: %v", err)
	}
	revision, err := store.Rebind(context.Background(), registration.WorktreeID, 1, true, otherRoot, otherCommon, "")
	if err != nil || revision != 2 {
		t.Fatalf("explicit clone rebind: %d %v", revision, err)
	}
	var repoID, storedRoot string
	if err := db.QueryRow(`SELECT repo_id,canonical_root FROM worktrees WHERE worktree_id=?`, registration.WorktreeID).Scan(&repoID, &storedRoot); err != nil {
		t.Fatal(err)
	}
	if !validUUID(repoID) || repoID == registration.RepoID || storedRoot != other {
		t.Fatalf("clone identity or root was reused: %q %q", repoID, storedRoot)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM repositories`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("repository history changed: %d %v", count, err)
	}
	for _, source := range []string{filepath.Join(repo, "file.go"), filepath.Join(other, "file.go")} {
		if data, err := os.ReadFile(source); err != nil || string(data) != "package fixture\n" {
			t.Fatalf("source changed: %v", err)
		}
	}
}

func TestRebindRejectsCommonPathIdentityCollision(t *testing.T) {
	repo, _ := gitFixture(t, false)
	other, _ := gitFixture(t, false)
	root := openFixtureRoot(t, repo)
	otherRoot := openFixtureRoot(t, other)
	common, err := fsruntime.OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer common.Close()
	otherCommon, err := fsruntime.OpenLocalGitMetadata(otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer otherCommon.Close()
	db := testDB(t)
	store, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	allowed := policyFixture(filepath.Dir(repo))
	allowed.AllowedRoots = []string{filepath.Dir(repo), filepath.Dir(other)}
	registration, err := store.Register(context.Background(), root, common, "", "default", body(t, allowed), body(t, profileFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,?,?,?)`, "55555555-5555-4555-8555-555555555555", filepath.Join(other, ".git"), "old-fixture-identity", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rebind(context.Background(), registration.WorktreeID, 1, true, otherRoot, otherCommon, ""); !errors.Is(err, ErrRootRebindRequired) {
		t.Fatalf("identity collision accepted: %v", err)
	}
	var storedRoot, repoID string
	var revision int64
	if err := db.QueryRow(`SELECT canonical_root,repo_id,binding_revision FROM worktrees WHERE worktree_id=?`, registration.WorktreeID).Scan(&storedRoot, &repoID, &revision); err != nil {
		t.Fatal(err)
	}
	if storedRoot != repo || repoID != registration.RepoID || revision != 1 {
		t.Fatalf("failed rebind changed catalog state: %q %q %d", storedRoot, repoID, revision)
	}
}

func TestRebindSamePathRequiresExplicitRepoLink(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		shared  bool
		removed bool
	}{{"single-worktree", false, false}, {"shared-with-linked-worktree", true, false}, {"shared-with-removed-worktree", true, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			repo, linked := gitFixture(t, scenario.shared)
			replacement, _ := gitFixture(t, false)
			root := openFixtureRoot(t, repo)
			common, err := fsruntime.OpenLocalGitMetadata(root)
			if err != nil {
				t.Fatal(err)
			}
			db := testDB(t)
			store, err := Open(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			policy := body(t, policyFixture(filepath.Dir(repo)))
			profile := body(t, profileFixture())
			registered, err := store.Register(context.Background(), root, common, "", "default", policy, profile)
			if err != nil {
				t.Fatal(err)
			}
			var linkedID string
			if scenario.shared {
				linkedRoot := openFixtureRoot(t, linked)
				linkedRegistration, err := store.Register(context.Background(), linkedRoot, common, "worktrees/linked", "default", policy, profile)
				if err != nil {
					t.Fatal(err)
				}
				linkedID = linkedRegistration.WorktreeID
				if scenario.removed {
					if _, err := store.Remove(context.Background(), linkedID, 1, true); err != nil {
						t.Fatal(err)
					}
				}
				if err := linkedRoot.Close(); err != nil {
					t.Fatal(err)
				}
			}
			var oldIdentity string
			if err := db.QueryRow(`SELECT filesystem_identity FROM repositories WHERE repo_id=?`, registered.RepoID).Scan(&oldIdentity); err != nil {
				t.Fatal(err)
			}
			// Do not rename while the old directory handles are open on DrvFS.
			if err := common.Close(); err != nil {
				t.Fatal(err)
			}
			if err := root.Close(); err != nil {
				t.Fatal(err)
			}
			oldRepo := filepath.Join(filepath.Dir(repo), "repo-old")
			if err := os.Rename(repo, oldRepo); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, repo); err != nil {
				t.Fatal(err)
			}
			newRoot := openFixtureRoot(t, repo)
			newCommon, err := fsruntime.OpenLocalGitMetadata(newRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer newCommon.Close()
			if _, err := store.Rebind(context.Background(), registered.WorktreeID, 1, true, newRoot, newCommon, ""); !errors.Is(err, ErrRootRebindRequired) {
				t.Fatalf("ordinary rebind silently linked clone: %v", err)
			}
			if _, err := store.RebindWithExplicitRepoLink(context.Background(), registered.WorktreeID, 1, true, false, newRoot, newCommon, ""); !errors.Is(err, ErrConfirmationRequired) {
				t.Fatalf("missing link confirmation accepted: %v", err)
			}
			revision, err := store.RebindWithExplicitRepoLink(context.Background(), registered.WorktreeID, 1, true, true, newRoot, newCommon, "")
			if scenario.shared {
				if !errors.Is(err, ErrRootRebindRequired) {
					t.Fatalf("shared repo identity changed: %d %v", revision, err)
				}
				previewRevision, impacts, err := store.PreviewRepoLink(context.Background(), registered.WorktreeID)
				if err != nil || previewRevision != 1 || len(impacts) != 1 || impacts[0].WorktreeID != linkedID {
					t.Fatalf("link preview: %d %+v %v", previewRevision, impacts, err)
				}
				if _, err := store.RebindWithSharedRepoLink(context.Background(), registered.WorktreeID, 1, true, false, impacts, newRoot, newCommon, ""); !errors.Is(err, ErrConfirmationRequired) {
					t.Fatalf("shared link missing confirmation: %v", err)
				}
				duplicate := append(append([]RepoLinkImpact(nil), impacts...), impacts[0])
				if _, err := store.RebindWithSharedRepoLink(context.Background(), registered.WorktreeID, 1, true, true, duplicate, newRoot, newCommon, ""); !errors.Is(err, ErrRevisionConflict) {
					t.Fatalf("duplicate shared link impact accepted: %v", err)
				}
				stale := append([]RepoLinkImpact(nil), impacts...)
				stale[0].BindingRevision++
				if _, err := store.RebindWithSharedRepoLink(context.Background(), registered.WorktreeID, 1, true, true, stale, newRoot, newCommon, ""); !errors.Is(err, ErrRevisionConflict) {
					t.Fatalf("stale shared link accepted: %v", err)
				}
				var unchangedIdentity, unchangedStatus string
				var unchangedRevision int64
				if err := db.QueryRow(`SELECT r.filesystem_identity,w.status,w.binding_revision FROM worktrees w JOIN repositories r ON r.repo_id=w.repo_id WHERE w.worktree_id=?`, linkedID).Scan(&unchangedIdentity, &unchangedStatus, &unchangedRevision); err != nil {
					t.Fatal(err)
				}
				if unchangedIdentity != oldIdentity || unchangedStatus != impacts[0].Status || unchangedRevision != impacts[0].BindingRevision {
					t.Fatalf("stale shared link modified catalog: %q %q %d", unchangedIdentity, unchangedStatus, unchangedRevision)
				}
				revision, err = store.RebindWithSharedRepoLink(context.Background(), registered.WorktreeID, 1, true, true, impacts, newRoot, newCommon, "")
				if err != nil || revision != 2 {
					t.Fatalf("confirmed shared link: %d %v", revision, err)
				}
				var status string
				var linkedRevision int64
				if err := db.QueryRow(`SELECT status,binding_revision FROM worktrees WHERE worktree_id=?`, linkedID).Scan(&status, &linkedRevision); err != nil {
					t.Fatal(err)
				}
				if scenario.removed && (status != "removed" || linkedRevision != 2) || !scenario.removed && (status != "unavailable" || linkedRevision != 2) {
					t.Fatalf("incorrect affected worktree state: %s %d", status, linkedRevision)
				}
			} else if err != nil || revision != 2 {
				t.Fatalf("explicit repo link failed: %d %v", revision, err)
			}
			var repoID, identity string
			var storedRevision int64
			if err := db.QueryRow(`SELECT w.repo_id,w.binding_revision,r.filesystem_identity FROM worktrees w JOIN repositories r ON r.repo_id=w.repo_id WHERE w.worktree_id=?`, registered.WorktreeID).Scan(&repoID, &storedRevision, &identity); err != nil {
				t.Fatal(err)
			}
			if repoID != registered.RepoID || storedRevision != 2 || identity == oldIdentity {
				t.Fatalf("unexpected link state: repo=%q revision=%d identity=%q", repoID, storedRevision, identity)
			}
			sources := []string{filepath.Join(repo, "file.go"), filepath.Join(oldRepo, "file.go")}
			if scenario.shared {
				sources = append(sources, filepath.Join(linked, "file.go"))
			}
			for _, path := range sources {
				if data, err := os.ReadFile(path); err != nil || string(data) != "package fixture\n" {
					t.Fatalf("source changed: %s: %v", path, err)
				}
			}
		})
	}
}
