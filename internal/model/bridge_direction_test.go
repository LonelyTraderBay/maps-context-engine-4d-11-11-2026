package model

import "testing"

func TestBridgeClosedDirections(t *testing.T) {
	parent := []string{"CHALLENGE", "BOUND", "PICK_AT", "LIST_INSTANCES", "HIGHLIGHT", "CLEAR_HIGHLIGHT", "REQUEST_SCROLL", "SET_MODE", "DISPOSE", "PING"}
	sdk := []string{"READY", "BOUND_ACK", "PICK_RESULT", "INSTANCES", "HIGHLIGHT_RESULT", "SCROLL_RESULT", "MODE_ACK", "BUILD_CHANGED", "INSTANCE_INVALIDATED", "CAPABILITY_STATUS", "DISPOSED", "PONG"}
	for _, typ := range parent {
		if !BridgeDirectionAllowed(typ, ParentToSDK) || BridgeDirectionAllowed(typ, SDKToParent) {
			t.Fatalf("bad direction %s", typ)
		}
	}
	for _, typ := range sdk {
		if !BridgeDirectionAllowed(typ, SDKToParent) || BridgeDirectionAllowed(typ, ParentToSDK) {
			t.Fatalf("bad direction %s", typ)
		}
	}
	if !BridgeDirectionAllowed("ERROR", ParentToSDK) || !BridgeDirectionAllowed("ERROR", SDKToParent) || BridgeDirectionAllowed("UNKNOWN", ParentToSDK) || BridgeDirectionAllowed("ERROR", BridgeDirection("unknown")) {
		t.Fatal("invalid closed direction behavior")
	}
}
