package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type BridgeMessage struct {
	ProtocolVersion int64         `json:"protocol_version"`
	SessionID       string        `json:"session_id"`
	DocumentEpoch   int64         `json:"document_epoch"`
	HMREpoch        int64         `json:"hmr_epoch"`
	MessageID       string        `json:"message_id"`
	ReplyTo         *string       `json:"reply_to"`
	SelectionID     *string       `json:"selection_id"`
	Seq             int64         `json:"seq"`
	Type            string        `json:"type"`
	Payload         BridgePayload `json:"payload"`
}

// DecodeBridgeMessageJSON enforces the closed protocol shape and hard byte cap.
// It does not authorize a peer or establish origin, session, sequence or ACK state.
func DecodeBridgeMessageJSON(data []byte, maxBytes int) (BridgeMessage, error) {
	var result BridgeMessage
	if maxBytes > 16384 {
		maxBytes = 16384
	}
	if err := ValidateWireJSON(data, maxBytes); err != nil {
		return result, err
	}
	f, err := scopeObject(data, []string{"protocol_version", "session_id", "document_epoch", "hmr_epoch", "message_id", "reply_to", "selection_id", "seq", "type", "payload"}, map[string]bool{"reply_to": true, "selection_id": true})
	if err != nil {
		return result, err
	}
	payload := f["payload"]
	delete(f, "payload")
	raw, err := json.Marshal(f)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return BridgeMessage{}, err
	}
	if result.ProtocolVersion != 1 || !uuid(result.SessionID) || !uuid(result.MessageID) || result.DocumentEpoch < 1 || result.DocumentEpoch > maxExactInteger || result.HMREpoch < 0 || result.HMREpoch > maxExactInteger || result.Seq < 1 || result.Seq > maxExactInteger || result.ReplyTo != nil && !uuid(*result.ReplyTo) || !optionalID(result.SelectionID) {
		return BridgeMessage{}, fmt.Errorf("invalid bridge envelope")
	}
	result.Payload, err = decodeBridgePayload(result.Type, payload, maxBytes)
	if err != nil {
		return BridgeMessage{}, err
	}
	if picked, ok := result.Payload.(*BridgePickResultPayload); ok {
		for _, target := range picked.Targets {
			if !bridgeInstanceMatches(result, target.InstanceRef) || target.ParentInstanceRef != nil && !bridgeInstanceMatches(result, *target.ParentInstanceRef) {
				return BridgeMessage{}, fmt.Errorf("stale picked instance")
			}
		}
	}
	return result, nil
}
func bridgeInstanceMatches(msg BridgeMessage, ref InstanceRef) bool {
	return msg.SessionID == ref.SessionID && msg.DocumentEpoch == ref.DocumentEpoch && msg.HMREpoch == ref.HMREpoch
}
func bridgeArray(raw json.RawMessage, max int, check func(json.RawMessage) error) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	if items == nil || len(items) > max {
		return fmt.Errorf("invalid bridge array")
	}
	for _, item := range items {
		if err := check(item); err != nil {
			return err
		}
	}
	return nil
}
func bridgeIDs(raw json.RawMessage, max int) error {
	return bridgeArray(raw, max, func(item json.RawMessage) error {
		var id string
		if err := json.Unmarshal(item, &id); err != nil {
			return err
		}
		if !validID(id) {
			return fmt.Errorf("invalid bridge ID")
		}
		return nil
	})
}
func bridgeInstances(raw json.RawMessage, max int, maxBytes int) error {
	return bridgeArray(raw, max, func(item json.RawMessage) error { _, err := DecodeInstanceRefJSON(item, maxBytes); return err })
}
func bridgeAnchors(raw json.RawMessage, max int, maxBytes int) error {
	return bridgeArray(raw, max, func(item json.RawMessage) error { _, err := DecodeDOMAnchorRefJSON(item, maxBytes); return err })
}
func bridgeRects(raw json.RawMessage, maxBytes int) error {
	return bridgeArray(raw, 16, func(item json.RawMessage) error { _, err := DecodeRectJSON(item, maxBytes); return err })
}
func bridgeTarget(raw json.RawMessage, maxBytes int, parent bool) error {
	fields := []string{"instance_ref", "anchor_ref", "rects"}
	nullable := map[string]bool{}
	if parent {
		fields = append(fields, "parent_instance_ref")
		nullable["parent_instance_ref"] = true
	} else {
		fields = append(fields, "visibility")
	}
	f, err := scopeObject(raw, fields, nullable)
	if err != nil {
		return err
	}
	if _, err := DecodeInstanceRefJSON(f["instance_ref"], maxBytes); err != nil {
		return err
	}
	if _, err := DecodeDOMAnchorRefJSON(f["anchor_ref"], maxBytes); err != nil {
		return err
	}
	if err := bridgeRects(f["rects"], maxBytes); err != nil {
		return err
	}
	if parent && !bytes.Equal(bytes.TrimSpace(f["parent_instance_ref"]), []byte("null")) {
		if _, err := DecodeInstanceRefJSON(f["parent_instance_ref"], maxBytes); err != nil {
			return err
		}
	}
	if !parent {
		var v string
		if err := json.Unmarshal(f["visibility"], &v); err != nil {
			return err
		}
		switch v {
		case "visible", "mounted_offscreen", "hidden", "not_rendered", "detached", "unknown":
		default:
			return fmt.Errorf("invalid visibility")
		}
	}
	return nil
}
