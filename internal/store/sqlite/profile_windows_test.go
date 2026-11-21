package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestWindowsPackagedProfilePreparationAndReport(t *testing.T) {
	bin := os.Getenv("MCE4D_WIN_PACKAGE_FIXTURE")
	if bin == "" {
		t.Skip("requires approved packaged Windows binaries")
	}
	if !filepath.IsAbs(bin) || filepath.Clean(bin) != bin {
		t.Fatal("packaged test binaries require an explicit absolute directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	parent := windowsTestControl(t, "")
	base, err := parent.control.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(base, "state-fixture")
	target := t.TempDir()
	targetFile := filepath.Join(target, "source.go")
	canary := []byte("package synthetic\r\n")
	if err := os.WriteFile(targetFile, canary, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(want int, tool string, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(bin, tool+".exe"), args...)
		cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "TEMP=" + t.TempDir(),
			"MCE4D_HOME=" + state, "GOMAXPROCS=2"}
		var diagnostics bytes.Buffer
		cmd.Stderr = &diagnostics
		output, err := cmd.Output()
		got := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			got = exit.ExitCode()
		}
		if got != want || want != 0 && len(output) != 0 {
			t.Fatalf("%s exited %d, wanted %d: %q", tool, got, want, diagnostics.String())
		}
		if want != 0 && diagnostics.Len() == 0 {
			t.Fatal("refusal without diagnostic")
		}
		return output
	}
	// SID declarations are reserved synthetic input. This only prepares the
	// controller stage and never authenticates the declared worker accounts.
	prepare := []string{"prepare", "--root", state, "--target-root", target,
		"--engine-sid", "S-1-5-21-1-2-3-1001", "--client-sid", "S-1-5-21-1-2-3-1002"}
	bad := append([]string{}, prepare...)
	bad[4] = base
	run(3, "state-profile", bad...)
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refusal changed the profile path: %v", err)
	}
	var summary ProtectedProfileSummary
	if err := json.Unmarshal(run(0, "state-profile", prepare...), &summary); err != nil ||
		summary.Stage != "CREATING" || summary.CatalogVerified || summary.TrustedHead || summary.CatalogIncarnation != "" {
		t.Fatalf("unexpected controller stage: %+v, %v", summary, err)
	}
	run(3, "state-profile", prepare...)
	var checked ProtectedProfileSummary
	if err := json.Unmarshal(run(0, "state-profile", "check", "--root", state), &checked); err != nil || checked != summary {
		t.Fatalf("checked controller stage: %+v, %v", checked, err)
	}
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	control, err := root.OpenDirectory("control")
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	expect := func(phase string, hasJournal bool) {
		t.Helper()
		var report StoreRecoveryReport
		if err := json.Unmarshal(run(0, "mce4d", "store", "recover", "--target", "catalog"), &report); err != nil ||
			!report.ReportOnly || report.ActualStateVerified || report.JournalPresent != hasJournal || report.Phase != phase {
			t.Fatalf("unexpected packaged report: %+v, %v", report, err)
		}
	}
	expect("none", false)
	var seeded ProtectedProfileSummary
	if err := json.Unmarshal(run(0, "state-profile", "bootstrap-catalog", "--root", state), &seeded); err != nil ||
		seeded.ProfileID != summary.ProfileID || seeded.Stage != "CREATING" ||
		!seeded.CatalogVerified || seeded.TrustedHead || !model.ValidUUID(seeded.CatalogIncarnation) {
		t.Fatalf("unexpected catalog seed: %+v, %v", seeded, err)
	}
	if err := json.Unmarshal(run(0, "state-profile", "check", "--root", state), &checked); err != nil || checked != seeded {
		t.Fatalf("reopened catalog seed: %+v, %v", checked, err)
	}
	run(3, "state-profile", "bootstrap-catalog", "--root", state)
	expect("none", false)
	journal := maintenanceJournalFixture()
	journal.ProfileInstallationID = summary.ProfileID
	files := windowsMaintenanceFiles{control: control, acl: parent.acl, profile: summary.ProfileID, scope: "catalog"}
	if err := files.write(ctx, journal, false, nil); err != nil {
		t.Fatal(err)
	}
	contents := func() map[string][]byte {
		t.Helper()
		out := make(map[string][]byte)
		if err := filepath.WalkDir(state, func(name string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative, err := filepath.Rel(state, name)
			if err != nil {
				return err
			}
			out[relative], err = os.ReadFile(name)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := contents()
	expect("NEEDS_RECOVERY", true)
	run(3, "mce4d", "store", "recover", "--target", "catalog", "--apply", "--confirm", "--wait-ms", "500")
	worktree := "33333333-3333-4333-8333-333333333333"
	run(3, "mce4d", "mcp", "--worktree-id", worktree, "--profile-id", strings.Repeat("a", 64))
	if !reflect.DeepEqual(before, contents()) {
		t.Fatal("report or denied action changed controller metadata")
	}
	if data, err := os.ReadFile(targetFile); err != nil || !bytes.Equal(canary, data) {
		t.Fatalf("packaged commands modified the target canary: %v", err)
	}
	record := filepath.Join(state, "control", windowsCatalogSeedRecord)
	held := record + ".test"
	if err := os.Rename(record, held); err != nil {
		t.Fatal(err)
	}
	run(3, "state-profile", "check", "--root", state)
	if err := os.Rename(held, record); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(state, "control", windowsCatalogSeedName)
	changed := append([]byte(nil), before[filepath.Join("control", windowsCatalogSeedName)]...)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(image, changed, 0600); err != nil {
		t.Fatal(err)
	}
	run(3, "state-profile", "check", "--root", state)
	if actual, err := os.ReadFile(image); err != nil || !bytes.Equal(actual, changed) {
		t.Fatalf("rejected check changed seed bytes: %v", err)
	}
	t.Log("packaged NTFS catalog seed verified and damage refused; recovery reports remain read-only")
}
