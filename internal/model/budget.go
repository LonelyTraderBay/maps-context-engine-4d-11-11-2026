package model

import (
	"encoding/json"
	"fmt"
)

type Budget struct {
	MaxOutputBytes  int64   `json:"max_output_bytes"`
	SerializedBytes int64   `json:"serialized_bytes"`
	TokenCount      *int64  `json:"token_count"`
	TokenEstimate   *int64  `json:"token_estimate"`
	TokenizerID     *string `json:"tokenizer_id"`
	EstimateOnly    bool    `json:"estimate_only"`
	DeliveryMode    string  `json:"delivery_mode"`
}

// DecodeBudgetJSON validates accounting labels, not the actual serialized byte
// measurement or evidence that an MCP host supports structured output.
func DecodeBudgetJSON(data []byte, maxBytes int) (Budget, error) {
	var b Budget
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return b, err
	}
	_, err := scopeObject(data, []string{"max_output_bytes", "serialized_bytes", "token_count", "token_estimate", "tokenizer_id", "estimate_only", "delivery_mode"}, map[string]bool{"token_count": true, "token_estimate": true, "tokenizer_id": true})
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return Budget{}, err
	}
	validCount := func(v *int64) bool { return v == nil || (*v >= 0 && *v <= maxExactInteger) }
	if b.MaxOutputBytes < 4096 || b.MaxOutputBytes > 1048576 || b.SerializedBytes < 0 || b.SerializedBytes > b.MaxOutputBytes || !validCount(b.TokenCount) || !validCount(b.TokenEstimate) || !optionalID(b.TokenizerID) || ((b.TokenCount == nil) != (b.TokenizerID == nil)) || (b.TokenEstimate != nil && !b.EstimateOnly) {
		return Budget{}, fmt.Errorf("invalid budget accounting")
	}
	switch b.DeliveryMode {
	case "http", "cli", "mcp_text", "mcp_dual", "mcp_structured_verified":
	default:
		return Budget{}, fmt.Errorf("invalid delivery mode")
	}
	return b, nil
}
