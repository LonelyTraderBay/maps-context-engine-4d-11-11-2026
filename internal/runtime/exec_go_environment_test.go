package runtime

import (
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestOfflineGoEnvironmentIgnoresPoisonedParent(t *testing.T) {
	t.Setenv("GOPACKAGESDRIVER", "/malicious/driver")
	t.Setenv("GOFLAGS", "-mod=mod -toolexec=/malicious/tool")
	t.Setenv("LD_PRELOAD", "/malicious/library")
	t.Setenv("GOPROXY", "https://malicious.invalid")
	t.Setenv("GOMAXPROCS", "999")
	base := t.TempDir()
	root := filepath.Join(base, "fixture-scratch")
	goRoot := filepath.Join(base, "approved-toolchain")
	input := GoEnvironmentInput{GoRoot: goRoot, ScratchRoot: root, WorkspaceRoot: filepath.Join(root, "input"), GOOS: "linux", GOARCH: "amd64", BuildTags: []string{"z", "a"}}
	env, flags, err := OfflineGoEnvironment(input)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok || values[key] != "" {
			t.Fatalf("duplicate environment entry: %q", key)
		}
		values[key] = value
	}
	for key, want := range map[string]string{"PATH": filepath.Join(goRoot, "bin"), "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOPACKAGESDRIVER": "off", "GOENV": "off", "GOWORK": "off", "GOFLAGS": "-mod=readonly", "CGO_ENABLED": "0", "GOVCS": "*:off", "GOTELEMETRY": "off"} {
		if values[key] != want {
			t.Errorf("%s=%q want %q", key, values[key], want)
		}
	}
	if _, ok := values["LD_PRELOAD"]; ok {
		t.Fatal("inherited injection variable")
	}
	if limit := GoWorkerLimit(); limit < 1 || limit > 4 || values["GOMAXPROCS"] != strconv.Itoa(limit) {
		t.Fatalf("unbounded child parallelism: %q", values["GOMAXPROCS"])
	}
	if !reflect.DeepEqual(flags, []string{"-mod=readonly", "-buildvcs=false", "-tags=a,z"}) || !reflect.DeepEqual(input.BuildTags, []string{"z", "a"}) {
		t.Fatalf("flags or caller input changed: %v", flags)
	}
}

func TestOfflineGoEnvironmentRejectsEscapesAndFlagInjection(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "fixture-scratch")
	good := GoEnvironmentInput{GoRoot: filepath.Join(base, "approved-toolchain"), ScratchRoot: root, WorkspaceRoot: filepath.Join(root, "input"), GOOS: "linux", GOARCH: "amd64", BuildTags: []string{}}
	for _, tc := range []struct {
		name   string
		modify func(*GoEnvironmentInput)
	}{
		{"relative-root", func(in *GoEnvironmentInput) { in.GoRoot = "relative" }},
		{"outside-workspace", func(in *GoEnvironmentInput) { in.WorkspaceRoot = filepath.Join(base, "outside") }},
		{"root-is-workspace", func(in *GoEnvironmentInput) { in.WorkspaceRoot = in.ScratchRoot }},
		{"toolchain-overlap", func(in *GoEnvironmentInput) { in.GoRoot = filepath.Join(in.ScratchRoot, "go") }},
		{"cache-overlap", func(in *GoEnvironmentInput) { in.WorkspaceRoot = filepath.Join(in.ScratchRoot, "cache", "input") }},
		{"bad-platform", func(in *GoEnvironmentInput) { in.GOARCH = "amd64/../x" }},
		{"tag-flag-injection", func(in *GoEnvironmentInput) { in.BuildTags = []string{"ok,-toolexec=evil"} }},
		{"tag-duplicate", func(in *GoEnvironmentInput) { in.BuildTags = []string{"a", "a"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := good
			tc.modify(&input)
			if env, flags, err := OfflineGoEnvironment(input); !errors.Is(err, ErrGoEnvironment) || env != nil || flags != nil {
				t.Fatalf("unsafe environment accepted: %v", err)
			}
		})
	}
}
