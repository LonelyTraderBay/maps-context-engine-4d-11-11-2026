package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func testPackagedWindowsRestore(t *testing.T, ctx context.Context, bin, root, worktree, profile string) {
	t.Helper()
	run := func(binary string, want int, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(bin, binary), args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+root, "MCE4D_WINDOWS_ROLE_IPC=1")
		out, err := cmd.CombinedOutput()
		exit := 0
		if err != nil {
			var ex *exec.ExitError
			if !errors.As(err, &ex) {
				t.Fatal(err)
			}
			exit = ex.ExitCode()
		}
		if exit != want {
			t.Fatalf("%s %s exited %d, want %d: %s", binary, strings.Join(args[:min(len(args), 2)], " "), exit, want, out)
		}
		return out
	}
	observe := func() (p sqlite.WindowsScopedPoolsCheck) {
		t.Helper()
		out := run("state-profile.exe", 0, "check-pools", "--root", root, "--worktree-id", worktree, "--profile-id", profile)
		if err := json.Unmarshal(out, &p); err != nil || !p.WALHeadsVerified || p.WorktreeID != worktree || p.ProfileID != profile ||
			!model.ValidUUID(p.CatalogIncarnation) || !model.ValidUUID(p.IndexIncarnation) || !model.ValidUUID(p.RecordsIncarnation) {
			t.Fatalf("verified pool identities: %s, %v", out, err)
		}
		return p
	}

	current := observe()
	key, err := contextpack.GenerateCursorKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec(key)
	clear(key[:])
	if err != nil {
		t.Fatal(err)
	}
	scope := func(p sqlite.WindowsScopedPoolsCheck) model.ScopeVector {
		t.Helper()
		digest := strings.Repeat("a", 64)
		s := model.ScopeVector{Kind: "single", CatalogIncarnation: p.CatalogIncarnation, UnavailableMembers: []model.MemberUnavailable{},
			Members: []model.ScopeMember{{WorktreeID: worktree, ProfileID: profile, GenerationID: "22222222-2222-4222-8222-222222222222",
				SnapshotDigest: digest, PolicyDigest: digest, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 0,
				IndexIncarnation: p.IndexIncarnation, RecordsIncarnation: p.RecordsIncarnation}}}
		var err error
		s.VectorDigest, err = model.ComputeScopeVectorDigest(s, 32768)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	authorize := func(_ context.Context, binding contextpack.CursorBinding) error {
		if model.ComparePinnedScope(binding.Scope, scope(current), 32768) != nil {
			return contextpack.ErrCursorInvalid
		}
		return nil
	}
	digest := strings.Repeat("a", 64)
	for _, target := range []string{"worktree", "catalog"} {
		prior := current
		tokens := map[string]string{}
		for _, purpose := range []string{"pack", "list", "rename-result"} {
			binding := contextpack.CursorBinding{Purpose: purpose, PrincipalDigest: digest, BindingDigest: digest,
				RequestDigest: digest, OrderVersion: "v1", Scope: scope(prior), ResultID: "33333333-3333-4333-8333-333333333333", ResultDigest: digest}
			token, err := codec.Issue(ctx, binding, contextpack.SignedPosition{NextItem: 1, LastKey: []string{}}, authorize)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.VerifyPrincipal(ctx, token, purpose, digest, authorize); err != nil {
				t.Fatal(err)
			}
			tokens[purpose] = token
		}

		directory := filepath.Join(root, "control")
		selection := []string{"--target", target}
		expectedCount := 2
		if target == "worktree" {
			directory = filepath.Join(directory, "worktrees", worktree)
			selection = append(selection, "--worktree-id", worktree)
			expectedCount = 3
		}
		out := filepath.Join(directory, "backups", "disposable")
		args := append([]string{"store", "backup"}, selection...)
		args = append(args, "--output", out)
		var report sqlite.ProtectedBackupSummary
		if raw := run("mce4d.exe", 0, args...); json.Unmarshal(raw, &report) != nil ||
			report.Target != target || report.FileCount != expectedCount {
			t.Fatalf("backup report: %s", raw)
		}
		run("mce4d.exe", 3, args...)
		manifest := filepath.Join(out, "backup.json")
		raw, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		layout, err := sqlite.DecodeStoreBackupManifest(raw)
		if err != nil || layout.Target != target || len(layout.Stores)+1 != report.FileCount || layout.OperationID != report.OperationID {
			t.Fatalf("invalid backup manifest: %v", err)
		}
		last := layout.Stores[len(layout.Stores)-1]
		artifact := filepath.Join(out, last.FileName)
		image, err := os.ReadFile(artifact)
		if err != nil || int64(len(image)) != last.ByteLength {
			t.Fatalf("invalid SQLite backup: %v", err)
		}
		changed := bytes.Clone(image)
		changed[len(changed)-1] ^= 1
		if err := os.WriteFile(artifact, changed, 0600); err != nil {
			t.Fatal(err)
		}
		restore := []string{"store", "restore", "--manifest", manifest, "--confirm", "--wait-ms", "4000"}
		run("mce4d.exe", 3, restore...)
		if err := os.WriteFile(artifact, image, 0600); err != nil {
			t.Fatal(err)
		}
		if observe() != prior {
			t.Fatal("rejected backup changed the live scope")
		}
		extra := filepath.Join(out, "extra.fixture")
		if err := os.WriteFile(extra, []byte("unexpected entry"), 0600); err != nil {
			t.Fatal(err)
		}
		run("mce4d.exe", 3, restore...)
		if err := os.Remove(extra); err != nil {
			t.Fatal(err)
		}
		var restored sqlite.ProtectedRestoreSummary
		if raw := run("mce4d.exe", 0, restore...); json.Unmarshal(raw, &restored) != nil ||
			restored.Target != target || restored.Outcome != "restored" || !model.ValidUUID(restored.OperationID) {
			t.Fatalf("restore report: %s", raw)
		}
		recoverArgs := append([]string{"store", "recover"}, selection...)
		recoverArgs = append(recoverArgs, "--report-only")
		var recovery sqlite.StoreRecoveryReport
		if raw := run("mce4d.exe", 0, recoverArgs...); json.Unmarshal(raw, &recovery) != nil ||
			!recovery.ReportOnly || recovery.Phase != "VERIFIED" || recovery.OperationID != restored.OperationID {
			t.Fatalf("recovery report: %s", raw)
		}
		current = observe()
		if target == "worktree" && (current.CatalogIncarnation != prior.CatalogIncarnation ||
			current.IndexIncarnation == prior.IndexIncarnation || current.RecordsIncarnation == prior.RecordsIncarnation) ||
			target == "catalog" && (current.CatalogIncarnation == prior.CatalogIncarnation ||
				current.IndexIncarnation != prior.IndexIncarnation || current.RecordsIncarnation != prior.RecordsIncarnation) {
			t.Fatalf("incorrect scope rotation on %s restore: old=%+v new=%+v", target, prior, current)
		}
		for purpose, token := range tokens {
			if _, err := codec.VerifyPrincipal(ctx, token, purpose, digest, authorize); !errors.Is(err, contextpack.ErrCursorInvalid) {
				t.Fatalf("old %s token survived %s restore: %v", purpose, target, err)
			}
		}
		t.Logf("packaged %s backup and restore verified, bad artifact refused, scoped identities rotated and three synthetic token purposes refused by the production codec", target)
		if os.Getenv("MCE4D_WIN_RESTORE_CRASH_FIXTURE") == "1" {
			testWindowsRestoreCrashBoundaries(t, ctx, bin, root, worktree, profile, manifest, target, selection, run)
			current = observe()
		}
	}
	control := filepath.Join(root, "control")
	slot := filepath.Join(control, "worktrees", worktree)
	head := filepath.Join(slot, "records-control", "wal-head.log")
	authority, err := os.ReadFile(head)
	if err != nil {
		t.Fatal(err)
	}
	testWindowsInspection(t, ctx, filepath.Join(bin, "mce4d.exe"), root, worktree, profile, control, slot, head, authority)
}

