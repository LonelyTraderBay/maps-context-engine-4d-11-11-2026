package model

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type Pagination struct {
	Truncated           bool    `json:"truncated"`
	EnumerationComplete bool    `json:"enumeration_complete"`
	ReturnedCount       int64   `json:"returned_count"`
	RequiredPending     *int64  `json:"required_pending"`
	OptionalPending     *int64  `json:"optional_pending"`
	NextCursor          *string `json:"next_cursor"`
	LimitReason         *string `json:"limit_reason"`
}

func DecodePaginationJSON(data []byte, maxBytes int) (Pagination, error) {
	var p Pagination
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return p, err
	}
	_, err := scopeObject(data, []string{"truncated", "enumeration_complete", "returned_count", "required_pending", "optional_pending", "next_cursor", "limit_reason"}, map[string]bool{"required_pending": true, "optional_pending": true, "next_cursor": true, "limit_reason": true})
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return Pagination{}, err
	}
	countOK := func(v *int64) bool { return v == nil || (*v >= 0 && *v <= maxExactInteger) }
	if p.ReturnedCount < 0 || p.ReturnedCount > maxExactInteger || !countOK(p.RequiredPending) || !countOK(p.OptionalPending) || !optionalID(p.LimitReason) {
		return Pagination{}, fmt.Errorf("invalid pagination fields")
	}
	if p.NextCursor != nil && (!utf8.ValidString(*p.NextCursor) || utf8.RuneCountInString(*p.NextCursor) < 1 || utf8.RuneCountInString(*p.NextCursor) > 32768) {
		return Pagination{}, fmt.Errorf("invalid cursor")
	}
	pending := p.RequiredPending == nil || p.OptionalPending == nil || *p.RequiredPending > 0 || *p.OptionalPending > 0 || !p.EnumerationComplete
	if p.Truncated != pending {
		return Pagination{}, fmt.Errorf("truncation disagrees with pending or enumeration")
	}
	if !p.EnumerationComplete && p.RequiredPending != nil && *p.RequiredPending == 0 && p.OptionalPending != nil && *p.OptionalPending == 0 {
		return Pagination{}, fmt.Errorf("incomplete enumeration has no pending work")
	}
	return p, nil
}
