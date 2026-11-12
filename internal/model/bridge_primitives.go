package model

import (
	"encoding/json"
	"fmt"
	"math"
)

type DOMAnchorRef struct {
	BuildMapID string `json:"build_map_id"`
	AnchorID   string `json:"anchor_id"`
}
type InstanceRef struct {
	SessionID     string `json:"session_id"`
	DocumentEpoch int64  `json:"document_epoch"`
	HMREpoch      int64  `json:"hmr_epoch"`
	InstanceID    string `json:"instance_id"`
}
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func DecodeDOMAnchorRefJSON(data []byte, maxBytes int) (DOMAnchorRef, error) {
	var r DOMAnchorRef
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"build_map_id", "anchor_id"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return DOMAnchorRef{}, err
	}
	if !lowerHex(r.BuildMapID, 64) || !lowerHex(r.AnchorID, 64) {
		return DOMAnchorRef{}, fmt.Errorf("invalid DOM anchor")
	}
	return r, nil
}
func DecodeInstanceRefJSON(data []byte, maxBytes int) (InstanceRef, error) {
	var r InstanceRef
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"session_id", "document_epoch", "hmr_epoch", "instance_id"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return InstanceRef{}, err
	}
	if !uuid(r.SessionID) || r.DocumentEpoch < 1 || r.DocumentEpoch > maxExactInteger || r.HMREpoch < 0 || r.HMREpoch > maxExactInteger || !validID(r.InstanceID) {
		return InstanceRef{}, fmt.Errorf("invalid instance ref")
	}
	return r, nil
}

// DecodeRectJSON accepts finite display coordinates, not identity numbers.
// The session adapter must additionally apply a tested per-field CSS bound.
func DecodeRectJSON(data []byte, maxBytes int) (Rect, error) {
	var r Rect
	if err := ValidateWireJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"x", "y", "width", "height"}, nil); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Rect{}, err
	}
	if !finite(r.X) || !finite(r.Y) || !finite(r.Width) || !finite(r.Height) || r.Width < 0 || r.Height < 0 {
		return Rect{}, fmt.Errorf("invalid rectangle")
	}
	return r, nil
}
func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

// ValidateBounds caps each field's magnitude using an explicit caller policy.
func (r Rect) ValidateBounds(maxMagnitude float64) error {
	if !finite(maxMagnitude) || maxMagnitude <= 0 || !finite(r.X) || !finite(r.Y) || !finite(r.Width) || !finite(r.Height) || r.Width < 0 || r.Height < 0 || math.Abs(r.X) > maxMagnitude || math.Abs(r.Y) > maxMagnitude || r.Width > maxMagnitude || r.Height > maxMagnitude {
		return fmt.Errorf("rectangle outside bounds")
	}
	return nil
}
