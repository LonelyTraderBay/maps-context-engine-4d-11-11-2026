//go:build linux

package golang

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

// This synthetic probe records an upstream behavior; it is not the guarded
// loader. It must not run on a target project or inherit its environment.
func TestPackagesConfigEnvDoesNotControlExecutableLookup(t *testing.T) {
	if os.Getenv("MCE_FIXTURE_GO_LOOKUP") == "child" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = packages.Load(&packages.Config{Context: ctx, Dir: os.Getenv("MCE_FIXTURE_DIR"), Mode: packages.NeedName,
			Env: []string{"PATH=" + filepath.Join(runtime.GOROOT(), "bin"), "GOPACKAGESDRIVER=off", "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "MCE_FIXTURE_MARKER=" + os.Getenv("MCE_FIXTURE_MARKER")}}, ".")
		return
	}
	dir := t.TempDir()
	fakeDir := filepath.Join(dir, "fake-bin")
	if err := os.Mkdir(fakeDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/probe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.go"), []byte("package probe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fake := []byte("#!/bin/sh\nprintf invoked > \"$MCE_FIXTURE_MARKER\"\nexit 1\n")
	if err := os.WriteFile(filepath.Join(fakeDir, "go"), fake, 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "lookup-marker")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPackagesConfigEnvDoesNotControlExecutableLookup$")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + fakeDir, "MCE_FIXTURE_GO_LOOKUP=child", "MCE_FIXTURE_DIR=" + dir, "MCE_FIXTURE_MARKER=" + marker}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated probe: %v: %s", err, output)
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "invoked" {
		t.Fatalf("expected inherited PATH lookup was not observed: %q %v", got, err)
	}
}
