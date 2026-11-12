package model

import (
	"fmt"
	"time"
)

type PendingSDKProbe struct {
	SessionID       string
	DocumentEpoch   int64
	HMREpoch        int64
	ProbeID         string
	MessageID       string
	LastSDKSequence int64
	Deadline        time.Time
}

// VerifyPongCorrelation checks a pending PING reply without updating any lease.
// Authenticated origin/source, direction and replay store are caller duties.
func VerifyPongCorrelation(p PendingSDKProbe, msg BridgeMessage, now time.Time) error {
	if !uuid(p.SessionID) || !uuid(p.ProbeID) || !uuid(p.MessageID) || !positiveVersion(p.DocumentEpoch) || !nonnegativeVersion(p.HMREpoch) || !nonnegativeVersion(p.LastSDKSequence) || p.Deadline.IsZero() || now.IsZero() || !now.Before(p.Deadline) {
		return fmt.Errorf("invalid or expired probe")
	}
	payload, ok := msg.Payload.(*BridgeProbePayload)
	if !ok || payload == nil || msg.Type != "PONG" || msg.ProtocolVersion != 1 || msg.SessionID != p.SessionID || msg.DocumentEpoch != p.DocumentEpoch || msg.HMREpoch != p.HMREpoch || !uuid(msg.MessageID) || msg.ReplyTo == nil || *msg.ReplyTo != p.MessageID || msg.Seq <= p.LastSDKSequence || msg.Seq > maxExactInteger || payload.ProbeID != p.ProbeID {
		return fmt.Errorf("unmatched pong")
	}
	return nil
}
