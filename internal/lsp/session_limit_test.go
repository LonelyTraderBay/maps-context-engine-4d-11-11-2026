package lsp

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestSessionLimiterConcurrentAndRelease(t *testing.T) {
	limiter, err := NewSessionLimiter(2)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 64
	start := make(chan struct{})
	results := make(chan *sessionPermit, workers)
	errorsFound := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			permit, err := limiter.acquire(context.Background())
			if err != nil {
				errorsFound <- err
				return
			}
			results <- permit
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsFound)
	permits := []*sessionPermit{}
	for permit := range results {
		permits = append(permits, permit)
	}
	if len(permits) != 2 {
		t.Fatalf("%d sessions acquired a limit of two", len(permits))
	}
	for err := range errorsFound {
		if !errors.Is(err, ErrLSPSessionLimit) {
			t.Fatal(err)
		}
	}
	for _, permit := range permits {
		permit.release()
		permit.release()
	}
	for i := 0; i < 2; i++ {
		permit, err := limiter.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		permits[i] = permit
	}
	if _, err := limiter.acquire(context.Background()); !errors.Is(err, ErrLSPSessionLimit) {
		t.Fatalf("double release increased quota: %v", err)
	}
	for _, permit := range permits {
		permit.release()
	}
}

func TestSessionLimiterDefaultMaximumAndCancellation(t *testing.T) {
	for _, invalid := range []int64{-1, 9} {
		if _, err := NewSessionLimiter(invalid); !errors.Is(err, ErrLSPSessionLimit) {
			t.Fatalf("invalid limit %d: %v", invalid, err)
		}
	}
	limiter, err := NewSessionLimiter(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire: %v", err)
	}
	permit, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.acquire(context.Background()); !errors.Is(err, ErrLSPSessionLimit) {
		t.Fatalf("default limit not one: %v", err)
	}
	permit.release()
	maximum, err := NewSessionLimiter(8)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		permit, err := maximum.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer permit.release()
	}
	if _, err := maximum.acquire(context.Background()); !errors.Is(err, ErrLSPSessionLimit) {
		t.Fatalf("maximum limit not enforced: %v", err)
	}
}
