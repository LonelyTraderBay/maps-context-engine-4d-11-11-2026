package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func TestPreviewPermissionCASAndReadExpiry(t *testing.T) {
	ctx := context.Background()
	db := leasedCatalogDB(t)
	if _, err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;
 INSERT INTO repositories VALUES('fixture','/fixture/.git','common-id','now');`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO worktrees VALUES(?,'fixture','/fixture','root-id',1,'registered','now','now')`, catalogOperation); err != nil {
		t.Fatal(err)
	}
	store, err := OpenExistingLeased(ctx, db, &lifecycleFixture{mode: ports.SharedLease})
	if err != nil {
		t.Fatal(err)
	}
	authorized := func(ctx context.Context) error { return ctx.Err() }
	writer, err := store.WithAuthorizedMutation(ctx, &mutationFixture{}, catalogOperation, authorized)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	id, reason := "fixture-preview", "Synthetic operator revocation"
	zero, one := int64(0), int64(1)
	profile := model.PreviewPermissionProfile{SchemaVersion: 1, ProfileID: id, ApprovedOrigin: "http://127.0.0.1:9001",
		ApprovedPathPrefix: "/app", TargetEnvironment: "local_mock", SourceWorktreeIDs: []string{catalogOperation},
		AllowAttach: true, AllowedDevDestinations: []string{}, AttestationID: "fixture-attestation", AttestedBy: "fixture-owner",
		AttestedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339), Version: 1}
	put := model.PreviewPermissionAdminRequest{Action: "put", Profile: &profile, ExpectedVersion: &zero}
	if _, err := writer.ChangePreviewPermission(ctx, put, "wrong-owner", now); !errors.Is(err, ErrPermissionInput) {
		t.Fatalf("untrusted attestation: %v", err)
	}
	first, err := writer.ChangePreviewPermission(ctx, put, "fixture-owner", now)
	if err != nil || first.EffectiveStatus != "approved" || first.Version != 1 {
		t.Fatalf("put: %+v %v", first, err)
	}
	if _, err := writer.ChangePreviewPermission(ctx, put, "fixture-owner", now); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale CAS: %v", err)
	}
	for i := 0; i < 3; i++ {
		expired, err := store.ReadPreviewPermission(ctx, id, now.Add(2*time.Minute), authorized)
		if err != nil || expired.Version != 1 || expired.EffectiveStatus != "expired" || expired.Digest != first.Digest {
			t.Fatalf("expiry read changed head: %+v %v", expired, err)
		}
	}
	revoke := model.PreviewPermissionAdminRequest{Action: "revoke", ProfileID: &id, ExpectedVersion: &one, Reason: &reason}
	checks := 0
	revokedWriter, err := store.WithAuthorizedMutation(ctx, &mutationFixture{}, catalogOperation, func(context.Context) error {
		checks++
		if checks == 2 {
			return errFixtureRevoked
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revokedWriter.ChangePreviewPermission(ctx, revoke, "fixture-owner", now); !errors.Is(err, errFixtureRevoked) {
		t.Fatalf("lost-write authority: %v", err)
	}
	last, err := writer.ChangePreviewPermission(ctx, revoke, "fixture-owner", now)
	if err != nil || last.EffectiveStatus != "revoked" || last.Version != 2 || last.Digest == first.Digest {
		t.Fatalf("revoke: %+v %v", last, err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM preview_permission_versions WHERE profile_id=?", id).Scan(&count); err != nil || count != 2 {
		t.Fatalf("history or rollback: %d %v", count, err)
	}
}
