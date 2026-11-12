package model

import "fmt"

// ValidateMappingCompleteness checks the global count before a unique exact
// label. The caller must supply a trusted count from complete enumeration.
func ValidateMappingCompleteness(status string, enumerationComplete bool, knownTotal *int64) error {
	if knownTotal != nil && (*knownTotal < 0 || *knownTotal > maxExactInteger) {
		return fmt.Errorf("invalid mapping count")
	}
	switch status {
	case "exact_in_supported_scope":
		if !enumerationComplete || knownTotal == nil || *knownTotal != 1 {
			return fmt.Errorf("exact mapping is not uniquely enumerated")
		}
	case "candidate", "ambiguous", "ancestor_fallback", "unmapped", "stale", "unsupported":
	default:
		return fmt.Errorf("invalid mapping status")
	}
	return nil
}
