package catalog

import (
	"context"
	"errors"
	"testing"
)

func TestVerifyReadOnlyImage(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	want, err := Initialize(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReadOnlyImage(ctx, db); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("writable connection accepted: %v", err)
	}
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if got, err := VerifyReadOnlyImage(ctx, db); err != nil || got != want {
		t.Fatalf("read-only image: %q %v", got, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := VerifyReadOnlyImage(canceled, db); got != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %q %v", got, err)
	}
	if _, err := db.Exec("PRAGMA query_only=OFF; CREATE TABLE unexpected(n INTEGER); PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if got, err := VerifyReadOnlyImage(ctx, db); got != "" || err == nil {
		t.Fatal("accepted changed schema")
	}
}
