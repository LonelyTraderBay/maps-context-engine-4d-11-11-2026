package inventory

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func TestWindowsIgnoreMatcherAgainstGit(t *testing.T) {
	git := os.Getenv("MCE4D_WINDOWS_GIT_ORACLE")
	if !filepath.IsAbs(git) {
		t.Skip("requires the approved local Git executable")
	}
	root := t.TempDir()
	global := []byte("global*.go\nconflict.go\n")
	info := []byte("info?.go\n!conflict.go\n")
	rules := []byte("*.md\n!keep.md\n/a.go\nparent/\n!parent/keep.go\ngenerated/**/trace?.go\nlog[0-9].txt\nclass[!xy].go\n" +
		"escaped\\ space.go\n\\#literal.go\nu?.go\nv??.go\nrootcase.go\n")
	nested := []byte("foo/   \n*.tmp\n!okay.tmp\n")
	for _, name := range []string{".git/refs", ".git/objects", ".git/info", "notes"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for file, data := range map[string][]byte{
		".git/HEAD": []byte("ref: refs/heads/fixture\n"), ".git/info/exclude": info,
		"global.ignore": global, ".gitignore": rules, "notes/.gitignore": nested,
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources := []IgnoreSource{{Kind: "global", Rules: global}, {Kind: "info", Rules: info},
		{Kind: "directory", Rules: rules}, {Kind: "directory", Base: "notes", Rules: nested}}
	walk := CandidateWalk{}
	var input bytes.Buffer
	for _, name := range []string{"globalfile.go", "conflict.go", "infoA.go", "infobb.go", "some.md", "nested/keep.md",
		"a.go", "nested/a.go", "parent/keep.go", "generated/trace1.go", "generated/deep/traceX.go", "generated/deep/trace12.go",
		"nested/log0.txt", "nested/logz.txt", "classa.go", "classx.go", "escaped space.go", "#literal.go", "uư.go", "vư.go",
		"ROOTCASE.go", "notes/foo/code.go", "notes/inner/foo/code.go", "notes/fooled/code.go", "notes/okay.tmp", "other/foo/code.go",
		"notes/shallow/no.tmp", "notes/deep/okay.tmp", "parent/", "parent-file.go"} {
		candidate := Candidate{Path: name}
		if name[len(name)-1] == '/' {
			candidate.Directory = true
			candidate.Path = name[:len(name)-1]
		}
		walk.Candidates = append(walk.Candidates, candidate)
		input.WriteString(name)
		input.WriteByte(0)
	}
	scanner, err := policy.NewScanner(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, compare := range []struct {
		ignoreCase bool
		option     string
	}{{false, "core.ignorecase=false"}, {true, "core.ignorecase=true"}} {
		cmd := exec.CommandContext(ctx, git, "-c", compare.option, "-c", "core.excludesFile="+filepath.Join(root, "global.ignore"),
			"check-ignore", "--no-index", "-v", "--non-matching", "-z", "--stdin")
		cmd.Dir = root
		cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "HOME=" + root, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=NUL", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"}
		cmd.Stdin = bytes.NewReader(input.Bytes())
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		fields := bytes.Split(output, []byte{0})
		if len(fields) != len(walk.Candidates)*4+1 {
			t.Fatalf("Git returned an incomplete comparison: %d fields for %d candidates", len(fields), len(walk.Candidates))
		}
		ours, err := evaluateWindowsIgnores(ctx, sources, walk, scanner, compare.ignoreCase)
		if err != nil {
			t.Fatal(err)
		}
		for i, candidate := range walk.Candidates {
			pattern := fields[i*4+2]
			gitIgnored := len(pattern) != 0 && pattern[0] != '!'
			if ours[i] != gitIgnored {
				t.Errorf("%q ignorecase=%v: local matcher=%v Git=%v pattern=%q", candidate.Path, compare.ignoreCase, ours[i], gitIgnored, pattern)
			}
		}
	}
}

func TestWindowsIgnoreRejectsUncertainPattern(t *testing.T) {
	scanner, err := policy.NewScanner(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = evaluateWindowsIgnores(context.Background(),
		[]IgnoreSource{{Kind: "directory", Rules: []byte("targets[[:alpha:]].go\n")}}, CandidateWalk{}, scanner, false)
	if !errors.Is(err, ErrIgnoreUnsupported) {
		t.Fatalf("unsupported class got %v", err)
	}
}
