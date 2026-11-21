package paths

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsConfinedReadAndPinnedAncestors(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "công-ລາວ")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	const name = "mã-😀.txt"
	const body = "Tiếng Việt\r\nລາວ 😀\r\n"
	if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	child, err := root.OpenDirectory(filepath.Base(directory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Close() })
	file, err := child.OpenRegular(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if err := VerifyRegularFile(file); err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(file)
	if err != nil || string(actual) != body {
		t.Fatalf("captured bytes differ, %v", err)
	}
	if _, err := child.CanonicalPath(); err != nil {
		t.Fatal(err)
	}
	duplicate, err := child.DirectoryHandle()
	if err != nil {
		t.Fatal(err)
	}
	if err := duplicate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, filepath.Join(parent, "moved")); err == nil {
		t.Fatal("pinned directory was renamed")
	}
}

func TestWindowsConfinedReadRejectsNoncanonicalEntries(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "Source.go")
	if err := os.WriteFile(name, []byte("package fixture\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, path := range []string{"../Source.go", "Source.go:stream", "Source.go.", "Source.go ",
		"source.go", "NUL.txt", "CONIN$", "invalid\x00.go", string([]byte{0xff})} {
		file, err := root.OpenRegular(path)
		if err == nil {
			file.Close()
			t.Errorf("accepted noncanonical %q", path)
		}
	}
	if _, err := root.OpenRegular("absent.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file returned %v", err)
	}
	if err := os.Link(name, filepath.Join(dir, "alias.go")); err != nil {
		t.Fatal(err)
	}
	if file, err := root.OpenRegular("Source.go"); !errors.Is(err, ErrHardLinked) {
		if file != nil {
			file.Close()
		}
		t.Fatalf("multiply linked file returned %v", err)
	}
}

func TestWindowsConfinedReadRejectsJunction(t *testing.T) {
	dir, target := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "fixture.txt"), []byte("synthetic fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(dir, "junction")
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
		"New-Item -ItemType Junction -Path $env:MCE4D_TEST_JUNCTION -Target $env:MCE4D_TEST_JUNCTION_TARGET -ErrorAction Stop | Out-Null")
	cmd.Env = append(os.Environ(), "MCE4D_TEST_JUNCTION="+junction, "MCE4D_TEST_JUNCTION_TARGET="+target)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture junction creation failed: %s, %v", output, err)
	}
	t.Cleanup(func() {
		if err := os.Remove(junction); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, "junction", "fixture.txt")); err != nil {
		t.Fatalf("fixture junction was not usable with ordinary filesystem reads: %v", err)
	}
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if file, err := root.OpenRegular("junction/fixture.txt"); err == nil {
		file.Close()
		t.Fatal("junction was followed")
	}
	if child, err := root.OpenDirectory("junction"); err == nil {
		child.Close()
		t.Fatal("junction opened as ordinary directory")
	}
	if child, err := OpenRoot(filepath.Join(dir, "junction")); err == nil {
		child.Close()
		t.Fatal("junction accepted as root")
	}
}
