package indexer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

var ErrPartitionWorker = errors.New("partition worker failed")

type PartitionWork struct {
	ID                   string
	EstimatedMemoryBytes int64
}
type PartitionWorkResult struct {
	ID   string
	Data []byte
}
type PartitionBatchInput struct {
	Items                   []PartitionWork
	MaxWorkers              int
	MaxEstimatedMemoryBytes int64
	MaxResultBytes          int
	MaxTotalResultBytes     int
	Scanner                 *policy.Scanner
	Authorize               func(context.Context) error
	// Run is trusted engine code and must stop on ctx cancellation. This
	// scheduler is not a sandbox and cannot terminate a stuck goroutine.
	Run func(context.Context, PartitionWork, io.Writer) error
}
type PartitionBatchResult struct {
	Results                  []PartitionWorkResult
	PeakWorkers              int
	PeakEstimatedMemoryBytes int64
}

type partitionOutcome struct {
	item PartitionWork
	data []byte
	err  error
}
type boundedPartitionWriter struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (w *boundedPartitionWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.buffer.Len() {
		w.exceeded = true
		return 0, ErrPartitionLimit
	}
	return w.buffer.Write(p)
}

func runPartition(ctx context.Context, input PartitionBatchInput, item PartitionWork) (out partitionOutcome) {
	out.item = item
	defer func() {
		if recover() != nil {
			out.data = nil
			out.err = ErrPartitionWorker
		}
	}()
	writer := &boundedPartitionWriter{limit: input.MaxResultBytes}
	if err := input.Run(ctx, item, writer); err != nil {
		if ctx.Err() != nil {
			out.err = ctx.Err()
		} else {
			out.err = ErrPartitionWorker
		}
		return out
	}
	if writer.exceeded {
		out.err = ErrPartitionLimit
		return out
	}
	if err := input.Scanner.CheckContent(writer.buffer.Bytes()); err != nil {
		out.err = err
		return out
	}
	out.data = bytes.Clone(writer.buffer.Bytes())
	return out
}

// RunPartitionBatch bounds worker count, output and declared in-flight memory
// estimates. It waits for every started callback before returning on failure,
// and never exposes partial results. Estimates are not an RSS limit.
func RunPartitionBatch(ctx context.Context, input PartitionBatchInput) (PartitionBatchResult, error) {
	if ctx == nil || input.Items == nil || len(input.Items) > 100000 || input.MaxWorkers < 1 || input.MaxWorkers > 64 || input.MaxEstimatedMemoryBytes < 1 || input.MaxResultBytes < 1 || input.MaxResultBytes > 16*1024*1024 || input.MaxTotalResultBytes < 1 || input.MaxTotalResultBytes > 64*1024*1024 || input.MaxResultBytes > input.MaxTotalResultBytes || input.Scanner == nil || input.Authorize == nil || input.Run == nil {
		return PartitionBatchResult{}, ErrInvalidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return PartitionBatchResult{}, ErrInvalidationInput
	}
	if err := ctx.Err(); err != nil {
		return PartitionBatchResult{}, err
	}
	if err := input.Authorize(ctx); err != nil {
		return PartitionBatchResult{}, contentReuseAuthorityError(ctx)
	}
	for _, item := range input.Items {
		if len(item.ID) > 768 {
			return PartitionBatchResult{}, ErrInvalidationInput
		}
	}
	items := append([]PartitionWork(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return PartitionBatchResult{}, err
		}
		if !validPartitionID(item.ID) || i > 0 && item.ID == items[i-1].ID || item.EstimatedMemoryBytes < 1 {
			return PartitionBatchResult{}, ErrInvalidationInput
		}
		if item.EstimatedMemoryBytes > input.MaxEstimatedMemoryBytes {
			return PartitionBatchResult{}, ErrPartitionLimit
		}
		if err := input.Scanner.CheckContent([]byte(item.ID)); err != nil {
			return PartitionBatchResult{}, err
		}
	}
	workers := input.MaxWorkers
	if workers > len(items) {
		workers = len(items)
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan PartitionWork)
	done := make(chan partitionOutcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				done <- runPartition(child, input, item)
			}
		}()
	}
	result := PartitionBatchResult{Results: []PartitionWorkResult{}}
	active, next, total := 0, 0, 0
	var inFlight int64
	var failure error
	accept := func(out partitionOutcome) {
		active--
		inFlight -= out.item.EstimatedMemoryBytes
		if failure != nil {
			return
		}
		if out.err != nil {
			if failure == nil {
				failure = out.err
				cancel()
			}
			return
		}
		if err := input.Authorize(child); err != nil {
			failure = contentReuseAuthorityError(child)
			cancel()
			return
		}
		if len(out.data) > input.MaxTotalResultBytes-total {
			if failure == nil {
				failure = ErrPartitionLimit
				cancel()
			}
			return
		}
		total += len(out.data)
		result.Results = append(result.Results, PartitionWorkResult{ID: out.item.ID, Data: out.data})
	}
	for next < len(items) || active > 0 {
		if failure != nil {
			if active == 0 {
				break
			}
			accept(<-done)
			continue
		}
		canStart := next < len(items) && active < workers && items[next].EstimatedMemoryBytes <= input.MaxEstimatedMemoryBytes-inFlight
		if canStart {
			if err := input.Authorize(child); err != nil {
				failure = contentReuseAuthorityError(child)
				cancel()
				continue
			}
			select {
			case jobs <- items[next]:
				active++
				inFlight += items[next].EstimatedMemoryBytes
				next++
				if active > result.PeakWorkers {
					result.PeakWorkers = active
				}
				if inFlight > result.PeakEstimatedMemoryBytes {
					result.PeakEstimatedMemoryBytes = inFlight
				}
			case out := <-done:
				accept(out)
			case <-ctx.Done():
				failure = ctx.Err()
				cancel()
			}
		} else {
			select {
			case out := <-done:
				accept(out)
			case <-ctx.Done():
				failure = ctx.Err()
				cancel()
			}
		}
	}
	close(jobs)
	wg.Wait()
	if failure != nil {
		return PartitionBatchResult{}, failure
	}
	if err := ctx.Err(); err != nil {
		return PartitionBatchResult{}, err
	}
	if err := input.Authorize(ctx); err != nil {
		return PartitionBatchResult{}, contentReuseAuthorityError(ctx)
	}
	sort.Slice(result.Results, func(i, j int) bool { return result.Results[i].ID < result.Results[j].ID })
	return result, nil
}
