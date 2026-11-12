package model

import (
	"encoding/json"
	"fmt"
)

func decodeBridgePayload(kind string, data []byte, maxBytes int) (BridgePayload, error) {
	var out BridgePayload
	var keys []string
	nullable := map[string]bool{}
	switch kind {
	case "CHALLENGE":
		keys = []string{"nonce", "approved_origin"}
		out = &BridgeChallengePayload{}
	case "READY":
		keys = []string{"nonce", "build_map_id", "capabilities"}
		out = &BridgeReadyPayload{}
	case "BOUND", "BOUND_ACK":
		keys = []string{"build_map_id", "authorization_epoch"}
		out = &BridgeBoundPayload{}
	case "PICK_AT":
		keys = []string{"x_css", "y_css", "selection_kind"}
		out = &BridgePickAtPayload{}
	case "PICK_RESULT":
		keys = []string{"targets", "status"}
		out = &BridgePickResultPayload{}
	case "LIST_INSTANCES":
		keys = []string{"anchor_refs", "limit", "cursor"}
		nullable["cursor"] = true
		out = &BridgeListInstancesPayload{}
	case "INSTANCES":
		keys = []string{"items", "next_cursor", "pending"}
		nullable["next_cursor"] = true
		nullable["pending"] = true
		out = &BridgeInstancesPayload{}
	case "HIGHLIGHT", "INSTANCE_INVALIDATED":
		keys = []string{"instance_refs"}
		out = &BridgeInstanceRefsPayload{}
	case "HIGHLIGHT_RESULT":
		keys = []string{"matched", "unavailable"}
		out = &BridgeHighlightResultPayload{}
	case "CLEAR_HIGHLIGHT", "DISPOSE", "DISPOSED":
		keys = []string{}
		out = &BridgeEmptyPayload{}
	case "REQUEST_SCROLL":
		keys = []string{"instance_ref"}
		out = &BridgeScrollPayload{}
	case "SCROLL_RESULT":
		keys = []string{"status"}
		out = &BridgeScrollResultPayload{}
	case "SET_MODE":
		keys = []string{"mode", "authorization_epoch", "command_id"}
		out = &BridgeSetModePayload{}
	case "MODE_ACK":
		keys = []string{"mode", "authorization_epoch", "command_id", "accepted"}
		out = &BridgeModeAckPayload{}
	case "BUILD_CHANGED":
		keys = []string{"build_map_id", "new_hmr_epoch"}
		out = &BridgeBuildChangedPayload{}
	case "CAPABILITY_STATUS":
		keys = []string{"capabilities"}
		out = &BridgeCapabilityStatusPayload{}
	case "ERROR":
		keys = []string{"code", "reason"}
		out = &BridgeErrorPayload{}
	case "PING", "PONG":
		keys = []string{"probe_id"}
		out = &BridgeProbePayload{}
	default:
		return nil, fmt.Errorf("unknown bridge message type")
	}
	f, err := scopeObject(data, keys, nullable)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, err
	}
	validEpoch := func(n int64) bool { return n >= 0 && n <= maxExactInteger }
	validMode := func(s string) bool { return s == "inspect" || s == "interact" }
	validCursor := func(s *string) bool { return s == nil || boundedString(*s, 1024) }
	switch p := out.(type) {
	case *BridgeChallengePayload:
		if !boundedString(p.Nonce, 64) || !boundedString(p.ApprovedOrigin, 2048) {
			return nil, fmt.Errorf("invalid challenge")
		}
	case *BridgeReadyPayload:
		if !boundedString(p.Nonce, 64) || !lowerHex(p.BuildMapID, 64) {
			return nil, fmt.Errorf("invalid ready")
		}
		if err := bridgeIDs(f["capabilities"], 64); err != nil {
			return nil, err
		}
	case *BridgeBoundPayload:
		if !lowerHex(p.BuildMapID, 64) || !validEpoch(p.AuthorizationEpoch) {
			return nil, fmt.Errorf("invalid bound")
		}
	case *BridgePickAtPayload:
		if !finite(p.XCSS) || !finite(p.YCSS) || (p.SelectionKind != "element" && p.SelectionKind != "component") {
			return nil, fmt.Errorf("invalid pick")
		}
	case *BridgePickResultPayload:
		if err := bridgeArray(f["targets"], 32, func(item json.RawMessage) error { return bridgeTarget(item, maxBytes, true) }); err != nil {
			return nil, err
		}
		switch p.Status {
		case "exact_in_supported_scope", "ancestor_fallback", "unmapped", "unsupported":
		default:
			return nil, fmt.Errorf("invalid pick status")
		}
	case *BridgeListInstancesPayload:
		if err := bridgeAnchors(f["anchor_refs"], 100, maxBytes); err != nil {
			return nil, err
		}
		if p.Limit < 1 || p.Limit > 100 || !validCursor(p.Cursor) {
			return nil, fmt.Errorf("invalid list instances")
		}
	case *BridgeInstancesPayload:
		if err := bridgeArray(f["items"], 100, func(item json.RawMessage) error { return bridgeTarget(item, maxBytes, false) }); err != nil {
			return nil, err
		}
		if !validCursor(p.NextCursor) || !optionalRange(p.Pending, 0, maxExactInteger) {
			return nil, fmt.Errorf("invalid instances")
		}
	case *BridgeInstanceRefsPayload:
		if err := bridgeInstances(f["instance_refs"], 100, maxBytes); err != nil {
			return nil, err
		}
	case *BridgeHighlightResultPayload:
		if err := bridgeInstances(f["matched"], 100, maxBytes); err != nil {
			return nil, err
		}
		if err := bridgeInstances(f["unavailable"], 100, maxBytes); err != nil {
			return nil, err
		}
	case *BridgeEmptyPayload:
	case *BridgeScrollPayload:
		if _, err := DecodeInstanceRefJSON(f["instance_ref"], maxBytes); err != nil {
			return nil, err
		}
	case *BridgeScrollResultPayload:
		switch p.Status {
		case "done", "not_permitted", "not_rendered":
		default:
			return nil, fmt.Errorf("invalid scroll status")
		}
	case *BridgeSetModePayload:
		if !validMode(p.Mode) || !validEpoch(p.AuthorizationEpoch) || !uuid(p.CommandID) {
			return nil, fmt.Errorf("invalid mode command")
		}
	case *BridgeModeAckPayload:
		if !validMode(p.Mode) || !validEpoch(p.AuthorizationEpoch) || !uuid(p.CommandID) {
			return nil, fmt.Errorf("invalid mode acknowledgement")
		}
	case *BridgeBuildChangedPayload:
		if !lowerHex(p.BuildMapID, 64) || p.NewHMREpoch < 1 || p.NewHMREpoch > maxExactInteger {
			return nil, fmt.Errorf("invalid build change")
		}
	case *BridgeCapabilityStatusPayload:
		if err := bridgeIDs(f["capabilities"], 64); err != nil {
			return nil, err
		}
	case *BridgeErrorPayload:
		if !validID(p.Code) || !boundedString(p.Reason, 512) {
			return nil, fmt.Errorf("invalid bridge error")
		}
	case *BridgeProbePayload:
		if !uuid(p.ProbeID) {
			return nil, fmt.Errorf("invalid liveness probe")
		}
	}
	return out, nil
}
