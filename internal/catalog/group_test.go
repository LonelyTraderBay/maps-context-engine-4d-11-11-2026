package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const fixtureGroup = "44444444-4444-4444-8444-444444444444"
const fixtureNamespace = `{"environment":"local","protocol":"http","base_origin":null,"route_prefix":null,"contract_version":null}`

func TestGroupMetadataReadsValidatedStableOrder(t *testing.T) {
	s, db := seededStore(t)
	profile, err := s.SetProfile(context.Background(), body(t, model.ProfileAdminRequest{WorktreeID: fixtureWorktree, ProfileName: "default", Profile: profileFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repo_groups(group_id,name,status,revision,created_at,updated_at) VALUES(?,?,'active',1,?,?)`, fixtureGroup, "fixture", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"service-z", "service-a"} {
		if _, err := db.Exec(`INSERT INTO group_members(group_id,service_id,worktree_id,profile_id,namespace_json) VALUES(?,?,?,?,?)`, fixtureGroup, service, fixtureWorktree, profile.ProfileID, fixtureNamespace); err != nil {
			t.Fatal(err)
		}
	}
	group, err := s.GroupMetadata(context.Background(), fixtureGroup)
	if err != nil || group.Status != "active" || group.Revision != 1 || len(group.Config.Members) != 2 || group.Config.Members[0].ServiceID != "service-a" || group.Config.Members[1].ServiceID != "service-z" {
		t.Fatalf("group metadata: %+v %v", group, err)
	}
	if _, err := s.GroupMetadata(context.Background(), "55555555-5555-4555-8555-555555555555"); !errors.Is(err, ErrGroupUnavailable) {
		t.Fatalf("missing group accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE group_members SET namespace_json='{"environment":"local"}' WHERE group_id=? AND service_id='service-a'`, fixtureGroup); err != nil {
		t.Fatal(err)
	}
	if group, err := s.GroupMetadata(context.Background(), fixtureGroup); !errors.Is(err, ErrUnrecognizedStore) || group.Config.GroupID != "" {
		t.Fatalf("invalid namespace yielded group data: %v", err)
	}
}

func TestGroupCASPreservesTombstoneAndInvalidatesListRevision(t *testing.T) {
	ctx := context.Background()
	db := leasedCatalogDB(t)
	if _, err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;
INSERT INTO repositories VALUES('fixture','/fixture/.git','common-id','now');`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO worktrees VALUES(?,'fixture','/fixture','root-id',1,'registered','now','now')`, fixtureWorktree); err != nil {
		t.Fatal(err)
	}
	store, err := OpenExistingLeased(ctx, db, &lifecycleFixture{mode: ports.SharedLease})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := store.WithAuthorizedMutation(ctx, &mutationFixture{}, catalogOperation, func(ctx context.Context) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	profile, err := writer.SetProfile(ctx, body(t, model.ProfileAdminRequest{WorktreeID: fixtureWorktree, ProfileName: "typed", Profile: profileFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	member := model.GroupMemberConfig{ServiceID: "web", WorktreeID: fixtureWorktree, ProfileID: profile.ProfileID,
		Namespace: model.ServiceNamespace{Environment: "local", Protocol: "http"}}
	config := model.GroupConfig{GroupID: fixtureGroup, Name: "fixture", Members: []model.GroupMemberConfig{member}}
	zero, one, two := int64(0), int64(1), int64(2)
	now := time.Date(2026, time.November, 21, 13, 10, 0, 0, time.UTC)
	put := model.GroupAdminRequest{Action: "put", ExpectedRevision: &zero, Config: &config}
	if _, err := store.ChangeGroup(ctx, put, now); !errors.Is(err, ErrCatalogWriterRequired) {
		t.Fatalf("read-only catalog changed: %v", err)
	}
	created, err := writer.ChangeGroup(ctx, put, now)
	if err != nil || created.Revision != 1 || created.Status != "active" {
		t.Fatalf("created: %+v %v", created, err)
	}
	page, err := store.ListGroups(ctx, "", 1)
	if err != nil || page.More || len(page.Groups) != 1 || page.Groups[0].Revision != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	if _, err := writer.ChangeGroup(ctx, put, now); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale put accepted: %v", err)
	}
	put.ExpectedRevision = &one
	config.Members = append(config.Members, member)
	if _, err := writer.ChangeGroup(ctx, put, now); !errors.Is(err, ErrGroupInput) {
		t.Fatalf("duplicate service accepted: %v", err)
	}
	config.Members = config.Members[:1]
	remove := model.GroupAdminRequest{Action: "remove", ExpectedRevision: &one, GroupID: &config.GroupID}
	removed, err := writer.ChangeGroup(ctx, remove, now.Add(time.Second))
	if err != nil || removed.Revision != 2 || removed.Status != "removed" || len(removed.Config.Members) != 1 {
		t.Fatalf("tombstone did not preserve the original revision or members: %+v %v", removed, err)
	}
	last, err := store.ListGroups(ctx, "", 1)
	if err != nil || last.RevisionDigest == page.RevisionDigest || len(last.Groups) != 1 || last.Groups[0].Status != "removed" {
		t.Fatalf("list missed mutation: %+v %v", last, err)
	}
	put.ExpectedRevision = &two
	restored, err := writer.ChangeGroup(ctx, put, now.Add(2*time.Second))
	if err != nil || restored.Revision != 3 || restored.Status != "active" {
		t.Fatalf("explicit put against tombstone revision: %+v %v", restored, err)
	}
}

func TestGroupMetadataRejectsExcessMembersAndOversizedNamespace(t *testing.T) {
	s, db := seededStore(t)
	profile, err := s.SetProfile(context.Background(), body(t, model.ProfileAdminRequest{WorktreeID: fixtureWorktree, ProfileName: "default", Profile: profileFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repo_groups(group_id,name,status,revision,created_at,updated_at) VALUES(?,?,'active',1,?,?)`, fixtureGroup, "fixture", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 33; i++ {
		if _, err := db.Exec(`INSERT INTO group_members(group_id,service_id,worktree_id,profile_id,namespace_json) VALUES(?,?,?,?,?)`, fixtureGroup, fmt.Sprintf("service-%02d", i), fixtureWorktree, profile.ProfileID, fixtureNamespace); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GroupMetadata(context.Background(), fixtureGroup); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("excess members accepted: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM group_members WHERE group_id=? AND service_id!='service-00'`, fixtureGroup); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO group_members(group_id,service_id,worktree_id,profile_id,namespace_json) VALUES(?,?,?,?,?)`, fixtureGroup, "service-01", fixtureWorktree, profile.ProfileID, fixtureNamespace); err != nil {
		t.Fatal(err)
	}
	large := `{"environment":"` + strings.Repeat("x", 17000) + `","protocol":"http","base_origin":null,"route_prefix":null,"contract_version":null}`
	if _, err := db.Exec(`UPDATE group_members SET namespace_json=? WHERE group_id=? AND service_id='service-00'`, large, fixtureGroup); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GroupMetadata(context.Background(), fixtureGroup); !errors.Is(err, ErrUnrecognizedStore) {
		t.Fatalf("oversized namespace accepted: %v", err)
	}
}
