//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func captureFixture(t *testing.T) (string, *paths.Root, *policy.Scanner) {
	t.Helper()
	dir := t.TempDir()
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	scanner, err := policy.NewScanner(128)
	if err != nil {
		t.Fatal(err)
	}
	return dir, root, scanner
}

func allowFixture(context.Context, string) error { return nil }

func TestCaptureHashesActualBytesEvenWhenMtimeAndSizeMatch(t *testing.T) {
	dir, root, scanner := captureFixture(t)
	path := filepath.Join(dir, "file.go")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureTime := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, fixtureTime, fixtureTime); err != nil {
		t.Fatal(err)
	}
	first, err := CaptureFile(context.Background(), root, scanner, "file.go", 128, allowFixture)
	if err != nil || first.ContentHash() != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("first capture: %q %v", first.ContentHash(), err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("xyz"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("fixture did not preserve mtime/size: before=%v after=%v", before.ModTime(), after.ModTime())
	}
	second, err := CaptureFile(context.Background(), root, scanner, "file.go", 128, allowFixture)
	if err != nil || second.ContentHash() == first.ContentHash() || string(second.Bytes()) != "xyz" || string(first.Bytes()) != "abc" {
		t.Fatalf("changed bytes not captured independently: %q %v", second.ContentHash(), err)
	}
}

