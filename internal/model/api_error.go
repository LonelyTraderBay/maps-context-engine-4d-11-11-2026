package model

import (
	"encoding/json"
	"fmt"
)

type ErrorInfo struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	Retryable       bool   `json:"retryable"`
	SuggestedAction string `json:"suggested_action"`
	Details         []Gap  `json:"details"`
}

var errorCodes = map[string]bool{"INVALID_ARGUMENT": true, "UNSUPPORTED_SCHEMA": true, "UNKNOWN_WORKTREE": true, "ROOT_IDENTITY_CHANGED": true, "ACCESS_DENIED": true, "SECRET_BLOCKED": true, "PATH_ESCAPE": true, "INDEX_MISSING": true, "INDEX_STALE": true, "FRESHNESS_UNKNOWN": true, "WORKTREE_CHANGED_DURING_INDEX": true, "AMBIGUOUS_SYMBOL": true, "UNSUPPORTED_CAPABILITY": true, "DEPENDENCY_UNAVAILABLE": true, "WORKTREE_BUSY": true, "RESOURCE_LIMIT": true, "TIMEOUT": true, "CANCELLED": true, "BUDGET_TOO_SMALL": true, "CONTEXT_INCOMPLETE": true, "CURSOR_INVALID": true, "CURSOR_EXPIRED": true, "SNAPSHOT_EXPIRED": true, "POLICY_CHANGED": true, "PROFILE_MISMATCH": true, "SOURCE_PRECONDITION_FAILED": true, "SCHEMA_TOO_NEW": true, "INDEX_CORRUPT": true, "DISK_FULL": true, "INTERNAL_ERROR": true, "PREVIEW_NOT_APPROVED": true, "PREVIEW_UNAVAILABLE": true, "PREVIEW_EMBED_BLOCKED": true, "PREVIEW_ORIGIN_MISMATCH": true, "PREVIEW_BUILD_MISMATCH": true, "BRIDGE_NOT_READY": true, "BRIDGE_PROTOCOL_MISMATCH": true, "STALE_INSTANCE": true, "AMBIGUOUS_UI_MAPPING": true, "UI_TARGET_NOT_RENDERED": true, "BUILD_MAP_MISSING": true, "BUILD_MAP_EXPIRED": true, "UNSUPPORTED_FRAMEWORK": true, "TRACE_UNBOUND": true, "INTERACTION_NOT_APPROVED": true, "EXPLORER_ASSETS_UNAVAILABLE": true, "RECORD_CONFLICT": true, "RECORD_CHANGED": true, "MODE_ACK_TIMEOUT": true, "AUTH_REQUIRED": true, "AUTH_EXPIRED": true, "AUTH_INVALID": true, "RATE_LIMITED": true, "CONTRACT_NOT_FROZEN": true, "UNEXPECTED_FIELD": true, "DUPLICATE_FIELD": true, "SOURCE_CAPTURE_MISMATCH": true, "RESULT_TOO_LARGE": true, "SOURCE_RANGE_INVALID": true, "RESULT_EXPIRED": true, "STORE_REPLACED": true, "STORE_MAINTENANCE": true, "CONFIG_CONFLICT": true, "PERMISSION_REVOKED": true, "PERMISSION_EXPIRED": true, "STORE_RECOVERY_REQUIRED": true}
var suggestedActions = map[string]bool{"fix_request": true, "reauthenticate": true, "register_scope": true, "explicit_index": true, "recreate_pack": true, "expand": true, "review_candidates": true, "verify_dependency": true, "retry_bounded": true, "review_policy": true, "recover_store": true, "cancel": true, "none": true}

// DecodeErrorInfoJSON validates the wire envelope, not a provider's claims.
func DecodeErrorInfoJSON(data []byte, maxBytes int) (ErrorInfo, error) {
	var e ErrorInfo
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return e, err
	}
	fields, err := scopeObject(data, []string{"code", "message", "retryable", "suggested_action", "details"}, nil)
	if err != nil {
		return e, err
	}
	var details []json.RawMessage
	if err := json.Unmarshal(fields["details"], &details); err != nil {
		return e, err
	}
	if len(details) > 16 {
		return e, fmt.Errorf("too many error details")
	}
	for _, detail := range details {
		if _, err := DecodeGapJSON(detail, maxBytes); err != nil {
			return e, err
		}
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return ErrorInfo{}, err
	}
	if !errorCodes[e.Code] || !boundedString(e.Message, 1024) || !suggestedActions[e.SuggestedAction] || e.Details == nil {
		return ErrorInfo{}, fmt.Errorf("invalid error info")
	}
	return e, nil
}
