package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
)

func runBenchmark(ctx context.Context, args []string, stdout, stderr io.Writer, render func(context.Context, []byte) ([]byte, error)) int {
	diagnostic := func(message string, exit int) int {
		fmt.Fprintln(stderr, "mce4d: "+message)
		return exit
	}
	if len(args) != 4 || args[0] != "--suite" || args[1] == "-" || args[2] != "--output" || args[3] == "" {
		return diagnostic("bench requires a local suite JSON file and a fresh output directory", 2)
	}
	if render == nil {
		return localAdminError(stderr, app.ErrAdminUnsupported)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	raw, err := readInput(ctx, args[1], nil, 12<<20)
	if err != nil {
		if ctx.Err() != nil {
			return localAdminError(stderr, ctx.Err())
		}
		return diagnostic("benchmark suite unavailable", 2)
	}
	report, err := render(ctx, raw)
	if errors.Is(err, app.ErrCoreInput) {
		return diagnostic("invalid benchmark suite, plan or observations", 2)
	}
	if err != nil {
		return localAdminError(stderr, err)
	}
	if !json.Valid(report) {
		return diagnostic("benchmark report unavailable", 5)
	}
	output, err := filepath.Abs(args[3])
	if err != nil {
		return diagnostic("invalid artifact directory", 2)
	}
	parent, err := os.OpenRoot(filepath.Dir(output))
	if err != nil {
		return diagnostic("benchmark output parent unavailable", 3)
	}
	defer parent.Close()
	info, err := parent.Stat(".")
	if err != nil || info.Mode().Perm()&0022 != 0 {
		return diagnostic("benchmark output parent must be private to its owner", 3)
	}
	dir := filepath.Base(output)
	if err := ctx.Err(); err != nil {
		return localAdminError(stderr, err)
	}
	if err := parent.Mkdir(dir, 0700); err != nil {
		return diagnostic("benchmark output requires a fresh private directory", 3)
	}
	pending := filepath.Join(dir, ".report.pending")
	file, err := parent.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return diagnostic("benchmark report could not be written", 5)
	}
	written, writeErr := file.Write(report)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil || written != len(report) {
		return diagnostic("benchmark output incomplete", 5)
	}
	if ctx.Err() != nil {
		return localAdminError(stderr, ctx.Err())
	}
	if err := parent.Rename(pending, filepath.Join(dir, "report.json")); err != nil {
		return diagnostic("benchmark report could not be finalized", 5)
	}
	return writeLocalAdminJSON(stdout, stderr, struct {
		Report string `json:"report"`
	}{filepath.Join(output, "report.json")})
}
