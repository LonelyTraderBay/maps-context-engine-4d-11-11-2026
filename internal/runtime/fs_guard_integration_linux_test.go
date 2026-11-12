//go:build linux

package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// This is an OS-level fixture for the T005 primitives. It is not a product
// indexing, authorization, Git inventory, or deletion integration test.
func TestSecurityPrimitivesOnOwnedFixture(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	output := filepath.Join(base, "output")
	for _, dir := range []string{filepath.Join(source, ".git"), filepath.Join(source, "nested-output"), filepath.Join(output, "records")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	marker := "TESTONLY" + strings.Repeat("X", 20)
	files := map[string]string{
		filepath.Join(source, ".git", "index"):         "DIRCfixture",
		filepath.Join(source, "clean.go"):              "package clean\n",
		filepath.Join(source, ".env"):                  "password=" + marker,
		filepath.Join(source, ".env.example"):          "api_key=" + marker,
		filepath.Join(output, "records", "records.db"): "durable fixture",
		filepath.Join(base, "outside"):                 "outside fixture",
	}
	for path, value := range files {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(filepath.Join(base, "outside"), filepath.Join(source, "linked.go")); err != nil {
		t.Fatal(err)
	}
	open := func(path string) *paths.Root {
		t.Helper()
		root, err := paths.OpenRoot(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { root.Close() })
		return root
	}
	root := open(source)
	outputRoot := open(output)
	recordsRoot := open(filepath.Join(output, "records"))
	nestedRoot := open(filepath.Join(source, "nested-output"))
	metadata, err := OpenLocalGitMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	index, err := ReadGitIndex(metadata, 1024)
	if err != nil || !bytes.Equal(index, []byte("DIRCfixture")) {
		t.Fatalf("bounded metadata fixture: %v", err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := scanner.ReadSource(root, "clean.go"); err != nil || string(body) != "package clean\n" {
		t.Fatalf("allowed source: %v", err)
	}
	for _, name := range []string{".env", ".env.example", "linked.go"} {
		body, err := scanner.ReadSource(root, name)
		if err == nil || body != nil || strings.Contains(err.Error(), marker) {
			t.Fatalf("protected source %q yielded bytes or marker", name)
		}
	}
	if err := CheckCleanupRoot(outputRoot, []*paths.Root{root}); err != nil {
		t.Fatalf("disjoint output root: %v", err)
	}
	for _, pair := range [][2]*paths.Root{{nestedRoot, root}, {outputRoot, recordsRoot}} {
		if err := CheckCleanupRoot(pair[0], []*paths.Root{pair[1]}); !errors.Is(err, ErrCleanupProtected) {
			t.Fatalf("protected cleanup root accepted: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if data, err := GitInventory(ctx, root, "/fixture/not-executed", 1024); !errors.Is(err, ErrGitMetadataUnverified) || data != nil {
		t.Fatalf("unverified Git inventory yielded data: %v", err)
	}
	for path, value := range files {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != value {
			t.Fatalf("fixture changed: %v", err)
		}
	}
}
