package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestBridgeMessagesClosedAndNumeric(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	format := `{"protocol_version":1,"session_id":"%s","document_epoch":1,"hmr_epoch":0,"message_id":"%s","reply_to":null,"selection_id":null,"seq":1,"type":"%s","payload":%s}`
	challenge := fmt.Sprintf(format, id, id, "CHALLENGE", `{"nonce":"n","approved_origin":"http://localhost:3000"}`)
	pick := fmt.Sprintf(format, id, id, "PICK_AT", `{"x_css":1.5,"y_css":-2.25,"selection_kind":"element"}`)
	ping := fmt.Sprintf(format, id, id, "PING", `{"probe_id":"`+id+`"}`)
	for _, raw := range []string{challenge, pick, ping} {
		if _, err := DecodeBridgeMessageJSON([]byte(raw), 16384); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	if _, err := DecodeBridgeMessageJSON([]byte(fmt.Sprintf(format, id, id, "DISPOSE", `null`)), 16384); err == nil {
		t.Fatal("accepted null empty payload")
	}
	decoded, err := DecodeBridgeMessageJSON([]byte(pick), 16384)
	if err != nil {
		t.Fatal(err)
	}
	p := decoded.Payload.(*BridgePickAtPayload)
	if err := p.ValidateBounds(1); err == nil {
		t.Fatal("pick outside bounds accepted")
	}
	for _, bad := range []string{strings.Replace(pick, `"x_css":1.5`, `"x_css":1e309`, 1), strings.Replace(challenge, `"nonce":"n"`, `"nonce":"n","source_ref":{}`, 1), strings.Replace(ping, `"seq":1`, `"seq":0`, 1), strings.Replace(ping, `"type":"PING"`, `"type":"UNKNOWN"`, 1), strings.Replace(ping, `"reply_to":null,`, "", 1)} {
		if _, err := DecodeBridgeMessageJSON([]byte(bad), 16384); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestBridgePickResultBindsInstanceEpochs(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	instance := fmt.Sprintf(`{"session_id":"%s","document_epoch":1,"hmr_epoch":0,"instance_id":"i"}`, id)
	anchor := fmt.Sprintf(`{"build_map_id":"%s","anchor_id":"%s"}`, h, h)
	payload := fmt.Sprintf(`{"targets":[{"instance_ref":%s,"anchor_ref":%s,"rects":[{"x":1.5,"y":0,"width":1,"height":2}],"parent_instance_ref":null}],"status":"exact_in_supported_scope"}`, instance, anchor)
	msg := fmt.Sprintf(`{"protocol_version":1,"session_id":"%s","document_epoch":1,"hmr_epoch":0,"message_id":"%s","reply_to":null,"selection_id":null,"seq":1,"type":"PICK_RESULT","payload":%s}`, id, id, payload)
	if _, err := DecodeBridgeMessageJSON([]byte(msg), 16384); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(msg, `"document_epoch":1,"hmr_epoch":0,"instance_id"`, `"document_epoch":2,"hmr_epoch":0,"instance_id"`, 1)
	if _, err := DecodeBridgeMessageJSON([]byte(bad), 16384); err == nil {
		t.Fatal("accepted stale picked instance")
	}
}

func TestBridgeInstancesNestedClosedShape(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	instance := fmt.Sprintf(`{"session_id":"%s","document_epoch":1,"hmr_epoch":0,"instance_id":"i"}`, id)
	anchor := fmt.Sprintf(`{"build_map_id":"%s","anchor_id":"%s"}`, h, h)
	payload := fmt.Sprintf(`{"items":[{"instance_ref":%s,"anchor_ref":%s,"visibility":"visible","rects":[{"x":1,"y":2,"width":3,"height":4}]}],"next_cursor":null,"pending":0}`, instance, anchor)
	msg := fmt.Sprintf(`{"protocol_version":1,"session_id":"%s","document_epoch":1,"hmr_epoch":0,"message_id":"%s","reply_to":null,"selection_id":null,"seq":1,"type":"INSTANCES","payload":%s}`, id, id, payload)
	if _, err := DecodeBridgeMessageJSON([]byte(msg), 16384); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(msg, `"visibility":"visible"`, `"visibility":"visible","dom_text":"secret"`, 1)
	if _, err := DecodeBridgeMessageJSON([]byte(bad), 16384); err == nil {
		t.Fatal("accepted DOM text")
	}
}
