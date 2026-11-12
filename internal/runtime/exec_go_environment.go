package runtime

import (
	"errors"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrGoEnvironment = errors.New("invalid offline Go execution environment")

// GoWorkerLimit bounds a single loader to the S15 default. Coordinating the
// total across simultaneous jobs remains the scheduler's responsibility.
func GoWorkerLimit() int {
	limit := runtime.NumCPU()
	if current := runtime.GOMAXPROCS(0); current < limit {
		limit = current
	}
	if limit > 4 {
		limit = 4
	}
	if limit < 1 {
		return 1
	}
	return limit
}

type GoEnvironmentInput struct {
	GoRoot, ScratchRoot, WorkspaceRoot string
	GOOS, GOARCH                       string
	BuildTags                          []string
}

// OfflineGoEnvironment constructs a complete child environment and fixed
// build flags; it does not inspect, create or authorize any path. The caller
// must verify toolchain identity, scratch ownership and process containment.
// In particular, packages.Load may resolve the executable using its parent
// process PATH before applying this child environment.
func OfflineGoEnvironment(input GoEnvironmentInput) ([]string, []string, error) {
	for _, value := range []string{input.GoRoot, input.ScratchRoot, input.WorkspaceRoot} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value || !utf8.ValidString(value) {
			return nil, nil, ErrGoEnvironment
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return nil, nil, ErrGoEnvironment
			}
		}
	}
	workspaceRelative, err := filepath.Rel(input.ScratchRoot, input.WorkspaceRoot)
	if err != nil || workspaceRelative == "." || workspaceRelative == ".." || strings.HasPrefix(workspaceRelative, ".."+string(filepath.Separator)) || filepath.IsAbs(workspaceRelative) {
		return nil, nil, ErrGoEnvironment
	}
	inside := func(base, candidate string) bool {
		relative, err := filepath.Rel(base, candidate)
		return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
	}
	if inside(input.ScratchRoot, input.GoRoot) || inside(input.GoRoot, input.ScratchRoot) {
		return nil, nil, ErrGoEnvironment
	}
	validPlatform := func(value string) bool {
		if len(value) < 1 || len(value) > 32 {
			return false
		}
		for _, r := range value {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
				return false
			}
		}
		return true
	}
	if !validPlatform(input.GOOS) || !validPlatform(input.GOARCH) || input.BuildTags == nil || len(input.BuildTags) > 128 {
		return nil, nil, ErrGoEnvironment
	}
	tags := append([]string(nil), input.BuildTags...)
	sort.Strings(tags)
	tagBytes := 0
	for i, tag := range tags {
		if len(tag) < 1 || len(tag) > 128 || i > 0 && tags[i-1] == tag {
			return nil, nil, ErrGoEnvironment
		}
		for _, r := range tag {
			if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' && r != '.' {
				return nil, nil, ErrGoEnvironment
			}
		}
		tagBytes += len(tag)
	}
	if tagBytes > 4096 {
		return nil, nil, ErrGoEnvironment
	}
	workspaceParts := strings.Split(workspaceRelative, string(filepath.Separator))
	for _, reserved := range []string{"cache", "modcache", "gopath", "home", "tmp"} {
		if workspaceParts[0] == reserved {
			return nil, nil, ErrGoEnvironment
		}
	}
	env := []string{
		"PATH=" + filepath.Join(input.GoRoot, "bin"),
		"GOROOT=" + input.GoRoot,
		"HOME=" + filepath.Join(input.ScratchRoot, "home"),
		"GOPATH=" + filepath.Join(input.ScratchRoot, "gopath"),
		"GOCACHE=" + filepath.Join(input.ScratchRoot, "cache"),
		"GOMODCACHE=" + filepath.Join(input.ScratchRoot, "modcache"),
		"TMPDIR=" + filepath.Join(input.ScratchRoot, "tmp"),
		"GOTMPDIR=" + filepath.Join(input.ScratchRoot, "tmp"),
		"GOOS=" + input.GOOS, "GOARCH=" + input.GOARCH, "CGO_ENABLED=0",
		"GOMAXPROCS=" + strconv.Itoa(GoWorkerLimit()),
		"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off",
		"GOVCS=*:off", "GOPACKAGESDRIVER=off", "GOTELEMETRY=off", "GOFLAGS=-mod=readonly",
	}
	flags := []string{"-mod=readonly", "-buildvcs=false"}
	if len(tags) > 0 {
		flags = append(flags, "-tags="+strings.Join(tags, ","))
	}
	return env, flags, nil
}
