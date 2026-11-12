package model

import (
	"testing"
	"time"
)

func TestPongRequiresPendingMatchingProbe(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	probe := "11234567-89ab-cdef-0123-456789abcdef"
	msgID := "21234567-89ab-cdef-0123-456789abcdef"
	reply := "31234567-89ab-cdef-0123-456789abcdef"
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	p := PendingSDKProbe{SessionID: id, DocumentEpoch: 1, HMREpoch: 2, ProbeID: probe, MessageID: msgID, LastSDKSequence: 3, Deadline: now.Add(time.Second)}
	msg := BridgeMessage{ProtocolVersion: 1, SessionID: id, DocumentEpoch: 1, HMREpoch: 2, MessageID: reply, ReplyTo: &msgID, Seq: 4, Type: "PONG", Payload: &BridgeProbePayload{ProbeID: probe}}
	if err := VerifyPongCorrelation(p, msg, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPongCorrelation(p, msg, p.Deadline); err == nil {
		t.Fatal("accepted late PONG")
	}
	bad := msg
	bad.ReplyTo = nil
	if err := VerifyPongCorrelation(p, bad, now); err == nil {
		t.Fatal("accepted unsolicited PONG")
	}
	bad = msg
	bad.Seq = 3
	if err := VerifyPongCorrelation(p, bad, now); err == nil {
		t.Fatal("accepted old sequence")
	}
	bad = msg
	bad.HMREpoch++
	if err := VerifyPongCorrelation(p, bad, now); err == nil {
		t.Fatal("accepted stale epoch")
	}
}
