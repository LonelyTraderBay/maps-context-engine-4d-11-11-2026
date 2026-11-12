package contract

import (
	"errors"
	"testing"
)

// pairingFixture is an independently executable AL-04 reference, not a browser,
// SDK, authorization service or product reducer.
type pairingFixture struct {
	phase, mode, origin, nonce, command, expectedReply string
	doc, hmr, auth, revision, sdkSeq, deadline         int
	shield, permission, ackPending                     bool
}
type pairingEvent struct {
	kind, origin, nonce, command, replyTo string
	doc, hmr, auth, seq, now              int
}

func reducePairingFixture(s pairingFixture, e pairingEvent) (pairingFixture, error) {
	reject := func() (pairingFixture, error) { return s, errors.New("invalid transition") }
	switch e.kind {
	case "challenge":
		if s.phase != "pairing" || !s.shield {
			return reject()
		}
		s.phase = "challenge_sent"
		s.expectedReply = "challenge-id"
	case "ready":
		if s.phase != "challenge_sent" || e.origin != s.origin || e.nonce != s.nonce || e.replyTo != s.expectedReply || e.doc != s.doc || e.hmr != s.hmr || e.seq <= s.sdkSeq {
			return reject()
		}
		s.sdkSeq = e.seq
		s.phase = "ready_received"
	case "bind":
		if s.phase != "ready_received" || !s.permission {
			return reject()
		}
		s.phase = "bind_pending"
	case "bind_success":
		if s.phase != "bind_pending" {
			return reject()
		}
		s.revision++
		s.phase = "bound_ack_pending"
		s.expectedReply = "bound-id"
		s.deadline = e.now + 3
	case "bound_ack":
		if s.phase != "bound_ack_pending" || e.origin != s.origin || e.replyTo != s.expectedReply || e.doc != s.doc || e.hmr != s.hmr || e.seq <= s.sdkSeq || e.now >= s.deadline {
			return reject()
		}
		s.sdkSeq = e.seq
		s.phase = "client_ready"
	case "arm":
		if s.phase != "client_ready" || !s.permission || !s.shield || e.command == "" {
			return reject()
		}
		s.mode = "arming"
		s.auth++
		s.command = e.command
		s.expectedReply = "mode-id"
		s.deadline = e.now + 3
		s.ackPending = false
	case "mode_ack":
		if s.mode != "arming" {
			return reject()
		}
		if e.origin != s.origin || e.replyTo != s.expectedReply || e.doc != s.doc || e.hmr != s.hmr || e.auth != s.auth || e.command != s.command || e.seq <= s.sdkSeq || e.now >= s.deadline {
			s.shield = true
			s.mode = "inspect"
			s.phase = "stale"
			s.auth++
			s.command = ""
			s.ackPending = false
			return s, errors.New("invalid mode acknowledgment")
		}
		s.sdkSeq = e.seq
		s.ackPending = true
	case "server_ack":
		if s.mode != "arming" || !s.ackPending || !s.permission || e.auth != s.auth || e.command != s.command || e.now >= s.deadline {
			return reject()
		}
		s.mode = "interact"
		s.shield = false
		s.ackPending = false
		s.revision++
	case "invalidate":
		s.shield = true
		s.mode = "inspect"
		s.phase = "stale"
		s.auth++
		s.command = ""
		s.ackPending = false
	case "timeout":
		if (s.phase != "bound_ack_pending" && s.mode != "arming") || e.now < s.deadline {
			return reject()
		}
		s.shield = true
		s.mode = "inspect"
		s.phase = "stale"
		s.auth++
		s.command = ""
		s.ackPending = false
	default:
		return reject()
	}
	return s, nil
}
func TestPreviewPairingFixtureRequiresCompleteAcknowledgments(t *testing.T) {
	s := pairingFixture{phase: "pairing", mode: "inspect", origin: "http://localhost:3000", nonce: "fixture", doc: 1, permission: true, shield: true, revision: 1}
	apply := func(e pairingEvent) {
		t.Helper()
		next, err := reducePairingFixture(s, e)
		if err != nil {
			t.Fatalf("%s: %v", e.kind, err)
		}
		s = next
	}
	rejects := func(e pairingEvent) {
		t.Helper()
		next, err := reducePairingFixture(s, e)
		if err == nil || next != s {
			t.Fatalf("accepted or mutated invalid %s", e.kind)
		}
	}
	rejects(pairingEvent{kind: "ready", replyTo: "challenge-id", origin: s.origin, nonce: s.nonce, doc: 1, seq: 1})
	apply(pairingEvent{kind: "challenge"})
	rejects(pairingEvent{kind: "ready", replyTo: "challenge-id", origin: "http://evil", nonce: s.nonce, doc: 1, seq: 1})
	rejects(pairingEvent{kind: "ready", replyTo: "challenge-id", origin: s.origin, nonce: "wrong", doc: 1, seq: 1})
	rejects(pairingEvent{kind: "ready", replyTo: "wrong-id", origin: s.origin, nonce: s.nonce, doc: 1, seq: 1})
	apply(pairingEvent{kind: "ready", replyTo: "challenge-id", origin: s.origin, nonce: s.nonce, doc: 1, seq: 1})
	apply(pairingEvent{kind: "bind"})
	apply(pairingEvent{kind: "bind_success", now: 1})
	rejects(pairingEvent{kind: "arm", command: "cmd", now: 1})
	rejects(pairingEvent{kind: "bound_ack", replyTo: "bound-id", origin: s.origin, doc: 2, seq: 2, now: 2})
	rejects(pairingEvent{kind: "bound_ack", replyTo: "bound-id", origin: s.origin, doc: 1, seq: 2, now: 4})
	apply(pairingEvent{kind: "bound_ack", replyTo: "bound-id", origin: s.origin, doc: 1, seq: 2, now: 2})
	if !s.shield {
		t.Fatal("bound acknowledgment removed shield")
	}
	apply(pairingEvent{kind: "arm", command: "cmd", now: 2})
	for _, bad := range []pairingEvent{{kind: "mode_ack", replyTo: "mode-id", origin: s.origin, doc: 1, auth: s.auth, command: "wrong", seq: 3, now: 3}, {kind: "mode_ack", replyTo: "mode-id", origin: s.origin, doc: 1, auth: s.auth, command: "cmd", seq: 3, now: s.deadline}} {
		failed, err := reducePairingFixture(s, bad)
		if err == nil || !failed.shield || failed.mode != "inspect" || failed.phase != "stale" || failed.auth <= s.auth {
			t.Fatal("invalid ack did not fail closed")
		}
	}
	rejects(pairingEvent{kind: "server_ack", auth: s.auth, command: "cmd", now: 3})
	apply(pairingEvent{kind: "mode_ack", replyTo: "mode-id", origin: s.origin, doc: 1, auth: s.auth, command: "cmd", seq: 3, now: 3})
	if !s.shield {
		t.Fatal("SDK ack removed shield before server ack")
	}
	apply(pairingEvent{kind: "server_ack", auth: s.auth, command: "cmd", now: 3})
	if s.shield || s.mode != "interact" {
		t.Fatal("fresh matched acknowledgments did not arm")
	}
	oldAuth := s.auth
	apply(pairingEvent{kind: "invalidate"})
	if !s.shield || s.mode != "inspect" || s.auth <= oldAuth {
		t.Fatal("invalidation failed to shield and revoke")
	}
	rejects(pairingEvent{kind: "mode_ack", replyTo: "mode-id", origin: s.origin, doc: 1, auth: oldAuth, command: "cmd", seq: 4, now: 3})
}
func TestPreviewPairingFixtureTimeoutNeverArms(t *testing.T) {
	s := pairingFixture{phase: "bound_ack_pending", mode: "inspect", shield: true, permission: true, deadline: 3}
	next, err := reducePairingFixture(s, pairingEvent{kind: "timeout", now: 3})
	if err != nil || !next.shield || next.phase != "stale" || next.mode != "inspect" {
		t.Fatalf("timeout state %+v %v", next, err)
	}
}
