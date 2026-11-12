//go:build linux

package golang

import (
	"context"
	"errors"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func TestTrustedStdlibBoundaries(t *testing.T) {
	ctx := preflightContext(t)
	goRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(goRoot, "src", "fmt"), 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte("package fmt\nvar Value = 1\n")
	filename := filepath.Join(goRoot, "src", "fmt", "print.go")
	if err := os.WriteFile(filename, source, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(filepath.Join(goRoot, "src"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	preflight := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	digest := strings.Repeat("d", 64)
	addLoaderFixtureDependency(t, &preflight, digest)
	scanner, err := policy.NewScanner(2 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{"unsafe", "fmt"}
	trusted := true
	authorize := true
	input := TrustedStdlibInput{SourceRoot: root, GoRoot: goRoot, Profile: preflight.Profile, Snapshot: preflight.Snapshot, DependencyDigest: digest, AllowedPackages: allowed,
		Scanner: scanner, MaxManifestBytes: 32768, MaxFileBytes: 65536, MaxFiles: 2, MaxSourceBytes: 65536,
		AuthorizePath: func(context.Context, string) error {
			if !authorize {
				return errors.New("private path")
			}
			return nil
		},
		VerifyAuthority: func(_ context.Context, sourceRoot, toolchain, dependency string, packages []string) (bool, error) {
			if sourceRoot != filepath.Join(goRoot, "src") || toolchain != preflight.Profile.ToolchainDigest || dependency != digest || strings.Join(packages, ",") != "fmt,unsafe" {
				t.Fatal("authority received wrong identity")
			}
			packages[0] = "changed"
			return trusted, nil
		}}
	wrong := input
	wrong.DependencyDigest = strings.Repeat("e", 64)
	if _, err := NewTrustedStdlib(ctx, wrong); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("unbound dependency accepted: %v", err)
	}
	wrong = input
	wrong.AllowedPackages = []string{"fmt", "fmt"}
	if _, err := NewTrustedStdlib(ctx, wrong); !errors.Is(err, ErrLoaderInput) {
		t.Fatalf("duplicate package accepted: %v", err)
	}
	scope, err := NewTrustedStdlib(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	allowed[1] = "not-fmt"
	if !scope.AllowsPackage("fmt") || scope.AllowsPackage("not-fmt") {
		t.Fatal("caller mutated package authority")
	}
	if _, err := scope.parseFile(ctx, token.NewFileSet(), filepath.Join(goRoot, "outside.go"), source); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("outside source accepted: %v", err)
	}
	if _, err := scope.parseFile(ctx, token.NewFileSet(), filename, []byte("package fmt\nvar Bad = 1\n")); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("mismatched bytes accepted: %v", err)
	}
	if _, err := scope.parseFile(ctx, token.NewFileSet(), filename, source); err != nil || !scope.fileVerified(filename) {
		t.Fatalf("valid source rejected: %v", err)
	}
	authorize = false
	if ok, err := scope.Check(ctx, preflight.Profile, preflight.Snapshot, goRoot); ok || !errors.Is(err, ErrLoaderAuthority) || strings.Contains(err.Error(), "private") {
		t.Fatalf("late path revocation leaked: %v %v", ok, err)
	}
	authorize = true
	if _, err := scope.parseFile(ctx, token.NewFileSet(), filename, source); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("parse budget ignored: %v", err)
	}
	if !scope.begin() || scope.begin() {
		t.Fatal("load scope was reused")
	}
	input.AllowedPackages = []string{"fmt", "unsafe"}
	linked, err := NewTrustedStdlib(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(goRoot, "src", "fmt", "symlink.go")
	if err := os.Symlink("print.go", symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.parseFile(ctx, token.NewFileSet(), symlink, source); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("symlink accepted: %v", err)
	}
	hardlink := filepath.Join(goRoot, "src", "fmt", "hardlink.go")
	if err := os.Link(filename, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.parseFile(ctx, token.NewFileSet(), hardlink, source); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("hardlink accepted: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := linked.parseFile(canceled, token.NewFileSet(), filename, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	trusted = false
	if ok, err := scope.Check(ctx, preflight.Profile, preflight.Snapshot, goRoot); err != nil || ok {
		t.Fatalf("revoked authority accepted: %v %v", ok, err)
	}
	trusted, authorize = true, false
	revoked, err := NewTrustedStdlib(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revoked.parseFile(ctx, token.NewFileSet(), filename, source); !errors.Is(err, ErrLoaderAuthority) || strings.Contains(err.Error(), "private") {
		t.Fatalf("path revocation leaked: %v", err)
	}
}
