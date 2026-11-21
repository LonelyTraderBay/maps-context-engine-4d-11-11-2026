package index

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func TestSchemaReferenceCacheCancellationVersionsAndCopies(t *testing.T) {
	var cache schemaReferenceCache
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if value, err := cache.get(canceled, 1); !errors.Is(err, context.Canceled) || value != nil {
		t.Fatalf("canceled initial build: %v", err)
	}
	first, err := cache.get(context.Background(), 1)
	if err != nil || len(first) == 0 {
		t.Fatalf("retry after cancellation: %v", err)
	}
	original := first[0]
	first[0].Name = "changed by caller"
	again, err := cache.get(context.Background(), 1)
	if err != nil || again[0] != original {
		t.Fatal("caller mutated cached reference")
	}
	second, err := cache.get(context.Background(), 2)
	if err != nil || len(second) == 0 || reflect.DeepEqual(second, again) {
		t.Fatalf("versions were not kept separate: %v", err)
	}
	if value, err := cache.get(canceled, 2); !errors.Is(err, context.Canceled) || value != nil {
		t.Fatalf("cached reference ignored cancellation: %v", err)
	}
	addon, err := cache.get(context.Background(), uiAddonSchema)
	if err != nil || len(addon) <= len(second) {
		t.Fatalf("addon schema was not built separately: %v", err)
	}
	if value, err := cache.get(context.Background(), uiAddonSchema+1); !errors.Is(err, ErrUnrecognizedStore) || value != nil {
		t.Fatalf("unsupported version accepted: %v", err)
	}
}

func TestSchemaReferenceCacheConcurrentColdStart(t *testing.T) {
	var cache schemaReferenceCache
	const readers = 4
	values := make([][]schemaObject, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for i := range values {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			values[i], errs[i] = cache.get(context.Background(), 2)
		}(i)
	}
	wg.Wait()
	for i := range values {
		if errs[i] != nil || len(values[i]) == 0 || !reflect.DeepEqual(values[0], values[i]) {
			t.Fatalf("concurrent reference %d: %v", i, errs[i])
		}
	}
	values[0][0].Name = "changed by caller"
	if values[1][0].Name == values[0][0].Name {
		t.Fatal("concurrent callers shared a mutable slice")
	}
}

func TestSchemaReferenceCacheStillChecksLiveSchema(t *testing.T) {
	db, _ := fixtureDB(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, db, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(ctx, db, 2); err != nil {
		t.Fatalf("warm reference against valid schema: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE unauthorized_fixture (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(ctx, db, 2); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("cached reference accepted changed live schema: %v", err)
	}
}
