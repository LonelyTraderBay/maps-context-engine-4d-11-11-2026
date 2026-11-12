package model

import (
	"encoding/json"
	"fmt"
)

// ValidateContinuationShape checks observable cursor invariants for a result
// page. Token authenticity, decoded position, scope and expiry remain caller duties.
func ValidateContinuationShape(p Pagination, hasData, supportsContinuation bool, previousCursor *string, maxBytes int) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err = DecodePaginationJSON(raw, maxBytes); err != nil {
		return err
	}
	if p.NextCursor != nil && !supportsContinuation {
		return fmt.Errorf("cursor has no continuation request")
	}
	if hasData && p.Truncated && supportsContinuation && p.NextCursor == nil {
		return fmt.Errorf("pending page has no cursor")
	}
	if previousCursor != nil && p.NextCursor != nil && *previousCursor == *p.NextCursor {
		return fmt.Errorf("cursor repeats")
	}
	return nil
}
