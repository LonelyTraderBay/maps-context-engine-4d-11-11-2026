package schemaref

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCacheDoesNotRetainErrorsOrCanceledBuilds(t *testing.T) {
	var cache Cache[string]
	failed := errors.New("fixture build failure")
	if _, err := cache.Get(context.Background(), 1, func(context.Context, int) ([]string, error) {
		return nil, failed
	}); !errors.Is(err, failed) {
		t.Fatalf("build error lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if value, err := cache.Get(ctx, 1, func(context.Context, int) ([]string, error) {
		cancel()
		return []string{"canceled"}, nil
	}); !errors.Is(err, context.Canceled) || value != nil {
		t.Fatalf("canceled build was accepted: %v", err)
	}
	value, err := cache.Get(context.Background(), 1, func(context.Context, int) ([]string, error) {
		return []string{"valid"}, nil
	})
	if err != nil || len(value) != 1 || value[0] != "valid" {
		t.Fatalf("retry after failed build: %v", err)
	}
	value[0] = "modified"
	value, err = cache.Get(context.Background(), 1, func(context.Context, int) ([]string, error) {
		t.Fatal("cached reference was rebuilt")
		return nil, nil
	})
	if err != nil || value[0] != "valid" {
		t.Fatalf("caller modified cached data: %v", err)
	}
}

func TestCacheWaitHonorsCancellationAndBuildsOnce(t *testing.T) {
	var cache Cache[int]
	started := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		_, err := cache.Get(context.Background(), 1, func(context.Context, int) ([]int, error) {
			close(started)
			<-release
			return []int{1}, nil
		})
		first <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		_, err := cache.Get(ctx, 1, func(context.Context, int) ([]int, error) {
			return []int{2}, nil
		})
		second <- err
	}()
	cancel()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting caller ignored cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("waiting caller did not stop before the first build")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if value, err := cache.Get(context.Background(), 1, func(context.Context, int) ([]int, error) {
		t.Fatal("successful reference was rebuilt")
		return nil, nil
	}); err != nil || len(value) != 1 || value[0] != 1 {
		t.Fatalf("cached reference changed: %v", err)
	}
}
