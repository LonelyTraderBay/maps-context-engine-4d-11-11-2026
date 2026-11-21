package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func runPlatformProfile(ctx context.Context, args []string) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	if args[0] == "check-pools" || args[0] == "activate" {
		flags := flag.NewFlagSet("state-profile "+args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("root", "", "")
		worktree := flags.String("worktree-id", "", "")
		profile := flags.String("profile-id", "", "")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *root == "" || *worktree == "" || *profile == "" {
			return true, 2
		}
		var result any
		var err error
		if args[0] == "activate" {
			result, err = sqlite.ActivateWindowsProtectedProfile(ctx, *root, *worktree, *profile)
		} else {
			result, err = sqlite.CheckWindowsScopedPools(ctx, *root, *worktree, *profile)
		}
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "protected scoped pools unavailable or incomplete")
			return true, 3
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return true, 5
		}
		return true, 0
	}
	if args[0] == "verify-roles" || args[0] == "bootstrap-wal" {
		flags := flag.NewFlagSet("state-profile "+args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("root", "", "")
		engine := flags.Uint64("engine-pid", 0, "")
		client := flags.Uint64("client-pid", 0, "")
		const maxPID = 1<<32 - 1
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *root == "" ||
			*engine == 0 || *engine > maxPID || *client == 0 || *client > maxPID || *engine == *client {
			return true, 2
		}
		var result any
		var err error
		if args[0] == "verify-roles" {
			result, err = sqlite.VerifyWindowsProfileRoles(ctx, *root, uint32(*engine), uint32(*client))
		} else {
			result, err = sqlite.BootstrapWindowsCatalogWAL(ctx, *root, uint32(*engine), uint32(*client))
		}
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "protected profile verification or WAL preparation refused")
			return true, 3
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return true, 5
		}
		return true, 0
	}
	if args[0] == "bootstrap-catalog" {
		flags := flag.NewFlagSet("state-profile bootstrap-catalog", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("root", "", "")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *root == "" {
			return true, 2
		}
		summary, err := sqlite.BootstrapWindowsCatalogSeed(ctx, *root)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "protected catalog preparation refused or incomplete")
			return true, 3
		}
		if err := sqlite.EncodeProfileSummary(os.Stdout, summary); err != nil {
			return true, 5
		}
		return true, 0
	}
	if args[0] != "prepare" {
		return false, 0
	}
	flags := flag.NewFlagSet("state-profile prepare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	opts := sqlite.WindowsProfilePrepareOptions{}
	flags.StringVar(&opts.Root, "root", "", "")
	flags.StringVar(&opts.EngineSID, "engine-sid", "", "")
	flags.StringVar(&opts.ClientSID, "client-sid", "", "")
	flags.StringVar(&opts.LspSID, "lsp-sid", "", "")
	flags.StringVar(&opts.GitIgnore, "git-ignore", "", "")
	var targets targetRoots
	flags.Var(&targets, "target-root", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 ||
		opts.Root == "" || opts.EngineSID == "" || opts.ClientSID == "" || len(targets) == 0 {
		return true, 2
	}
	opts.TargetRoots = targets
	summary, err := sqlite.PrepareWindowsProtectedProfile(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "protected profile preparation refused or incomplete")
		return true, 3
	}
	if err := sqlite.EncodeProfileSummary(os.Stdout, summary); err != nil {
		return true, 5
	}
	return true, 0
}
