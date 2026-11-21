package inventory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// ReadWindowsTrackedIndex reads a bounded local index for the supported
// SHA-1 repository configuration. It neither walks untracked files nor runs
// Git. Callers still authorize and revalidate each selected file.
func ReadWindowsTrackedIndex(ctx context.Context, metadata *paths.Root, maxEntries int) (IndexObservation, string, bool, error) {
	const maxIndex = 4 << 20
	capture := func() ([]byte, []byte, error) {
		config, err := readObservedCapture(ctx, metadata, "config", 64<<10)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		index, err := readObservedCapture(ctx, metadata, "index", maxIndex)
		if errors.Is(err, os.ErrNotExist) {
			return config, nil, nil
		}
		return config, index, err
	}
	config, raw, err := capture()
	if err != nil {
		return IndexObservation{}, "", false, err
	}
	ignoreCase, ok := windowsTrackedRepositoryConfig(config)
	if !ok {
		return IndexObservation{}, "", false, ErrGitConfigUnsupported
	}
	index := IndexObservation{IndexMissing: raw == nil}
	if raw != nil {
		index.Index, err = ParseGitIndex(raw, "sha1", maxEntries, 4096, maxIndex)
		if err != nil {
			return IndexObservation{}, "", false, err
		}
	}
	againConfig, again, err := capture()
	if err != nil || !bytes.Equal(config, againConfig) || (raw == nil) != (again == nil) || !bytes.Equal(raw, again) {
		return IndexObservation{}, "", false, errors.Join(ErrIndexChanged, err)
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("MCE4D-WINDOWS-TRACKED1\x00"))
	_, _ = hash.Write(config)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(raw)
	return index, hex.EncodeToString(hash.Sum(nil)), ignoreCase, metadata.Verify()
}

// The staged path supports Git's basic local SHA-1 repository configuration.
// Extensions, includes, aliases and external worktree selection are refused.
func windowsTrackedRepositoryConfig(raw []byte) (ignoreCase, valid bool) {
	section := ""
	readCore := false
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "[") {
			switch {
			case lower == "[core]" && !readCore:
				section, readCore = "core", true
			case strings.HasSuffix(lower, "\"]") && (strings.HasPrefix(lower, "[remote \"") || strings.HasPrefix(lower, "[branch \"")):
				section = "reference"
			default:
				return false, false
			}
			continue
		}
		key, value, present := strings.Cut(line, "=")
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.ToLower(strings.TrimSpace(value))
		if !present || strings.ContainsAny(value, "\x00\r") || strings.HasSuffix(value, "\\") {
			return false, false
		}
		if section == "reference" {
			switch key {
			case "url", "pushurl", "fetch", "remote", "merge", "rebase":
				continue
			default:
				return false, false
			}
		}
		if section != "core" || seen[key] {
			return false, false
		}
		seen[key] = true
		switch key {
		case "repositoryformatversion":
			if value != "0" {
				return false, false
			}
		case "bare":
			if value != "false" {
				return false, false
			}
		case "filemode", "logallrefupdates", "symlinks", "ignorecase":
			if value != "true" && value != "false" {
				return false, false
			}
			if key == "ignorecase" {
				ignoreCase = value == "true"
			}
		case "autocrlf":
			if value != "true" && value != "false" && value != "input" {
				return false, false
			}
		default:
			return false, false
		}
	}
	return ignoreCase, true
}
