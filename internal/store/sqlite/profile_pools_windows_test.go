package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWindowsObservationBindingKeepsFullHeadCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var bindingErr error
	var headErr error
	pools := &WindowsScopedPools{
		gate:       make(chan struct{}, 1),
		checks:     []func(context.Context) error{func(context.Context) error { return bindingErr }},
		headChecks: []func(context.Context) error{func(context.Context) error { return headErr }},
	}

	if err := pools.Check(ctx); err != nil {
		t.Fatal(err)
	}
	headErr = ErrStoreRecoveryRequired
	if err := pools.CheckObservationBinding(ctx); err != nil {
		t.Fatalf("live binding check entered a head read: %v", err)
	}
	if err := pools.Check(ctx); !errors.Is(err, ErrStoreRecoveryRequired) {
		t.Fatalf("the final check missed an invalid head: %v", err)
	}
	bindingErr = ErrProfileDenied
	if err := pools.CheckObservationBinding(ctx); !errors.Is(err, ErrProfileDenied) {
		t.Fatalf("the read callback missed a changed binding: %v", err)
	}
}
