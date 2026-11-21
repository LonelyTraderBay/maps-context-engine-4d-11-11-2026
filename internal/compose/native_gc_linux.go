//go:build linux

package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

type NativeGCReport struct {
	KeepGenerations int
	Generations     []index.GCResult
	More            bool
}

// RunNativeGC maintains only the bound derived index. It uses the installed
// native WAL/receipt owner, shared lifecycle and the real worktree writer.
// At most sixteen generation transactions are attempted per explicit command.
func RunNativeGC(ctx context.Context, installation, selected, worktree, profile string, cpus int, apply bool) (out NativeGCReport, resultErr error) {
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	n, err := openNativeScope(startup, installation, selected, worktree, profile, cpus)
	if err != nil {
		cancel()
		return out, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, n.Close())
		if resultErr != nil {
			out = NativeGCReport{}
		}
	}()
	job, err := nativeOperationID()
	if err != nil {
		cancel()
		return out, err
	}
	_, lease, _, err := n.stores.CacheResources(startup)
	if err != nil {
		cancel()
		return out, err
	}
	wait, err := nativeMilliseconds(n.effective.Core.WriterLockWaitMs)
	if err != nil {
		cancel()
		return out, err
	}
	writer, err := runtime.AcquireWriter(startup, lease, job, wait)
	cancel()
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	store, err := n.stores.Index.Store()
	if err != nil {
		return out, err
	}
	keep := n.effective.Core.KeepGenerations
	if keep < 2 || keep > 128 {
		return out, app.ErrCoreUnsupported
	}
	request := index.GCRequest{ProfileID: profile, JobID: job, KeepGenerations: int(keep), Writer: writer, Authorize: func(ctx context.Context, w, p string) error {
		if w != worktree || p != profile {
			return ErrAuthority
		}
		return n.recheckBinding(ctx)
	}}
	plan, err := store.PlanGC(ctx, request, 16)
	if err != nil {
		return out, err
	}
	out.KeepGenerations = int(keep)
	out.More = plan.More
	out.Generations = []index.GCResult{}
	for _, generation := range plan.GenerationIDs {
		result, err := store.CollectGeneration(ctx, request, generation, apply)
		if err != nil {
			return out, err
		}
		out.Generations = append(out.Generations, result)
	}
	return out, n.recheckBinding(ctx)
}
