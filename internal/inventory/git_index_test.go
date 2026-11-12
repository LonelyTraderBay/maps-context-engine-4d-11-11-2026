//go:build linux

package inventory

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type gitFixture struct {
	git, root, home string
}

func newGitFixture(t *testing.T, format string) *gitFixture {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	f := &gitFixture{git: git, root: root, home: base}
	args := []string{"init", "-q"}
	if format == "sha256" {
		args = append(args, "--object-format=sha256")
	}
	if out, err := f.command(nil, args...); err != nil {
		if format == "sha256" {
			t.Skipf("fixture Git lacks SHA-256 repository support: %v", err)
		}
		t.Fatalf("git init: %v %s", err, out)
	}
	return f
}

func (f *gitFixture) command(input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.git, append([]string{"-C", f.root}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + f.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0"}
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	return cmd.CombinedOutput()
}

func (f *gitFixture) run(t *testing.T, input []byte, args ...string) []byte {
	t.Helper()
	out, err := f.command(input, args...)
	if err != nil {
		t.Fatalf("fixture git %q: %v %s", args, err, out)
	}
	return out
}

func (f *gitFixture) write(t *testing.T, name string) {
	t.Helper()
	path := filepath.Join(f.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture source\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *gitFixture) index(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type oracleEntry struct {
	mode  uint32
	oid   string
	stage uint8
	path  string
}

func (f *gitFixture) oracle(t *testing.T, options ...string) []oracleEntry {
	t.Helper()
	args := append([]string{"ls-files", "--stage", "-z"}, options...)
	data := f.run(t, nil, args...)
	var result []oracleEntry
	for _, item := range bytes.Split(data, []byte{0}) {
		if len(item) == 0 {
			continue
		}
		tab := bytes.IndexByte(item, '\t')
		if tab < 0 {
			t.Fatalf("bad Git oracle record: %q", item)
		}
		fields := strings.Split(string(item[:tab]), " ")
		if len(fields) != 3 {
			t.Fatalf("bad Git oracle fields: %q", item)
		}
		mode, err := strconv.ParseUint(fields[0], 8, 32)
		if err != nil {
			t.Fatal(err)
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, oracleEntry{mode: uint32(mode), oid: fields[1], stage: uint8(stage), path: string(item[tab+1:])})
	}
	return result
}

func assertGitOracle(t *testing.T, f *gitFixture, format string, version uint32, options ...string) GitIndex {
	t.Helper()
	got, err := ParseGitIndex(f.index(t), format, 1000, 4096, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != version {
		t.Fatalf("index version: got %d want %d", got.Version, version)
	}
	want := f.oracle(t, options...)
	if len(got.Entries) != len(want) {
		t.Fatalf("entries: got %d want %d", len(got.Entries), len(want))
	}
	for i, entry := range got.Entries {
		actual := oracleEntry{mode: entry.Mode, oid: hex.EncodeToString(entry.ObjectID), stage: entry.Stage, path: string(entry.Path)}
		if actual != want[i] {
			t.Fatalf("entry %d: got %+v want %+v", i, actual, want[i])
		}
	}
	return got
}

func TestParseGitIndexAgainstGitV2V3V4(t *testing.T) {
	f := newGitFixture(t, "sha1")
	files := []string{"alpha.go", "dir/shared-one.go", "dir/shared-two.go", "space name.go", "-leading.go", "line\nbreak.go", strings.Repeat("p", 140) + "/long.go"}
	for _, file := range files {
		f.write(t, file)
	}
	f.run(t, nil, append([]string{"add", "--"}, files...)...)
	assertGitOracle(t, f, "sha1", 2)
	f.run(t, nil, "update-index", "--assume-unchanged", "--", "alpha.go")
	f.run(t, nil, "update-index", "--skip-worktree", "--", "dir/shared-one.go")
	three := assertGitOracle(t, f, "sha1", 3)
	for _, entry := range three.Entries {
		if string(entry.Path) == "alpha.go" && !entry.AssumeUnchanged {
			t.Fatal("assume-unchanged flag missing")
		}
		if string(entry.Path) == "dir/shared-one.go" && !entry.SkipWorktree {
			t.Fatal("skip-worktree flag missing")
		}
	}
	f.run(t, nil, "update-index", "--index-version=4")
	assertGitOracle(t, f, "sha1", 4)
	if _, err := ParseGitIndex(f.index(t), "sha1", 1000, 4096, 100); !errors.Is(err, ErrIndexBudget) {
		t.Fatalf("v4 decoded path budget was ignored: %v", err)
	}
}

func TestParseGitSparseIndexReportsDirectoryBoundary(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "a/inside.go")
	f.write(t, "b/outside.go")
	f.run(t, nil, "add", "--", "a/inside.go", "b/outside.go")
	f.run(t, nil, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
	f.run(t, nil, "sparse-checkout", "init", "--cone", "--sparse-index")
	f.run(t, nil, "sparse-checkout", "set", "a")
	raw := f.index(t)
	version := binary.BigEndian.Uint32(raw[4:8])
	parsed := assertGitOracle(t, f, "sha1", version, "--sparse")
	if !parsed.SparseDirectories {
		t.Fatal("sparse-index extension not reported")
	}
	found := false
	for _, entry := range parsed.Entries {
		if string(entry.Path) == "b/" {
			found = entry.Mode == 0040000 && entry.SkipWorktree
		}
	}
	if !found {
		t.Fatalf("sparse directory boundary missing: %+v", parsed.Entries)
	}
}

func TestParseGitIndexUnmergedAndSHA256(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			f := newGitFixture(t, format)
			f.write(t, "normal.go")
			f.run(t, nil, "add", "--", "normal.go")
			oid := strings.TrimSpace(string(f.run(t, nil, "hash-object", "-w", "normal.go")))
			input := []byte(fmt.Sprintf("100644 %s 1\tconflict.go\n100644 %s 2\tconflict.go\n", oid, oid))
			f.run(t, input, "update-index", "--index-info")
			parsed := assertGitOracle(t, f, format, 2)
			if len(parsed.Entries) != 3 || parsed.Entries[0].Stage != 1 || parsed.Entries[1].Stage != 2 {
				t.Fatalf("conflict stages: %+v", parsed.Entries)
			}
		})
	}
}

func appendIndexExtension(raw []byte, format, signature string, payload []byte) []byte {
	size := sha1.Size
	if format == "sha256" {
		size = sha256.Size
	}
	body := bytes.Clone(raw[:len(raw)-size])
	body = append(body, signature...)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(payload)))
	body = append(body, n[:]...)
	body = append(body, payload...)
	if format == "sha256" {
		sum := sha256.Sum256(body)
		return append(body, sum[:]...)
	}
	sum := sha1.Sum(body)
	return append(body, sum[:]...)
}

