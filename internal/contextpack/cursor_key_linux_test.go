//go:build linux

package contextpack

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

func cursorKeyRoot(t *testing.T) (*paths.Root, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, dir
}

func TestCursorKeyConcurrentCreationAndReuse(t *testing.T) {
	root, dir := cursorKeyRoot(t)
	const n = 16
	keys := make([][32]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			keys[i], errs[i] = LoadOrCreateCursorKey(ctx, root)
		}(i)
	}
	wg.Wait()
	for i := range keys {
		if errs[i] != nil || keys[i] != keys[0] || keys[i] == ([32]byte{}) {
			t.Fatalf("concurrent key mismatch at %d: %v", i, errs[i])
		}
	}
	info, err := os.Stat(filepath.Join(dir, cursorKeyName))
	if err != nil || info.Size() != 32 || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("unsafe installed key metadata: %v", err)
	}
	if _, err = os.Lstat(filepath.Join(dir, cursorKeyPending)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending key remains after success: %v", err)
	}
}

func TestCursorKeyRejectsUnsafeState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(string) error
		want  error
	}{
		{"shared directory", func(dir string) error { return os.Chmod(dir, 0755) }, ErrCursorKeyUnsafe},
		{"shared key", func(dir string) error {
			name := filepath.Join(dir, cursorKeyName)
			if err := os.WriteFile(name, bytesOf(7, 32), 0600); err != nil {
				return err
			}
			// Set the unsafe mode explicitly, independent of inherited umask.
			return os.Chmod(name, 0644)
		}, ErrCursorKeyUnsafe},
		{"short key", func(dir string) error { return os.WriteFile(filepath.Join(dir, cursorKeyName), bytesOf(7, 31), 0600) }, ErrCursorKeyUnsafe},
		{"zero key", func(dir string) error { return os.WriteFile(filepath.Join(dir, cursorKeyName), bytesOf(0, 32), 0600) }, ErrCursorKeyUnsafe},
		{"pending recovery", func(dir string) error {
			return os.WriteFile(filepath.Join(dir, cursorKeyPending), []byte("pending"), 0600)
		}, ErrCursorKeyPending},
		{"symlink key", func(dir string) error {
			return os.Symlink(filepath.Join(dir, "external"), filepath.Join(dir, cursorKeyName))
		}, ErrCursorKeyUnsafe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, dir := cursorKeyRoot(t)
			if err := tc.setup(dir); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			key, err := LoadOrCreateCursorKey(ctx, root)
			if !errors.Is(err, tc.want) || key != ([32]byte{}) {
				t.Fatalf("unsafe state accepted: %v", err)
			}
			if tc.want == ErrCursorKeyPending {
				data, err := os.ReadFile(filepath.Join(dir, cursorKeyPending))
				if err != nil || string(data) != "pending" {
					t.Fatal("pending data was changed")
				}
			}
		})
	}
	root, _ := cursorKeyRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadOrCreateCursorKey(ctx, root); !errors.Is(err, ErrCursorKeyUnsafe) {
		t.Fatalf("unbounded request was accepted: %v", err)
	}
}

func bytesOf(v byte, count int) []byte {
	buf := make([]byte, count)
	for i := range buf {
		buf[i] = v
	}
	return buf
}

func TestCursorKeyCrossProcess(t *testing.T) {
	if dir := os.Getenv("MCE4D_CURSOR_KEY_FIXTURE"); dir != "" {
		root, err := paths.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		key, err := LoadOrCreateCursorKey(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%x\n", sha256.Sum256(key[:]))
		return
	}
	_, dir := cursorKeyRoot(t)
	const n = 4
	outputs := make([][]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range outputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCursorKeyCrossProcess$")
			cmd.Env = append(os.Environ(), "MCE4D_CURSOR_KEY_FIXTURE="+dir)
			outputs[i], errs[i] = cmd.CombinedOutput()
		}(i)
	}
	wg.Wait()
	var expected string
	for i := range outputs {
		fields := strings.Fields(string(outputs[i]))
		if errs[i] != nil || len(fields) < 1 || len(fields[0]) != 64 {
			t.Fatalf("child %d failed: %v %s", i, errs[i], outputs[i])
		}
		if expected == "" {
			expected = fields[0]
		}
		if fields[0] != expected {
			t.Fatal("different keys across processes")
		}
	}
}
