//go:build linux

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func fixtureGoExecutable(t *testing.T) (*GoExecutable, string, context.Context) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("synthetic go executable; never executed\n")
	if err := os.WriteFile(filepath.Join(dir, "go"), data, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	sum := sha256.Sum256(data)
	tool, err := OpenGoExecutable(ctx, GoExecutableInput{BinRoot: root, ExpectedSHA256: hex.EncodeToString(sum[:]), MaxBytes: 1024, Authorize: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tool.Close() })
	return tool, dir, ctx
}

func TestGoExecutableRejectsPoisonedParentPath(t *testing.T) {
	tool, dir, ctx := fixtureGoExecutable(t)
	t.Setenv("PATH", dir)
	if err := tool.CheckForLoad(ctx, filepath.Dir(dir)); err != nil {
		t.Fatalf("expected verified executable: %v", err)
	}
	t.Setenv("PATH", filepath.Join(dir, "untrusted")+string(os.PathListSeparator)+dir)
	if err := tool.CheckForLoad(ctx, filepath.Dir(dir)); !errors.Is(err, ErrGoToolchain) {
		t.Fatalf("poisoned parent PATH accepted: %v", err)
	}
}

func TestGoExecutableDetectsChangedBytesAndReplacement(t *testing.T) {
	for _, mode := range []string{"content", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			tool, dir, ctx := fixtureGoExecutable(t)
			t.Setenv("PATH", dir)
			filename := filepath.Join(dir, "go")
			if mode == "content" {
				if err := os.WriteFile(filename, []byte("different executable"), 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				next := filepath.Join(dir, "next")
				if err := os.WriteFile(next, []byte("synthetic go executable; never executed\n"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(next, filename); err != nil {
					t.Fatal(err)
				}
			}
			if err := tool.CheckForLoad(ctx, filepath.Dir(dir)); !errors.Is(err, ErrGoToolchain) {
				t.Fatalf("changed executable accepted: %v", err)
			}
		})
	}
}

func TestGoExecutableRejectsSymlinkBadDigestAndRevocation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("fake"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "go")); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sum := sha256.Sum256([]byte("fake"))
	input := GoExecutableInput{BinRoot: root, ExpectedSHA256: hex.EncodeToString(sum[:]), MaxBytes: 1024, Authorize: func(context.Context) error { return nil }}
	if tool, err := OpenGoExecutable(ctx, input); !errors.Is(err, ErrGoToolchain) || tool != nil {
		t.Fatalf("symlink accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "real"), filepath.Join(dir, "go")); err != nil {
		t.Fatal(err)
	}
	input.ExpectedSHA256 = string(make([]byte, 64))
	if tool, err := OpenGoExecutable(ctx, input); !errors.Is(err, ErrGoToolchain) || tool != nil {
		t.Fatalf("invalid digest accepted: %v", err)
	}
	input.ExpectedSHA256 = hex.EncodeToString(sum[:])
	checks := 0
	input.Authorize = func(context.Context) error {
		checks++
		if checks == 2 {
			return errors.New("private authority detail")
		}
		return nil
	}
	if tool, err := OpenGoExecutable(ctx, input); !errors.Is(err, ErrGoToolchain) || tool != nil {
		t.Fatalf("revoked executable accepted: %v", err)
	}
}
