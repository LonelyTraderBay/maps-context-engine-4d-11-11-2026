package model

import (
	"strings"
	"testing"
	"time"
)

func TestModeAckCannotArmStaleSession(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	command := "11234567-89ab-cdef-0123-456789abcdef"
	message := "21234567-89ab-cdef-0123-456789abcdef"
	reply := "31234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	p := PendingModeAck{SessionID: id, DocumentEpoch: 1, HMREpoch: 2, AuthorizationEpoch: 3, Revision: 4, PermissionVersion: 5, CommandID: command, MessageID: message, BuildMapID: h, LastSDKSequence: 6, Deadline: now.Add(3 * time.Second)}
	s := PreviewStatusData{SessionID: id, DocumentEpoch: 1, HMREpoch: 2, AuthorizationEpoch: 3, Revision: 4, PermissionVersion: 5, State: "bound", ModeState: "arming", PermissionStatus: "approved", BindingID: &h, ExpiresAt: "2026-11-11T01:00:00Z"}
	ack := BridgeMessage{ProtocolVersion: 1, SessionID: id, DocumentEpoch: 1, HMREpoch: 2, MessageID: reply, ReplyTo: &message, Seq: 7, Type: "MODE_ACK", Payload: &BridgeModeAckPayload{Mode: "interact", AuthorizationEpoch: 3, CommandID: command, Accepted: true}}
	if err := VerifyModeAckCorrelation(p, s, h, ack, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModeAckCorrelation(p, s, h, ack, p.Deadline); err == nil {
		t.Fatal("accepted late ACK")
	}
	bad := ack
	bad.HMREpoch++
	if err := VerifyModeAckCorrelation(p, s, h, bad, now); err == nil {
		t.Fatal("accepted old document")
	}
	bad = ack
	bad.Seq = 6
	if err := VerifyModeAckCorrelation(p, s, h, bad, now); err == nil {
		t.Fatal("accepted replayed sequence")
	}
	for name, change := range map[string]func(*PreviewStatusData){
		"wrong revision":           func(s *PreviewStatusData) { s.Revision++ },
		"wrong epoch":              func(s *PreviewStatusData) { s.AuthorizationEpoch++ },
		"wrong permission version": func(s *PreviewStatusData) { s.PermissionVersion++ },
		"stale":                    func(s *PreviewStatusData) { s.State = "stale" },
		"not arming":               func(s *PreviewStatusData) { s.ModeState = "inspect" },
		"no binding":               func(s *PreviewStatusData) { s.BindingID = nil },
		"expired":                  func(s *PreviewStatusData) { s.ExpiresAt = "2026-11-10T23:59:59Z" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := s
			change(&changed)
			if err := VerifyModeAckCorrelation(p, changed, h, ack, now); err == nil {
				t.Fatal("accepted stale state")
			}
		})
	}
	if err := VerifyModeAckCorrelation(p, s, strings.Repeat("b", 64), ack, now); err == nil {
		t.Fatal("accepted different build")
	}
	bad = ack
	bad.Payload = &BridgeModeAckPayload{Mode: "interact", AuthorizationEpoch: 3, CommandID: command, Accepted: false}
	if err := VerifyModeAckCorrelation(p, s, h, bad, now); err == nil {
		t.Fatal("accepted rejected ACK")
	}
	s.PermissionStatus = "revoked"
	if err := VerifyModeAckCorrelation(p, s, h, ack, now); err == nil {
		t.Fatal("accepted revoked permission")
	}
}
