package model

import "testing"

func TestContinuationShape(t *testing.T) {
	zero := int64(0)
	next := "next"
	p := Pagination{Truncated: true, EnumerationComplete: false, ReturnedCount: 1, OptionalPending: &zero, NextCursor: &next}
	if err := ValidateContinuationShape(p, true, true, nil, 8192); err != nil {
		t.Fatal(err)
	}
	if err := ValidateContinuationShape(p, true, true, &next, 8192); err == nil {
		t.Fatal("accepted repeated cursor")
	}
	if err := ValidateContinuationShape(p, true, false, nil, 8192); err == nil {
		t.Fatal("accepted cursor for unsupported continuation")
	}
	p.NextCursor = nil
	if err := ValidateContinuationShape(p, true, true, nil, 8192); err == nil {
		t.Fatal("accepted pending page without cursor")
	}
	if err := ValidateContinuationShape(p, false, true, nil, 8192); err != nil {
		t.Fatal(err)
	}
}
