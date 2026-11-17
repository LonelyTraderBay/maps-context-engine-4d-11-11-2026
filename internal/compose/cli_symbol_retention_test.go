package compose

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCLIPackWorkIsBoundedAndCancelable(t *testing.T) {
	backend := &CoreBackend{buildGate: make(chan struct{}, 1)}
	release, err := backend.acquireCLIPackWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := backend.acquireCLIPackWork(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrent materialization did not wait for the gate: %v", err)
	}
	release()
	next, err := backend.acquireCLIPackWork(context.Background())
	if err != nil {
		t.Fatalf("released gate remained unavailable: %v", err)
	}
	next()
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := backend.acquireCLIPackWork(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled materialization acquired the gate: %v", err)
	}
}
