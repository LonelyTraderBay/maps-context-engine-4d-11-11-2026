package records

import (
	"errors"
	"testing"
)

func TestCachedReferenceStillRejectsChangedRecordsSchema(t *testing.T) {
	ctx, db, _, _, _ := initialized(t)
	if err := validateSchema(ctx, db); err != nil {
		t.Fatalf("valid schema: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE unauthorized_fixture (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := validateSchema(ctx, db); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("cached reference accepted changed records: %v", err)
	}
}
