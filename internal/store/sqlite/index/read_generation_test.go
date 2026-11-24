package index

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestReadGenerationPinsAndValidatesPublishedManifest(t *testing.T) {
	store, capture, request := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manifest := capture.Snapshot.Manifest()
	value, err := store.ReadGeneration(ctx, manifest.ProfileID, "", request.AuthorizeGeneration, 8192)
	if err != nil || value.Identity != store.identity || value.GenerationID != capture.GenerationID || value.SnapshotDigest != manifest.SnapshotDigest || value.PolicyDigest != manifest.PolicyDigest || value.ProviderDigest != manifest.ProviderDigest {
		t.Fatalf("active generation: %+v %v", value, err)
	}
	if _, err := store.ReadGeneration(ctx, manifest.ProfileID, "", request.AuthorizeGeneration, 10); !errors.Is(err, ErrCaptureLimit) {
		t.Fatalf("manifest limit ignored: %v", err)
	}
	denied, calls := errors.New("fixture revoked"), 0
	if value, err := store.ReadGeneration(ctx, manifest.ProfileID, capture.GenerationID, func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return denied
		}
		return nil
	}, 8192); !errors.Is(err, denied) || value.GenerationID != "" {
		t.Fatalf("late revocation leaked generation: %+v %v", value, err)
	}
	if _, err := store.writer.ExecContext(ctx, `DELETE FROM active_generations WHERE profile_id=?`, manifest.ProfileID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadGeneration(ctx, manifest.ProfileID, "", request.AuthorizeGeneration, 8192); !errors.Is(err, ErrSnapshotUnavailable) {
		t.Fatalf("missing active generation accepted: %v", err)
	}
	if pinned, err := store.ReadGeneration(ctx, manifest.ProfileID, capture.GenerationID, request.AuthorizeGeneration, 8192); err != nil || pinned.GenerationID != capture.GenerationID {
		t.Fatalf("pinned published generation lost: %+v %v", pinned, err)
	}
	manifest.Files[0].Path = "other.go"
	changed, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.verifyGenerationManifest(changed, value, 8192); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("changed manifest reused a prior validation proof: %v", err)
	}
}
