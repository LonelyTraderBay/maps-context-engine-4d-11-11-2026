package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/cli"
	sqlite "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func main() {
	prepareNativeProcess()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	if handled, code := sqlite.RunProtectedWorker(ctx, os.Args[1:], os.Stdout); handled {
		cancel()
		os.Exit(code)
	}
	cpus := runtime.NumCPU()
	deps := cli.Dependencies{CPUs: cpus, SQLiteProbe: sqlite.ProbeMemory, LookupExecutable: exec.LookPath, ExplorerAssets: bundledExplorerAssets(),
		OpenConfig: func(ctx context.Context, configPath string) (cli.ConfigOwner, func() error, error) {
			return cli.OpenDefaultReadOnlyConfig(ctx, configPath, cpus)
		},
		ProbeConfig: func(ctx context.Context, configPath string) error {
			return cli.ProbeDefaultReadOnlyConfig(ctx, configPath, cpus)
		}}
	configureNativeDependencies(&deps)
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, deps)
	cancel()
	os.Exit(code)
}
