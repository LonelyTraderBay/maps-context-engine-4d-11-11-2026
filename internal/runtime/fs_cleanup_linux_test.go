//go:build linux

package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func TestInspectCleanupFileIsReadOnlyAndConfined(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "output")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, "candidate")
	outside := filepath.Join(base, "protected")
	for path, data := range map[string]string{candidate: "output", outside: "protected"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	report, err := InspectCleanupFile(root, "candidate")
	if err != nil || report.Path != "candidate" || report.Size != 6 || !report.Mode.IsRegular() {
		t.Fatalf("report: %+v %v", report, err)
	}
	for _, name := range []string{"link", "../protected"} {
		if report, err = InspectCleanupFile(root, name); err == nil || report != (CleanupFileReport{}) {
			t.Fatalf("unconfined candidate accepted: %+v %v", report, err)
		}
	}
	for path, want := range map[string]string{candidate: "output", outside: "protected"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("inspection changed fixture %q: %v", path, err)
		}
	}
}

func TestCleanupReportDoesNotEstablishIdentityForRemoval(t *testing.T) {
	base := t.TempDir()
	root, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	path := filepath.Join(base, "candidate")
	if err := os.WriteFile(path, []byte("output"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := InspectCleanupFile(root, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(base, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := InspectCleanupFile(root, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("fixture must have identical report fields: %+v %+v", before, after)
	}
	for file, want := range map[string]string{path: "source", filepath.Join(base, "original"): "output"} {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != want {
			t.Fatalf("report changed fixture: %v", err)
		}
	}
}
