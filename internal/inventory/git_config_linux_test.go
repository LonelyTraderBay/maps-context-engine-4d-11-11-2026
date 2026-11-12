//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"golang.org/x/sys/unix"
)

func TestParseGitConfigFieldsUsesSealedDataWithoutExecutingConfig(t *testing.T) {
	f := newGitFixture(t, "sha1")
	marker := filepath.Join(f.home, "must-not-run")
	hook := filepath.Join(f.home, "fsmonitor.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf x > '"+strings.ReplaceAll(marker, "'", "'\\''")+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("[core]\n ignorecase = false\n ignorecase = yes\n excludesfile = ./approved-ignore\n worktree = ./approved-worktree\n bare = false\n fsmonitor = " + hook + "\n[extensions]\n worktreeConfig = true\n objectFormat = sha256\n[remote \"origin\"]\n url = fixture://example\n")
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	checks := 0
	got, err := ParseGitConfigFields(ctx, f.git, raw, scanner, 4096, 8192, func(context.Context) error { checks++; return nil })
	if err != nil || got.IgnoreCase == nil || !*got.IgnoreCase || got.ExcludesFile == nil || *got.ExcludesFile != "./approved-ignore" || got.Worktree == nil || *got.Worktree != "./approved-worktree" || got.Bare == nil || *got.Bare || got.WorktreeConfig == nil || !*got.WorktreeConfig || got.ObjectFormat == nil || *got.ObjectFormat != "sha256" || checks != 2 {
		t.Fatalf("config fields mismatch: %+v checks=%d err=%v", got, checks, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config command executed fsmonitor: %v", err)
	}
}

func TestParseGitConfigRejectsIncludesWithoutReadingThem(t *testing.T) {
	f := newGitFixture(t, "sha1")
	fifo := filepath.Join(f.home, "unapproved-fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"include", "includeIf \"gitdir:*\""} {
		raw := []byte("[" + section + "]\n path = " + fifo + "\n")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		got, err := ParseGitConfigFields(ctx, f.git, raw, scanner, 4096, 8192, func(context.Context) error { return nil })
		cancel()
		if !errors.Is(err, ErrGitConfigUnsupported) || got.IgnoreCase != nil || got.ExcludesFile != nil {
			t.Fatalf("include was followed or accepted: %+v %v", got, err)
		}
	}
}

func TestParseGitConfigFailsClosedOnInvalidAndRevokedInput(t *testing.T) {
	f := newGitFixture(t, "sha1")
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, raw := range [][]byte{[]byte("[core]\n ignorecase = perhaps\n"), []byte("[extensions]\n objectformat = sha999\n"), []byte("[core]\n excludesfile = \"bad\\npath\"\n")} {
		if got, err := ParseGitConfigFields(ctx, f.git, raw, scanner, 4096, 8192, func(context.Context) error { return nil }); !errors.Is(err, ErrGitConfigUnsupported) || got.IgnoreCase != nil || got.ExcludesFile != nil {
			t.Fatalf("invalid config accepted: %+v %v", got, err)
		}
	}
	denied := errors.New("fixture authority revoked")
	calls := 0
	got, err := ParseGitConfigFields(ctx, f.git, []byte("[core]\n ignorecase = true\n"), scanner, 4096, 8192, func(context.Context) error {
		calls++
		if calls == 2 {
			return denied
		}
		return nil
	})
	if !errors.Is(err, denied) || got.IgnoreCase != nil {
		t.Fatalf("revoked config exposed fields: %+v %v", got, err)
	}
	if _, err := ParseGitConfigFields(ctx, f.git, []byte("[core]\n ignorecase = true\n"), scanner, 4096, 1, func(context.Context) error { return nil }); !errors.Is(err, ErrIgnoreBudget) {
		t.Fatalf("config output limit ignored: %v", err)
	}
}
