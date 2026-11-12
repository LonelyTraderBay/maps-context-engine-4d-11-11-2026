package model

import (
	"fmt"
	"strings"
	"unicode"
)

// VerifyCanonicalPreviewPathPrefix applies D014 to canonical browser pathnames.
// The caller must obtain both paths from a trusted WHATWG-compatible URL
// parser and enforce query/fragment and authorization policy separately.
func VerifyCanonicalPreviewPathPrefix(approvedPrefix, pathname string) error {
	valid := func(s string) bool {
		if !boundedString(s, 2048) || !strings.HasPrefix(s, "/") || strings.ContainsAny(s, "?#\\") {
			return false
		}
		for _, r := range s {
			if unicode.IsControl(r) {
				return false
			}
		}
		return true
	}
	if !valid(approvedPrefix) || !valid(pathname) {
		return fmt.Errorf("invalid canonical preview path")
	}
	if approvedPrefix == "/" || pathname == approvedPrefix || strings.HasSuffix(approvedPrefix, "/") && strings.HasPrefix(pathname, approvedPrefix) || !strings.HasSuffix(approvedPrefix, "/") && strings.HasPrefix(pathname, approvedPrefix+"/") {
		return nil
	}
	return fmt.Errorf("preview path outside approved prefix")
}
