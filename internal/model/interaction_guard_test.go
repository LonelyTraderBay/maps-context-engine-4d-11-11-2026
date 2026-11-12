package model

import (
	"strings"
	"testing"
	"time"
)

func TestInteractionGuardFailsClosed(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	p := InteractionGuardInput{SessionID: id, DocumentEpoch: 1, HMREpoch: 2, AuthorizationEpoch: 3, PermissionVersion: 4, MinimumRevision: 5, BindingID: h, Visible: true, LastVerifiedPong: now, PongDeadline: now.Add(5 * time.Second), Now: now}
	s := PreviewStatusData{SessionID: id, DocumentEpoch: 1, HMREpoch: 2, AuthorizationEpoch: 3, PermissionVersion: 4, Revision: 5, BindingID: &h, State: "bound", ModeState: "interact", PermissionStatus: "approved", InteractionAllowed: true, ExpiresAt: "2026-11-11T01:00:00Z"}
	if MustShield(p, s) {
		t.Fatal("valid fixture shielded")
	}
	for name, change := range map[string]func(*InteractionGuardInput, *PreviewStatusData){"hidden": func(p *InteractionGuardInput, s *PreviewStatusData) { p.Visible = false }, "PONG lost": func(p *InteractionGuardInput, s *PreviewStatusData) { p.Now = p.PongDeadline }, "clock rollback": func(p *InteractionGuardInput, s *PreviewStatusData) { p.Now = now.Add(-time.Second) }, "permission revoked": func(p *InteractionGuardInput, s *PreviewStatusData) { s.PermissionStatus = "revoked" }, "changed permission": func(p *InteractionGuardInput, s *PreviewStatusData) { s.PermissionVersion++ }, "stale": func(p *InteractionGuardInput, s *PreviewStatusData) { s.State = "stale" }, "old revision": func(p *InteractionGuardInput, s *PreviewStatusData) { s.Revision-- }, "expired": func(p *InteractionGuardInput, s *PreviewStatusData) { s.ExpiresAt = "2026-11-10T23:59:59Z" }, "unbound": func(p *InteractionGuardInput, s *PreviewStatusData) { s.BindingID = nil }, "not interact": func(p *InteractionGuardInput, s *PreviewStatusData) { s.ModeState = "inspect" }} {
		t.Run(name, func(t *testing.T) {
			a, b := p, s
			change(&a, &b)
			if !MustShield(a, b) {
				t.Fatal("failed to shield")
			}
		})
	}
}
