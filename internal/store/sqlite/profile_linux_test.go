//go:build linux

package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProtectedProfileManifestValidation(t *testing.T) {
	m := protectedProfileManifest{FormatVersion: 1, ProfileID: "12345678-1234-4234-8234-123456789abc", Stage: "CREATING", EngineUID: 1000, EngineGID: 1000, ClientUID: 1001, TargetRoots: []string{"/targets"}, KeyDigest: strings.Repeat("a", 64), CatalogAuthorityID: "22345678-1234-4234-8234-123456789abc", BootstrapExecutableSHA256: strings.Repeat("b", 64)}
	if err := validateProfileManifest(m); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*protectedProfileManifest){
		"root_engine":               func(m *protectedProfileManifest) { m.EngineUID = 0 },
		"same_uid":                  func(m *protectedProfileManifest) { m.ClientUID = m.EngineUID },
		"ready_without_incarnation": func(m *protectedProfileManifest) { m.Stage = "READY" },
		"draft_with_incarnation":    func(m *protectedProfileManifest) { m.CatalogIncarnation = m.ProfileID },
		"unknown_stage":             func(m *protectedProfileManifest) { m.Stage = "RECOVERED" },
		"missing_key":               func(m *protectedProfileManifest) { m.KeyDigest = "" },
		"duplicate_target":          func(m *protectedProfileManifest) { m.TargetRoots = []string{"/targets", "/targets"} },
		"relative_target":           func(m *protectedProfileManifest) { m.TargetRoots = []string{"targets"} },
		"reserved_uid":              func(m *protectedProfileManifest) { m.EngineUID = ^uint32(0) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := m
			change(&bad)
			if validateProfileManifest(bad) == nil {
				t.Fatal("accepted invalid root metadata")
			}
		})
	}
}

func TestProtectedProfileRequiresAdministrator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := ProvisionProtectedProfile(ctx, ProtectedProfileOptions{Root: "/not-created-by-unit-test"}); err == nil {
		t.Fatal("unprivileged/UID-namespace setup was accepted")
	}
}