func TestCaptureDeniesPathsSecretsAndLostAuthorization(t *testing.T) {
	dir, root, scanner := captureFixture(t)
	if file, err := CaptureFile(context.Background(), root, scanner, ".env", 128, allowFixture); !errors.Is(err, policy.ErrDeniedPath) || file.Path() != "" {
		t.Fatalf("denied path was read: %v", err)
	}
	secret := []byte(`api_key = "abcdefghijklmnop"`)
	if err := os.WriteFile(filepath.Join(dir, "file.go"), secret, 0600); err != nil {
		t.Fatal(err)
	}
	if file, err := CaptureFile(context.Background(), root, scanner, "file.go", 128, allowFixture); !errors.Is(err, policy.ErrSecretBlocked) || file.Path() != "" {
		t.Fatalf("secret was released: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("package p\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if file, err := CaptureFile(context.Background(), root, scanner, "file.go", 3, allowFixture); !errors.Is(err, ErrCaptureLimit) || file.Path() != "" {
		t.Fatalf("oversized capture returned data: %v", err)
	}
	denied := errors.New("fixture permission revoked")
	calls := 0
	authorize := func(context.Context, string) error {
		calls++
		if calls > 1 {
			return denied
		}
		return nil
	}
	if file, err := CaptureFile(context.Background(), root, scanner, "file.go", 128, authorize); !errors.Is(err, denied) || file.Path() != "" || calls != 2 {
		t.Fatalf("revoked authorization returned data: %v calls=%d", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if file, err := CaptureFile(ctx, root, scanner, "file.go", 128, allowFixture); !errors.Is(err, context.Canceled) || file.Path() != "" {
		t.Fatalf("cancelled capture returned data: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "file.go"), filepath.Join(dir, "link.go")); err != nil {
		t.Fatal(err)
	}
	if file, err := CaptureFile(context.Background(), root, scanner, "link.go", 128, allowFixture); err == nil || file.Path() != "" {
		t.Fatalf("symlink capture accepted: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(dir, "hardlink.go")); err != nil {
		t.Fatal(err)
	}
	if file, err := CaptureFile(context.Background(), root, scanner, "hardlink.go", 128, allowFixture); !errors.Is(err, paths.ErrHardLinked) || file.Path() != "" {
		t.Fatalf("hard-linked capture accepted: %v", err)
	}
}

type changingReader struct {
	file   *os.File
	change func() error
	done   bool
}

func (r *changingReader) Read(p []byte) (int, error) {
	n, err := r.file.Read(p)
	if n > 0 && !r.done {
		r.done = true
		if changeErr := r.change(); changeErr != nil {
			return n, changeErr
		}
	}
	return n, err
}

func TestCaptureDetectsSameSizeAndMtimeChangeDuringRead(t *testing.T) {
	dir, root, _ := captureFixture(t)
	path := filepath.Join(dir, "file.go")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureTime := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, fixtureTime, fixtureTime); err != nil {
		t.Fatal(err)
	}
	file, err := root.OpenRegular("file.go")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	reader := &changingReader{file: file, change: func() error {
		if err := os.WriteFile(path, []byte("xyz"), 0600); err != nil {
			return err
		}
		if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
			return err
		}
		after, err := os.Stat(path)
		if err != nil {
			return err
		}
		if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			return errors.New("fixture failed to preserve mtime and size")
		}
		return nil
	}}
	data, err := readOpenedCapture(context.Background(), root, "file.go", file, reader, 128)
	if !errors.Is(err, ErrCaptureChanged) || data != nil {
		t.Fatalf("changed file was accepted: %q %v", data, err)
	}
}

func TestCaptureRejectsPathReplacementDuringRead(t *testing.T) {
	dir, root, _ := captureFixture(t)
	path := filepath.Join(dir, "file.go")
	replacement := filepath.Join(dir, "replacement")
	for name, data := range map[string]string{path: "abc", replacement: "xyz"} {
		if err := os.WriteFile(name, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file, err := root.OpenRegular("file.go")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := &changingReader{file: file, change: func() error { return os.Rename(replacement, path) }}
	data, err := readOpenedCapture(context.Background(), root, "file.go", file, reader, 128)
	if !errors.Is(err, ErrCaptureChanged) || data != nil {
		t.Fatalf("replaced path returned data: %q %v", data, err)
	}
}

func TestCaptureDetectsUnlinkedFileBeforeRead(t *testing.T) {
	dir, root, _ := captureFixture(t)
	path := filepath.Join(dir, "file.go")
	replacement := filepath.Join(dir, "replacement")
	for name, data := range map[string]string{path: "abc", replacement: "xyz"} {
		if err := os.WriteFile(name, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file, err := root.OpenRegular("file.go")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	data, err := readOpenedCapture(context.Background(), root, "file.go", file, file, 128)
	if !errors.Is(err, ErrCaptureChanged) || data != nil {
		t.Fatalf("unlinked source did not trigger retry: %q %v", data, err)
	}
}

func TestCaptureToFrozenManifestPreservesInput(t *testing.T) {
	dir, root, scanner := captureFixture(t)
	path := filepath.Join(dir, "file.go")
	if err := os.WriteFile(path, []byte("package p\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := CaptureFile(context.Background(), root, scanner, "file.go", 128, allowFixture)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{
		SchemaVersion: 1, WorktreeID: "01234567-89ab-cdef-0123-456789abcdef",
		ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64),
		Files:                  []model.ManifestFile{{Path: file.Path(), Status: "captured", ContentHash: &hash, ByteLength: &length}},
		DependencyInputDigests: []string{},
	}
	frozen, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil || model.VerifyCaptureDigest(frozen.Manifest(), 8192) != nil {
		t.Fatalf("freeze captured input: %v", err)
	}
	if err := os.WriteFile(path, []byte("package q\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	captured, ok := frozen.File("file.go")
	if !ok || string(captured.Bytes()) != "package p\r\n" || captured.ContentHash() != hash {
		t.Fatal("later source change altered frozen input")
	}
}

func TestCaptureRetriesOnlyBoundedObservedChanges(t *testing.T) {
	dir, root, scanner := captureFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reader := func(ctx context.Context, root *paths.Root, path string, limit int) ([]byte, error) {
		calls++
		if calls < 3 {
			return nil, ErrCaptureChanged
		}
		return readObservedCapture(ctx, root, path, limit)
	}
	file, err := captureFile(context.Background(), root, scanner, "file.go", 128, allowFixture, reader)
	if err != nil || calls != 3 || string(file.Bytes()) != "abc" {
		t.Fatalf("bounded retry: calls=%d %v", calls, err)
	}
	calls = 0
	reader = func(context.Context, *paths.Root, string, int) ([]byte, error) {
		calls++
		return nil, ErrCaptureChanged
	}
	file, err = captureFile(context.Background(), root, scanner, "file.go", 128, allowFixture, reader)
	if !errors.Is(err, ErrCaptureChanged) || calls != 3 || file.Path() != "" {
		t.Fatalf("unbounded retry or partial data: calls=%d %v", calls, err)
	}
}
