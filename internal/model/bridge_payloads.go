package model

import (
	"fmt"
	"math"
)

type BridgePayload interface{ bridgePayload() }
type BridgeChallengePayload struct {
	Nonce          string `json:"nonce"`
	ApprovedOrigin string `json:"approved_origin"`
}
type BridgeReadyPayload struct {
	Nonce        string   `json:"nonce"`
	BuildMapID   string   `json:"build_map_id"`
	Capabilities []string `json:"capabilities"`
}
type BridgeBoundPayload struct {
	BuildMapID         string `json:"build_map_id"`
	AuthorizationEpoch int64  `json:"authorization_epoch"`
}
type BridgePickAtPayload struct {
	XCSS          float64 `json:"x_css"`
	YCSS          float64 `json:"y_css"`
	SelectionKind string  `json:"selection_kind"`
}
type BridgeTarget struct {
	InstanceRef       InstanceRef  `json:"instance_ref"`
	AnchorRef         DOMAnchorRef `json:"anchor_ref"`
	Rects             []Rect       `json:"rects"`
	ParentInstanceRef *InstanceRef `json:"parent_instance_ref"`
}
type BridgePickResultPayload struct {
	Targets []BridgeTarget `json:"targets"`
	Status  string         `json:"status"`
}
type BridgeListInstancesPayload struct {
	AnchorRefs []DOMAnchorRef `json:"anchor_refs"`
	Limit      int64          `json:"limit"`
	Cursor     *string        `json:"cursor"`
}
type BridgeInstance struct {
	InstanceRef InstanceRef  `json:"instance_ref"`
	AnchorRef   DOMAnchorRef `json:"anchor_ref"`
	Visibility  string       `json:"visibility"`
	Rects       []Rect       `json:"rects"`
}
type BridgeInstancesPayload struct {
	Items      []BridgeInstance `json:"items"`
	NextCursor *string          `json:"next_cursor"`
	Pending    *int64           `json:"pending"`
}
type BridgeInstanceRefsPayload struct {
	InstanceRefs []InstanceRef `json:"instance_refs"`
}
type BridgeHighlightResultPayload struct {
	Matched     []InstanceRef `json:"matched"`
	Unavailable []InstanceRef `json:"unavailable"`
}
type BridgeEmptyPayload struct{}
type BridgeScrollPayload struct {
	InstanceRef InstanceRef `json:"instance_ref"`
}
type BridgeScrollResultPayload struct {
	Status string `json:"status"`
}
type BridgeSetModePayload struct {
	Mode               string `json:"mode"`
	AuthorizationEpoch int64  `json:"authorization_epoch"`
	CommandID          string `json:"command_id"`
}
type BridgeModeAckPayload struct {
	Mode               string `json:"mode"`
	AuthorizationEpoch int64  `json:"authorization_epoch"`
	CommandID          string `json:"command_id"`
	Accepted           bool   `json:"accepted"`
}
type BridgeBuildChangedPayload struct {
	BuildMapID  string `json:"build_map_id"`
	NewHMREpoch int64  `json:"new_hmr_epoch"`
}
type BridgeCapabilityStatusPayload struct {
	Capabilities []string `json:"capabilities"`
}
type BridgeErrorPayload struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}
type BridgeProbePayload struct {
	ProbeID string `json:"probe_id"`
}

func (*BridgeChallengePayload) bridgePayload()        {}
func (*BridgeReadyPayload) bridgePayload()            {}
func (*BridgeBoundPayload) bridgePayload()            {}
func (*BridgePickAtPayload) bridgePayload()           {}
func (*BridgePickResultPayload) bridgePayload()       {}
func (*BridgeListInstancesPayload) bridgePayload()    {}
func (*BridgeInstancesPayload) bridgePayload()        {}
func (*BridgeInstanceRefsPayload) bridgePayload()     {}
func (*BridgeHighlightResultPayload) bridgePayload()  {}
func (*BridgeEmptyPayload) bridgePayload()            {}
func (*BridgeScrollPayload) bridgePayload()           {}
func (*BridgeScrollResultPayload) bridgePayload()     {}
func (*BridgeSetModePayload) bridgePayload()          {}
func (*BridgeModeAckPayload) bridgePayload()          {}
func (*BridgeBuildChangedPayload) bridgePayload()     {}
func (*BridgeCapabilityStatusPayload) bridgePayload() {}
func (*BridgeErrorPayload) bridgePayload()            {}
func (*BridgeProbePayload) bridgePayload()            {}

// ValidateBounds caps each pick coordinate using an explicit caller policy.
func (p BridgePickAtPayload) ValidateBounds(maxMagnitude float64) error {
	if !finite(maxMagnitude) || maxMagnitude <= 0 || !finite(p.XCSS) || !finite(p.YCSS) || math.Abs(p.XCSS) > maxMagnitude || math.Abs(p.YCSS) > maxMagnitude {
		return fmt.Errorf("pick outside bounds")
	}
	return nil
}
