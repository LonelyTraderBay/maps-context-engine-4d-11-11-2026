// state_profile is an offline administrator tool. It is not a daemon, setuid
// helper, or a replacement for the mce4d runtime CLI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

type targetRoots []string

func (t *targetRoots) String() string { return "approved local root" }
func (t *targetRoots) Set(s string) error {
	if len(*t) >= 64 {
		return fmt.Errorf("too many roots")
	}
	*t = append(*t, s)
	return nil
}

func run(ctx context.Context, args []string) int {
	if handled, code := sqlite.RunProtectedWorker(ctx, args, os.Stdout); handled {
		return code
	}
	if handled, code := runPlatformProfile(ctx, args); handled {
		return code
	}
	if len(args) < 1 {
		return 2
	}
	flags := flag.NewFlagSet("state_profile", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "")
	engine := flags.String("engine-uid", "", "")
	group := flags.String("engine-gid", "", "")
	client := flags.String("client-uid", "", "")
	worktree := flags.String("worktree-id", "", "")
	worker := flags.String("worker-executable", "", "")
	var targets targetRoots
	flags.Var(&targets, "target-root", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *root == "" {
		return 2
	}
	var summary sqlite.ProtectedProfileSummary
	var err error
	switch args[0] {
	case "create":
		if *worktree != "" {
			return 2
		}
		u, e1 := strconv.ParseUint(*engine, 10, 32)
		g, e2 := strconv.ParseUint(*group, 10, 32)
		c, e3 := strconv.ParseUint(*client, 10, 32)
		if e1 != nil || e2 != nil || e3 != nil || len(targets) == 0 {
			return 2
		}
		summary, err = sqlite.ProvisionProtectedProfile(ctx, sqlite.ProtectedProfileOptions{Root: *root, EngineUID: uint32(u), EngineGID: uint32(g), ClientUID: uint32(c), TargetRoots: targets, WorkerExecutable: *worker})
	case "check":
		if *engine != "" || *group != "" || *client != "" || len(targets) != 0 || *worktree != "" || *worker != "" {
			return 2
		}
		summary, err = sqlite.CheckProtectedProfile(ctx, *root)
	case "worktree-create":
		if *engine != "" || *group != "" || *client != "" || len(targets) != 0 || *worktree == "" || *worker != "" {
			return 2
		}
		worktreeSummary, createErr := sqlite.ProvisionProtectedWorktree(ctx, *root, *worktree)
		if createErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, "protected worktree unavailable or incomplete; no automatic recovery performed")
			return 3
		}
		if json.NewEncoder(os.Stdout).Encode(worktreeSummary) != nil {
			return 5
		}
		return 0
	default:
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "protected profile unavailable or incomplete; no automatic recovery performed")
		return 3
	}
	if sqlite.EncodeProfileSummary(os.Stdout, summary) != nil {
		return 5
	}
	return 0
}

func main() {
	base, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	ctx, cancel := context.WithTimeout(base, 45*time.Second)
	code := run(ctx, os.Args[1:])
	cancel()
	stop()
	os.Exit(code)
}
