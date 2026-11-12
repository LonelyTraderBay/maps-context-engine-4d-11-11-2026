//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

func TestCleanupRootRejectsProtectedOverlap(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"app/nested", "application", "output/records", "separate"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, value := range map[string]string{"app/source.go": "source", "output/records/records.sqlite": "durable"} {
		if err := os.WriteFile(filepath.Join(base, path), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	open := func(name string) *paths.Root {
		t.Helper()
		r, err := paths.OpenRoot(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		return r
	}
	app, appNested := open("app"), open("app/nested")
	application, output := open("application"), open("output")
	records, separate := open("output/records"), open("separate")
	if err := CheckCleanupRoot(application, []*paths.Root{app, separate}); err != nil {
		t.Fatalf("disjoint roots rejected: %v", err)
	}
	for _, pair := range [][2]*paths.Root{{app, app}, {appNested, app}, {app, appNested}, {output, records}} {
		if err := CheckCleanupRoot(pair[0], []*paths.Root{pair[1]}); !errors.Is(err, ErrCleanupProtected) {
			t.Fatalf("overlap was not rejected: %v", err)
		}
	}
	if err := CheckCleanupRoot(output, nil); !errors.Is(err, ErrCleanupUnverified) {
		t.Fatalf("missing protected set was accepted: %v", err)
	}
	if err := CheckCleanupRoot(output, []*paths.Root{nil}); !errors.Is(err, ErrCleanupUnverified) {
		t.Fatalf("nil protected root was accepted: %v", err)
	}
	if err := CheckCleanupRoot(nil, []*paths.Root{app}); !errors.Is(err, ErrCleanupUnverified) {
		t.Fatalf("nil output root was accepted: %v", err)
	}
	if err := CheckCleanupRoot(output, make([]*paths.Root, 65)); !errors.Is(err, ErrCleanupUnverified) {
		t.Fatalf("unbounded protected set accepted: %v", err)
	}
	for path, value := range map[string]string{"app/source.go": "source", "output/records/records.sqlite": "durable"} {
		got, err := os.ReadFile(filepath.Join(base, path))
		if err != nil || string(got) != value {
			t.Fatalf("protected fixture changed: %v", err)
		}
	}
}

func TestCleanupRootRejectsNestedBindMount(t *testing.T) {
	if os.Getenv("MCE4D_FIXTURE_MOUNT_HELPER") != "1" {
		unshare, err := exec.LookPath("unshare")
		if err != nil {
			t.Skip("unshare unavailable")
		}
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		parentNamespace, err := os.Readlink("/proc/self/ns/mnt")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, unshare, "--user", "--map-root-user", "--mount", binary, "-test.run=^TestCleanupRootRejectsNestedBindMount$", "-test.v")
		cmd.Env = append(os.Environ(), "MCE4D_FIXTURE_MOUNT_HELPER=1", "MCE4D_FIXTURE_PARENT_MOUNT_NS="+parentNamespace)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "unshare failed: Operation not permitted") {
				t.Skip("isolated user mount namespace unavailable")
			}
			t.Fatalf("isolated mount fixture: %v\n%s", err, out)
		}
		return
	}
	parentNamespace := os.Getenv("MCE4D_FIXTURE_PARENT_MOUNT_NS")
	currentNamespace, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil || parentNamespace == "" || currentNamespace == parentNamespace {
		t.Fatal("mount fixture must run in a distinct namespace")
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	source := filepath.Join(base, "source")
	output := filepath.Join(base, "output with space")
	alias := filepath.Join(output, "alias with space")
	for _, dir := range []string{source, alias} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount(source, alias, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Unmount(alias, 0); err != nil {
			t.Error(err)
		}
	}()
	outRoot, err := paths.OpenRoot(output)
	if err != nil {
		t.Fatal(err)
	}
	defer outRoot.Close()
	sourceRoot, err := paths.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceRoot.Close()
	if err := CheckCleanupRoot(outRoot, []*paths.Root{sourceRoot}); !errors.Is(err, ErrCleanupUnverified) && !errors.Is(err, ErrCleanupProtected) {
		t.Fatalf("nested bind mount was accepted: %v", err)
	}
}

func TestCleanupMountHelperRefusesOriginalNamespace(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestCleanupRootRejectsNestedBindMount$")
	cmd.Env = append(os.Environ(), "MCE4D_FIXTURE_MOUNT_HELPER=1", "MCE4D_FIXTURE_PARENT_MOUNT_NS="+namespace)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "mount fixture must run in a distinct namespace") {
		t.Fatalf("fixture did not reject the original namespace: %v\n%s", err, out)
	}
}

func TestCleanupMountPathParsingAndBoundary(t *testing.T) {
	for _, test := range []struct {
		encoded, want string
		valid         bool
	}{
		{`/a\040b/child`, "/a b/child", true},
		{`/a\134b`, `/a\b`, true},
		{`/a\011b`, "/a\tb", true},
		{`/a\09x`, "", false},
		{`/a\000b`, "", false},
		{`/a\777`, "", false},
		{`/a\04`, "", false},
	} {
		got, ok := decodeCleanupMountPath(test.encoded)
		if got != test.want || ok != test.valid {
			t.Fatalf("mount escape %q: %q %v", test.encoded, got, ok)
		}
	}
	for _, test := range []struct {
		root, path string
		nested     bool
	}{
		{"/app", "/app", false},
		{"/app", "/app/child", true},
		{"/app", "/application", false},
		{"/", "/", false},
		{"/", "/child", true},
	} {
		if got := isNestedCleanupPath(test.root, test.path); got != test.nested {
			t.Fatalf("root %q path %q: %v", test.root, test.path, got)
		}
	}
}

func TestCleanupRootRejectsReplacedRootAndDifferentMount(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"output", "protected"} {
		if err := os.Mkdir(filepath.Join(base, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	output, err := paths.OpenRoot(filepath.Join(base, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	protected, err := paths.OpenRoot(filepath.Join(base, "protected"))
	if err != nil {
		t.Fatal(err)
	}
	defer protected.Close()
	proc, err := paths.OpenRoot("/proc")
	if err == nil {
		defer proc.Close()
		a, errA := cleanupRootAncestors(output)
		b, errB := cleanupRootAncestors(proc)
		if errA == nil && errB == nil && a.mount != b.mount {
			if err := CheckCleanupRoot(output, []*paths.Root{proc}); !errors.Is(err, ErrCleanupUnverified) {
				t.Fatalf("different mount was accepted: %v", err)
			}
		}
	}
	if err := os.Rename(filepath.Join(base, "output"), filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "protected"), filepath.Join(base, "output")); err != nil {
		t.Fatal(err)
	}
	if err := CheckCleanupRoot(output, []*paths.Root{protected}); !errors.Is(err, ErrCleanupUnverified) {
		t.Fatalf("replaced root was accepted: %v", err)
	}
}