func TestParseGitIndexRejectsCorruptionAndLimits(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.write(t, "one.go")
	f.write(t, "two.go")
	f.run(t, nil, "add", "--", "one.go", "two.go")
	raw := f.index(t)
	changed := bytes.Clone(raw)
	changed[15] ^= 0x01
	invalidModeBody := bytes.Clone(raw[:len(raw)-sha1.Size])
	binary.BigEndian.PutUint32(invalidModeBody[12+24:12+28], 0100777)
	invalidModeHash := sha1.Sum(invalidModeBody)
	invalidMode := append(invalidModeBody, invalidModeHash[:]...)
	invalidVersionBody := bytes.Clone(raw[:len(raw)-sha1.Size])
	binary.BigEndian.PutUint32(invalidVersionBody[4:8], 5)
	invalidVersionHash := sha1.Sum(invalidVersionBody)
	invalidVersion := append(invalidVersionBody, invalidVersionHash[:]...)
	for _, tc := range []struct {
		name          string
		data          []byte
		format        string
		entries, path int
		want          error
	}{
		{"checksum", changed, "sha1", 10, 100, ErrIndexCorrupt},
		{"truncated", raw[:len(raw)-1], "sha1", 10, 100, ErrIndexCorrupt},
		{"count", raw, "sha1", 1, 100, ErrIndexBudget},
		{"path", raw, "sha1", 10, 3, ErrIndexBudget},
		{"unknown-hash", raw, "other", 10, 100, ErrIndexUnsupported},
		{"split-index", appendIndexExtension(raw, "sha1", "link", nil), "sha1", 10, 100, ErrIndexUnsupported},
		{"unknown-required", appendIndexExtension(raw, "sha1", "abcd", nil), "sha1", 10, 100, ErrIndexUnsupported},
		{"invalid-mode", invalidMode, "sha1", 10, 100, ErrIndexCorrupt},
		{"unsupported-version", invalidVersion, "sha1", 10, 100, ErrIndexUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseGitIndex(tc.data, tc.format, tc.entries, tc.path, 1024); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := ParseGitIndex(raw, "sha1", 10, 100, 10); !errors.Is(err, ErrIndexBudget) {
		t.Fatalf("aggregate decoded path budget was ignored: %v", err)
	}
}
