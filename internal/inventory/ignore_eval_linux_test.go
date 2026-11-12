//go:build linux

package inventory

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func ignoreBudget() IgnoreBudget {
	return IgnoreBudget{MaxSources: 100, MaxRules: 1000, MaxRuleBytes: 1 << 20, MaxQueries: 1000, MaxQueryBytes: 1 << 20, MaxOutputBytes: 1 << 20}
}

func ignoreSandbox(t *testing.T) (*paths.Root, string) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "sandbox")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := PrepareIgnoreSandbox(ctx, root, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return root, base
}

func ignoreOracle(t *testing.T, f *gitFixture, global string, queries []IgnoreQuery) []bool {
	t.Helper()
	var input bytes.Buffer
	for _, query := range queries {
		input.WriteString(query.Path)
		if query.IsDirectory {
			input.WriteByte('/')
		}
		input.WriteByte(0)
	}
	out, err := f.command(input.Bytes(), "-c", "core.excludesfile="+global, "-c", "core.fsmonitor=false", "check-ignore", "--no-index", "--verbose", "--non-matching", "-z", "--stdin")
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("Git oracle: %v", err)
		}
	}
	fields := bytes.Split(out, []byte{0})
	if len(fields) != len(queries)*4+1 {
		t.Fatalf("unexpected Git oracle output shape: %d", len(fields))
	}
	result := make([]bool, len(queries))
	for i := range queries {
		pattern := fields[i*4+2]
		result[i] = len(pattern) > 0 && pattern[0] != '!'
	}
	return result
}

func TestEvaluateIgnoreRulesMatchesGitAcrossRuleScopes(t *testing.T) {
	f := newGitFixture(t, "sha1")
	sandbox, _ := ignoreSandbox(t)
	scanner, err := policy.NewScanner(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	global := "*.cache\na/\n"
	info := "!a/\n*.bak\n"
	rootRules := "*.log\n!keep.log\ncache/\n!cache/keep.txt\n/only-root\n"
	aRules := "*.tmp\n!keep.tmp\nfoo/**/bar\n"
	subRules := "!foo.tmp\n*.csv\n"
	weirdRules := "*.o\n!keep.o\n"
	globalPath := filepath.Join(f.home, "global.ignore")
	if err := os.WriteFile(globalPath, []byte(global), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".git", "info", "exclude"), []byte(info), 0600); err != nil {
		t.Fatal(err)
	}
	for base, text := range map[string]string{"": rootRules, "a": aRules, "a/sub": subRules, "weird[dir]": weirdRules} {
		dir := filepath.Join(f.root, filepath.FromSlash(base))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	queries := []IgnoreQuery{
		{"x.cache", false}, {"x.bak", false}, {"x.log", false}, {"keep.log", false},
		{"dir/x.log", false}, {"cache", true}, {"cache/keep.txt", false}, {"only-root", false},
		{"a/x.tmp", false}, {"a/keep.tmp", false}, {"a/sub/foo.tmp", false}, {"a/sub/x.csv", false},
		{"a/foo/bar", false}, {"a/foo/x/bar", false}, {"weird[dir]/x.o", false}, {"weird[dir]/keep.o", false}, {"z/file", false},
	}
	sources := []IgnoreSource{
		{Kind: "directory", Base: "a/sub", Rules: []byte(subRules)},
		{Kind: "directory", Base: "weird[dir]", Rules: []byte(weirdRules)},
		{Kind: "info", Rules: []byte(info)},
		{Kind: "directory", Base: "a", Rules: []byte(aRules)},
		{Kind: "global", Rules: []byte(global)},
		{Kind: "directory", Rules: []byte(rootRules)},
	}
	want := ignoreOracle(t, f, globalPath, queries)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	checks := 0
	got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, sources, queries, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { checks++; return nil })
	if err != nil || !reflect.DeepEqual(got, want) || checks != 2 {
		t.Fatalf("isolated ignore vs Git: got=%v want=%v checks=%d err=%v", got, want, checks, err)
	}
}

