package model

import (
	"fmt"
	"time"
)

// PendingModeAck is trusted local pairing state, not a wire credential.
type PendingModeAck struct {
	SessionID          string
	DocumentEpoch      int64
	HMREpoch           int64
	AuthorizationEpoch int64
	Revision           int64
	PermissionVersion  int64
	CommandID          string
	MessageID          string
	BuildMapID         string
	LastSDKSequence    int64
	Deadline           time.Time
}

// VerifyModeAckCorrelation rejects stale, mismatched, negative and late ACKs.
// Origin/window, direction, replay storage, SDK liveness, authenticated status,
// server CAS and shield transition must independently be enforced by the caller.
func VerifyModeAckCorrelation(p PendingModeAck, current PreviewStatusData, currentBuildMapID string, msg BridgeMessage, now time.Time) error {
	if !uuid(p.SessionID) || !uuid(p.CommandID) || !uuid(p.MessageID) || !lowerHex(p.BuildMapID, 64) || !positiveVersion(p.DocumentEpoch) || !nonnegativeVersion(p.HMREpoch) || !nonnegativeVersion(p.AuthorizationEpoch) || !positiveVersion(p.Revision) || !positiveVersion(p.PermissionVersion) || !nonnegativeVersion(p.LastSDKSequence) || p.Deadline.IsZero() || now.IsZero() || !now.Before(p.Deadline) {
		return fmt.Errorf("invalid or expired mode command")
	}
	if current.SessionID != p.SessionID || current.DocumentEpoch != p.DocumentEpoch || current.HMREpoch != p.HMREpoch || current.AuthorizationEpoch != p.AuthorizationEpoch || current.Revision != p.Revision || current.PermissionVersion != p.PermissionVersion || current.State != "bound" || current.ModeState != "arming" || current.PermissionStatus != "approved" || current.BindingID == nil || !lowerHex(*current.BindingID, 64) || currentBuildMapID != p.BuildMapID {
		return fmt.Errorf("mode command no longer current")
	}
	expires, err := utcTimestamp(current.ExpiresAt)
	if err != nil || !now.Before(expires) {
		return fmt.Errorf("permission expired")
	}
	ack, ok := msg.Payload.(*BridgeModeAckPayload)
	if !ok || ack == nil || msg.Type != "MODE_ACK" || msg.ProtocolVersion != 1 || msg.SessionID != p.SessionID || msg.DocumentEpoch != p.DocumentEpoch || msg.HMREpoch != p.HMREpoch || msg.ReplyTo == nil || *msg.ReplyTo != p.MessageID || msg.Seq <= p.LastSDKSequence || msg.Seq > maxExactInteger || !uuid(msg.MessageID) || ack.Mode != "interact" || !ack.Accepted || ack.AuthorizationEpoch != p.AuthorizationEpoch || ack.CommandID != p.CommandID {
		return fmt.Errorf("mode acknowledgement mismatch")
	}
	return nil
}
