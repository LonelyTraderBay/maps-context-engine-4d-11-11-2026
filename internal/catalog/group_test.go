package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
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