func TestEvaluateNestedPatternsMatchGit(t *testing.T) {
	patterns := []string{
		"foo\n", "/foo\n", "foo/\n", "foo/bar\n", "**/foo\n",
		"foo/**\n", "foo/**/bar\n", "*.tmp\n!keep.tmp\n", "[ab]*.txt\n",
		"\\#comment\n", "\\!bang\n", "space\\ \n", "foo/  \n", "foo/\\ \n", "/\n", "foo//bar\n",
		"**/x/**\n", "a?c\n", "foo\\/bar\n", "foo\r\n!keep\r\n",
	}
	queries := []IgnoreQuery{
		{"a/foo", false}, {"a/foo", true}, {"a/foo/bar", false}, {"a/foo/x/bar", false},
		{"a/deep/foo", false}, {"a/deep/foo", true}, {"a/deep/foo/bar", false},
		{"a/a.tmp", false}, {"a/keep.tmp", false}, {"a/sub/a.tmp", false},
		{"a/apple.txt", false}, {"a/beta.txt", false}, {"a/#comment", false},
		{"a/!bang", false}, {"a/space ", false}, {"a/foo/ ", false}, {"a/deep/foo/ ", false}, {"a/abc", false}, {"a/x/z", false},
		{"a/foo", false},
	}
	for _, pattern := range patterns {
		t.Run(strings.ReplaceAll(pattern, "\n", "|"), func(t *testing.T) {
			f := newGitFixture(t, "sha1")
			dir := filepath.Join(f.root, "a")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(pattern), 0600); err != nil {
				t.Fatal(err)
			}
			sandbox, _ := ignoreSandbox(t)
			scanner, err := policy.NewScanner(1 << 20)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			want := ignoreOracle(t, f, os.DevNull, queries)
			got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, []IgnoreSource{{Kind: "directory", Base: "a", Rules: []byte(pattern)}}, queries, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { return nil })
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("pattern %q: got=%v want=%v err=%v", pattern, got, want, err)
			}
		})
	}
}

func TestEvaluateNestedPatternsDifferentialCorpus(t *testing.T) {
	f := newGitFixture(t, "sha1")
	dir := filepath.Join(f.root, "a")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	sandbox, _ := ignoreSandbox(t)
	scanner, err := policy.NewScanner(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	queries := []IgnoreQuery{{"a/a", false}, {"a/b", false}, {"a/foo", false}, {"a/a/b", false}, {"a/foo/bar", false}, {"a/x/foo", false}, {"a/a b", false}, {"a/!x", false}, {"a/#x", false}, {"a/sub", true}, {"a/sub/foo", false}}
	atoms := []string{"a", "b", "foo", "*", "**", "?", "[ab]", "/", " ", "\\ ", "\\!", "\\#"}
	rng := rand.New(rand.NewSource(39017))
	matched, unsupported := 0, 0
	for i := 0; i < 200; i++ {
		var pattern strings.Builder
		if rng.Intn(4) == 0 {
			pattern.WriteByte('!')
		}
		for n := 0; n < 1+rng.Intn(5); n++ {
			pattern.WriteString(atoms[rng.Intn(len(atoms))])
		}
		pattern.WriteByte('\n')
		text := pattern.String()
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		want := ignoreOracle(t, f, os.DevNull, queries)
		got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, []IgnoreSource{{Kind: "directory", Base: "a", Rules: []byte(text)}}, queries, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { return nil })
		if errors.Is(err, ErrIgnoreUnsupported) {
			unsupported++
			continue
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("corpus case %d pattern=%q got=%v want=%v err=%v", i, text, got, want, err)
		}
		matched++
	}
	if matched < 150 {
		t.Fatalf("too many unsupported corpus cases: matched=%d unsupported=%d", matched, unsupported)
	}
}

