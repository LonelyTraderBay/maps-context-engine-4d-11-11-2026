//go:build linux

package inventory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// ParseGitConfigFields parses a previously authorized configuration snapshot
// using a trusted Git binary and a sealed memory file. Includes are disabled;
// any include directive is refused rather than silently omitting its effects.
// It never executes target configuration and returns no unrelated config data.
func ParseGitConfigFields(ctx context.Context, gitBinary string, raw []byte, scanner *policy.Scanner, maxInputBytes, maxOutputBytes int, authorize func(context.Context) error) (fields GitConfigFields, resultErr error) {
	if ctx == nil || !filepath.IsAbs(gitBinary) || scanner == nil || authorize == nil || maxInputBytes < 1 || maxInputBytes > 4*1024*1024 || maxOutputBytes < 1 || maxOutputBytes > 8*1024*1024 || len(raw) > maxInputBytes {
		return GitConfigFields{}, ErrGitConfigUnsupported
	}
	if _, ok := ctx.Deadline(); !ok {
		return GitConfigFields{}, ErrGitConfigUnsupported
	}
	if err := authorize(ctx); err != nil {
		return GitConfigFields{}, err
	}
	data := bytes.Clone(raw)
	if err := scanner.CheckContent(data); err != nil {
		return GitConfigFields{}, err
	}
	file, err := sealedMemoryFile("mce4d-git-config", data)
	if err != nil {
		return GitConfigFields{}, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fields = GitConfigFields{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	cmd := exec.CommandContext(ctx, gitBinary, "config", "--file", "/proc/self/fd/3", "--no-includes", "--null", "--list")
	cmd.Dir = "/"
	cmd.Env = []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "HOME=/nonexistent", "PATH=/usr/bin:/bin", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	cmd.ExtraFiles = []*os.File{file}
	output := &ignoreBoundedWriter{max: maxOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return GitConfigFields{}, ctx.Err()
		}
		if output.exceeded {
			return GitConfigFields{}, ErrIgnoreBudget
		}
		return GitConfigFields{}, ErrGitConfigUnsupported
	}
	if output.exceeded {
		return GitConfigFields{}, ErrIgnoreBudget
	}
	data = output.buffer.Bytes()
	if len(data) > 0 && data[len(data)-1] != 0 {
		return GitConfigFields{}, ErrGitConfigUnsupported
	}
	for len(data) > 0 {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			return GitConfigFields{}, ErrGitConfigUnsupported
		}
		record := data[:end]
		data = data[end+1:]
		cut := bytes.IndexByte(record, '\n')
		key, value, hasValue := string(record), "", false
		if cut >= 0 {
			key, value, hasValue = string(record[:cut]), string(record[cut+1:]), true
		}
		key = strings.ToLower(key)
		if key == "include.path" || strings.HasPrefix(key, "includeif.") {
			return GitConfigFields{}, ErrGitConfigUnsupported
		}
		switch key {
		case "core.ignorecase", "core.bare", "extensions.worktreeconfig":
			v, ok := gitConfigBool(value, hasValue)
			if !ok {
				return GitConfigFields{}, ErrGitConfigUnsupported
			}
			switch key {
			case "core.ignorecase":
				fields.IgnoreCase = &v
			case "core.bare":
				fields.Bare = &v
			default:
				fields.WorktreeConfig = &v
			}
		case "core.excludesfile", "core.worktree":
			if !hasValue || !validGitConfigPath(value) {
				return GitConfigFields{}, ErrGitConfigUnsupported
			}
			v := value
			if key == "core.excludesfile" {
				fields.ExcludesFile = &v
			} else {
				fields.Worktree = &v
			}
		case "extensions.objectformat":
			v := strings.ToLower(value)
			if !hasValue || v != "sha1" && v != "sha256" {
				return GitConfigFields{}, ErrGitConfigUnsupported
			}
			fields.ObjectFormat = &v
		}
	}
	if err := authorize(ctx); err != nil {
		return GitConfigFields{}, err
	}
	if err := ctx.Err(); err != nil {
		return GitConfigFields{}, err
	}
	return fields, nil
}

func gitConfigBool(value string, hasValue bool) (bool, bool) {
	if !hasValue {
		return true, true
	}
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return true, true
	case "false", "no", "off", "0":
		return false, true
	default:
		return false, false
	}
}

func validGitConfigPath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
