package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func partitionBatchFixture(t *testing.T, n int) (context.Context, PartitionBatchInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	items := []PartitionWork{}
	for i := n - 1; i >= 0; i-- {
		items = append(items, PartitionWork{ID: fmt.Sprintf("unit-%03d", i), EstimatedMemoryBytes: 100})
	}
	return ctx, PartitionBatchInput{Items: items, MaxWorkers: 4, MaxEstimatedMemoryBytes: 200, MaxResultBytes: 4096, MaxTotalResultBytes: 65536, Scanner: scanner, Authorize: func(context.Context) error { return nil }, Run: func(_ context.Context, item PartitionWork, w io.Writer) error {
		_, err := io.WriteString(w, item.ID)
		return err
	}}
}

func TestPartitionBatchBoundsWorkersAndOrdersResults(t *testing.T) {
	ctx, input := partitionBatchFixture(t, 10)
	var running, peak atomic.Int64
	input.Run = func(ctx context.Context, item PartitionWork, w io.Writer) error {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
		_, err := io.WriteString(w, item.ID)
		return err
	}
	result, err := RunPartitionBatch(ctx, input)
	if err != nil || len(result.Results) != 10 || result.PeakWorkers > 2 || result.PeakEstimatedMemoryBytes > 200 || peak.Load() > 2 || peak.Load() < 2 || running.Load() != 0 {
		t.Fatalf("bad worker bounds: %+v peak=%d %v", result, peak.Load(), err)
	}
	for i, item := range result.Results {
		if item.ID != fmt.Sprintf("unit-%03d", i) || string(item.Data) != item.ID {
			t.Fatalf("non-deterministic output: %+v", item)
		}
	}
}

func TestPartitionBatchHonorsWorkerCountSeparatelyFromMemory(t *testing.T) {
	ctx, input := partitionBatchFixture(t, 8)
	input.MaxWorkers, input.MaxEstimatedMemoryBytes = 2, 1000
	var running, peak atomic.Int64
	input.Run = func(ctx context.Context, item PartitionWork, w io.Writer) error {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
		_, err := io.WriteString(w, item.ID)
		return err
	}
	result, err := RunPartitionBatch(ctx, input)
	if err != nil || result.PeakWorkers > 2 || peak.Load() > 2 || peak.Load() < 2 {
		t.Fatalf("worker count exceeded: %+v peak=%d %v", result, peak.Load(), err)
	}
}

func TestPartitionBatchFailsClosedOnWorkerOutputAndRevocation(t *testing.T) {
	ctx, input := partitionBatchFixture(t, 3)
	input.Items[0].EstimatedMemoryBytes = input.MaxEstimatedMemoryBytes + 1
	if result, err := RunPartitionBatch(ctx, input); !errors.Is(err, ErrPartitionLimit) || len(result.Results) != 0 {
		t.Fatalf("oversized partition was not rejected: %v", err)
	}
	ctx, input = partitionBatchFixture(t, 3)
	input.Run = func(_ context.Context, item PartitionWork, w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		if item.ID == "unit-001" {
			return errors.New("private source")
		}
		return nil
	}
	if result, err := RunPartitionBatch(ctx, input); !errors.Is(err, ErrPartitionWorker) || len(result.Results) != 0 || strings.Contains(err.Error(), "private") {
		t.Fatalf("worker failure leaked data: %v", err)
	}
	ctx, input = partitionBatchFixture(t, 1)
	input.MaxResultBytes = 4
	input.Run = func(_ context.Context, _ PartitionWork, w io.Writer) error {
		_, _ = io.WriteString(w, "too long")
		return nil
	}
	if result, err := RunPartitionBatch(ctx, input); !errors.Is(err, ErrPartitionLimit) || len(result.Results) != 0 {
		t.Fatalf("ignored writer error accepted: %v", err)
	}
	ctx, input = partitionBatchFixture(t, 3)
	input.MaxResultBytes = 8
	input.MaxTotalResultBytes = 10
	input.Run = func(_ context.Context, _ PartitionWork, w io.Writer) error {
		_, err := io.WriteString(w, "123456")
		return err
	}
	if result, err := RunPartitionBatch(ctx, input); !errors.Is(err, ErrPartitionLimit) || len(result.Results) != 0 {
		t.Fatalf("total budget leaked partial results: %v", err)
	}
	ctx, input = partitionBatchFixture(t, 1)
	input.Run = func(_ context.Context, _ PartitionWork, w io.Writer) error {
		_, err := io.WriteString(w, "api_key=abcdefghijklmnop")
		return err
	}
	if result, err := RunPartitionBatch(ctx, input); !errors.Is(err, policy.ErrSecretBlocked) || len(result.Results) != 0 {
		t.Fatalf("secret output returned: %v", err)
	}
	ctx, input = partitionBatchFixture(t, 1)
	checks := 0
	ran := false
	input.Run = func(_ context.Context, item PartitionWork, w io.Writer) error {
		ran = true
		_, err := io.WriteString(w, item.ID)
		return err
	}
	input.Authorize = func(context.Context) error {
		checks++
		if checks == 3 {
			return errors.New("private authority")
		}
		return nil
	}
	if result, err := RunPartitionBatch(ctx, input); !errors.Is(err, ErrInvalidationAuthority) || len(result.Results) != 0 || !ran || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked batch returned data: %v", err)
	}
	ctx, input = partitionBatchFixture(t, 1)
	input.Run = func(context.Context, PartitionWork, io.Writer) error { panic("private panic") }
	if result, err := RunPartitionBatch(ctx, input); !errors.Is(err, ErrPartitionWorker) || len(result.Results) != 0 {
		t.Fatalf("panic escaped scheduler: %v", err)
	}
}

func TestPartitionBatchCancellationWaitsForWorkers(t *testing.T) {
	parent, input := partitionBatchFixture(t, 2)
	ctx, cancel := context.WithCancel(parent)
	started := make(chan struct{}, 2)
	var running atomic.Int64
	input.Run = func(ctx context.Context, _ PartitionWork, _ io.Writer) error {
		running.Add(1)
		defer running.Add(-1)
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	type outcome struct {
		result PartitionBatchResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := RunPartitionBatch(ctx, input); done <- outcome{r, e} }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-parent.Done():
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case out := <-done:
		if !errors.Is(out.err, context.Canceled) || len(out.result.Results) != 0 || running.Load() != 0 {
			t.Fatalf("workers outlived cancellation: %v", out.err)
		}
	case <-parent.Done():
		t.Fatal("cancellation did not complete")
	}
}

func TestPartitionBatchesDoNotUseGlobalWorkerLock(t *testing.T) {
	ctx, first := partitionBatchFixture(t, 1)
	_, second := partitionBatchFixture(t, 1)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	run := func(ctx context.Context, _ PartitionWork, w io.Writer) error {
		started <- struct{}{}
		select {
		case <-release:
			_, err := io.WriteString(w, "ok")
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	first.Run, second.Run = run, run
	errs := make(chan error, 2)
	for _, batch := range []PartitionBatchInput{first, second} {
		wg.Add(1)
		go func(batch PartitionBatchInput) { defer wg.Done(); _, err := RunPartitionBatch(ctx, batch); errs <- err }(batch)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("independent batch blocked by global lock")
		}
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