func TestEvaluateIgnoreRulesRejectsTamperedSandboxBeforeExec(t *testing.T) {
	sandbox, base := ignoreSandbox(t)
	if err := os.WriteFile(filepath.Join(base, "work", ".gitignore"), []byte("untrusted"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "executed")
	binary := filepath.Join(t.TempDir(), "fake-git")
	script := "#!/bin/sh\nprintf executed > '" + strings.ReplaceAll(marker, "'", "'\\''") + "'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := EvaluateIgnoreRules(ctx, sandbox, binary, nil, []IgnoreQuery{{"safe.go", false}}, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { return nil })
	if !errors.Is(err, ErrIgnoreUnsupported) || got != nil {
		t.Fatalf("tampered sandbox accepted: %v %v", got, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untrusted binary executed: %v", err)
	}
}

func TestEvaluateIgnoreHonorsExplicitCaseOption(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.run(t, nil, "config", "core.ignorecase", "true")
	if err := os.WriteFile(filepath.Join(f.root, ".gitignore"), []byte("*.LOG\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sandbox, _ := ignoreSandbox(t)
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queries := []IgnoreQuery{{"a.log", false}, {"a.LOG", false}, {"other.go", false}}
	want := ignoreOracle(t, f, os.DevNull, queries)
	got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, []IgnoreSource{{Kind: "directory", Rules: []byte("*.LOG\n")}}, queries, scanner, IgnoreOptions{IgnoreCase: true}, ignoreBudget(), func(context.Context) error { return nil })
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("case option differs from Git: got=%v want=%v err=%v", got, want, err)
	}
	empty, err := EvaluateIgnoreRules(ctx, sandbox, f.git, nil, []IgnoreQuery{{"plain.go", false}}, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { return nil })
	if err != nil || len(empty) != 1 || empty[0] {
		t.Fatalf("empty ignore source: %v %v", empty, err)
	}
}

func TestEvaluateIgnoreRejectsAmbiguousCaseSensitiveDirectoryScopes(t *testing.T) {
	f := newGitFixture(t, "sha1")
	f.run(t, nil, "config", "core.ignorecase", "true")
	for _, name := range []string{"a", "A"} {
		if err := os.Mkdir(filepath.Join(f.root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.root, "a", ".gitignore"), []byte("*.log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	queries := []IgnoreQuery{{"A/file.log", false}}
	want := ignoreOracle(t, f, os.DevNull, queries)
	if len(want) != 1 || want[0] {
		t.Fatalf("fixture must keep sibling directory scope distinct: %v", want)
	}
	sandbox, _ := ignoreSandbox(t)
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, []IgnoreSource{{Kind: "directory", Base: "a", Rules: []byte("*.log\n")}}, queries, scanner, IgnoreOptions{IgnoreCase: true}, ignoreBudget(), func(context.Context) error { return nil })
	if !errors.Is(err, ErrIgnoreUnsupported) || got != nil {
		t.Fatalf("ambiguous case scope accepted: %v %v", got, err)
	}
}

func TestEvaluateIgnoreRulesLimitsAndRevocation(t *testing.T) {
	f := newGitFixture(t, "sha1")
	sandbox, _ := ignoreSandbox(t)
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sources := []IgnoreSource{{Kind: "directory", Rules: []byte("*.log\n")}}
	queries := []IgnoreQuery{{"a.log", false}}
	budget := ignoreBudget()
	budget.MaxOutputBytes = 1
	if got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, sources, queries, scanner, IgnoreOptions{}, budget, func(context.Context) error { return nil }); !errors.Is(err, ErrIgnoreBudget) || got != nil {
		t.Fatalf("output limit ignored: %v %v", got, err)
	}
	denied := errors.New("fixture authorization revoked")
	checks := 0
	got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, sources, queries, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error {
		checks++
		if checks == 2 {
			return denied
		}
		return nil
	})
	if !errors.Is(err, denied) || got != nil {
		t.Fatalf("revoked rules returned data: %v %v", got, err)
	}
	if _, err := EvaluateIgnoreRules(context.Background(), sandbox, f.git, sources, queries, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { return nil }); !errors.Is(err, ErrIgnoreBudget) {
		t.Fatalf("unbounded evaluation accepted: %v", err)
	}
	if _, err := EvaluateIgnoreRules(ctx, sandbox, f.git, sources, []IgnoreQuery{{".env", false}}, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { return nil }); !errors.Is(err, ErrIgnoreUnsupported) {
		t.Fatalf("denied path accepted: %v", err)
	}
	secret := []IgnoreSource{{Kind: "directory", Rules: []byte(`api_key = "abcdefghijklmnop"`)}}
	if got, err := EvaluateIgnoreRules(ctx, sandbox, f.git, secret, queries, scanner, IgnoreOptions{}, ignoreBudget(), func(context.Context) error { return nil }); !errors.Is(err, policy.ErrSecretBlocked) || got != nil {
		t.Fatalf("secret-like rule source accepted: %v %v", got, err)
	}
}

func TestPrepareIgnoreSandboxNeverOverwritesExistingFiles(t *testing.T) {
	base := filepath.Join(t.TempDir(), "sandbox")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(base, "preserve")
	if err := os.WriteFile(marker, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := PrepareIgnoreSandbox(ctx, root, func(context.Context) error { return nil }); !errors.Is(err, ErrIgnoreUnsupported) {
		t.Fatalf("nonempty sandbox accepted: %v", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "original" {
		t.Fatalf("existing file changed: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(base, "git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial sandbox created in nonempty root: %v", err)
	}
}
