package model

import "fmt"

type BridgeDirection string

const (
	ParentToSDK BridgeDirection = "parent_to_sdk"
	SDKToParent BridgeDirection = "sdk_to_parent"
)

// BridgeDirectionAllowed enforces D015's closed type/direction registry. It
// does not verify origin, source window, nonce, sequence, replay or session state.
func BridgeDirectionAllowed(messageType string, direction BridgeDirection) bool {
	if direction != ParentToSDK && direction != SDKToParent {
		return false
	}
	switch messageType {
	case "ERROR":
		return true
	case "CHALLENGE", "BOUND", "PICK_AT", "LIST_INSTANCES", "HIGHLIGHT", "CLEAR_HIGHLIGHT", "REQUEST_SCROLL", "SET_MODE", "DISPOSE", "PING":
		return direction == ParentToSDK
	case "READY", "BOUND_ACK", "PICK_RESULT", "INSTANCES", "HIGHLIGHT_RESULT", "SCROLL_RESULT", "MODE_ACK", "BUILD_CHANGED", "INSTANCE_INVALIDATED", "CAPABILITY_STATUS", "DISPOSED", "PONG":
		return direction == SDKToParent
	default:
		return false
	}
}
func DecodeBridgeMessageForDirectionJSON(data []byte, maxBytes int, direction BridgeDirection) (BridgeMessage, error) {
	msg, err := DecodeBridgeMessageJSON(data, maxBytes)
	if err != nil {
		return BridgeMessage{}, err
	}
	if !BridgeDirectionAllowed(msg.Type, direction) {
		return BridgeMessage{}, fmt.Errorf("bridge direction not allowed")
	}
	return msg, nil
}
