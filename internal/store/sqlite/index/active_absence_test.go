package index

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActiveGenerationAbsenceRequiresAuthorityAndValidStore(t *testing.T) {
	store, capture, _ := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	profile := capture.Snapshot.Manifest().ProfileID
	authorize := func(context.Context, Identity, string) error { return nil }
	if absent, err := store.ActiveGenerationAbsent(ctx, profile, authorize); err != nil || absent {
		t.Fatalf("active generation reported missing: %v %v", absent, err)
	}
	if _, err := store.writer.ExecContext(ctx, `DELETE FROM active_generations WHERE profile_id=?`, profile); err != nil {
		t.Fatal(err)
	}
	if absent, err := store.ActiveGenerationAbsent(ctx, profile, authorize); err != nil || !absent {
		t.Fatalf("missing active pointer not observed: %v %v", absent, err)
	}
	denied, calls := errors.New("fixture revoked"), 0
	if absent, err := store.ActiveGenerationAbsent(ctx, profile, func(context.Context, Identity, string) error {
		calls++
		if calls == 2 {
			return denied
		}
		return nil
	}); !errors.Is(err, denied) || absent {
		t.Fatalf("late revocation leaked absence: %v %v", absent, err)
	}
	if _, err := store.writer.ExecContext(ctx, `CREATE TABLE unexpected_metadata(value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if absent, err := store.ActiveGenerationAbsent(ctx, profile, authorize); !errors.Is(err, ErrUnrecognizedStore) || absent {
		t.Fatalf("invalid store reported missing: %v %v", absent, err)
	}
}
