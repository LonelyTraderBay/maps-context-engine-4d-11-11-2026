package model

import "time"

// InteractionGuardInput is trusted local state. SDK liveness is independent of
// server status; clocks must carry monotonic readings from the owning runtime.
type InteractionGuardInput struct {
	SessionID          string
	DocumentEpoch      int64
	HMREpoch           int64
	AuthorizationEpoch int64
	PermissionVersion  int64
	MinimumRevision    int64
	BindingID          string
	Visible            bool
	LastVerifiedPong   time.Time
	PongDeadline       time.Time
	Now                time.Time
}

// MustShield is a fail-closed decision only; callers must apply shield before
// issuing disarm and authenticate status, PONG, origin, window and clock inputs.
func MustShield(p InteractionGuardInput, s PreviewStatusData) bool {
	if !uuid(p.SessionID) || !positiveVersion(p.DocumentEpoch) || !nonnegativeVersion(p.HMREpoch) || !nonnegativeVersion(p.AuthorizationEpoch) || !positiveVersion(p.PermissionVersion) || !positiveVersion(p.MinimumRevision) || !lowerHex(p.BindingID, 64) || !p.Visible || p.Now.IsZero() || p.LastVerifiedPong.IsZero() || p.PongDeadline.IsZero() || p.Now.Before(p.LastVerifiedPong) || !p.Now.Before(p.PongDeadline) {
		return true
	}
	if s.SessionID != p.SessionID || s.DocumentEpoch != p.DocumentEpoch || s.HMREpoch != p.HMREpoch || s.AuthorizationEpoch != p.AuthorizationEpoch || s.PermissionVersion != p.PermissionVersion || s.Revision < p.MinimumRevision || s.State != "bound" || s.ModeState != "interact" || s.PermissionStatus != "approved" || !s.InteractionAllowed || s.BindingID == nil || *s.BindingID != p.BindingID {
		return true
	}
	expiry, err := utcTimestamp(s.ExpiresAt)
	return err != nil || !p.Now.Before(expiry)
}