func testWindowsRestoreCrashBoundaries(t *testing.T, ctx context.Context, bin, root, worktree, profile, manifest, target string,
	selection []string, run func(string, int, ...string) []byte) {
	t.Helper()
	duringReplacement := "catalog_main"
	if target == "worktree" {
		duringReplacement = "records_main"
	}
	recoverBase := append([]string{"store", "recover"}, selection...)
	for _, boundary := range []string{"PREPARED", duringReplacement, "VERIFYING"} {
		cmd := exec.CommandContext(ctx, filepath.Join(bin, "journal.test.exe"), "-test.run=^TestWindowsRestoreCrashWorker$")
		cmd.Env = append(os.Environ(), "MCE4D_WINDOWS_RESTORE_TEST_BOUNDARY="+boundary,
			"MCE4D_WINDOWS_RESTORE_TEST_ROOT="+root, "MCE4D_WINDOWS_RESTORE_TEST_MANIFEST="+manifest)
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 79 {
			t.Fatalf("%s crash at %s: %s, %v", target, boundary, out, err)
		}
		expectPhase := boundary
		if boundary == duringReplacement {
			expectPhase = "REPLACING"
		}
		var report sqlite.StoreRecoveryReport
		raw := run("mce4d.exe", 0, append(recoverBase, "--report-only")...)
		if err := json.Unmarshal(raw, &report); err != nil || report.Phase != expectPhase || !report.JournalPresent ||
			report.ActualStateVerified || !model.ValidUUID(report.OperationID) {
			t.Fatalf("%s crash report: %s, %v", target, raw, err)
		}
		run("state-profile.exe", 3, "check-pools", "--root", root, "--worktree-id", worktree, "--profile-id", profile)
		apply := append(recoverBase, "--apply", "--confirm", "--wait-ms", "4000")
		if boundary == duringReplacement && target == "worktree" {
			slot := filepath.Join(root, "control", "worktrees", worktree)
			stagedHead := filepath.Join(slot, "restore-"+report.OperationID, "new-records_head")
			head, err := os.ReadFile(stagedHead)
			if err != nil || len(head) == 0 {
				t.Fatalf("staged head unavailable: %v", err)
			}
			tampered := bytes.Clone(head)
			tampered[len(tampered)-1] ^= 1
			if err := os.WriteFile(stagedHead, tampered, 0600); err != nil {
				t.Fatal(err)
			}
			run("mce4d.exe", 3, apply...)
			again := run("mce4d.exe", 0, append(recoverBase, "--report-only")...)
			if !bytes.Equal(raw, again) {
				t.Fatal("failed recovery advanced the journal")
			}
			if err := os.WriteFile(stagedHead, head, 0600); err != nil {
				t.Fatal(err)
			}
		}
		var result sqlite.ProtectedRestoreSummary
		raw = run("mce4d.exe", 0, apply...)
		if err := json.Unmarshal(raw, &result); err != nil || result.OperationID != report.OperationID || result.Outcome != "restored" {
			t.Fatalf("%s recovery: %s, %v", target, raw, err)
		}
		retry := run("mce4d.exe", 0, apply...)
		if !bytes.Equal(raw, retry) {
			t.Fatalf("%s repeated recovery returned a different report", target)
		}
		run("state-profile.exe", 0, "check-pools", "--root", root, "--worktree-id", worktree, "--profile-id", profile)
		t.Logf("%s restore recovered after process termination at %s", target, boundary)
	}
}
