package model

import "strings"

// validSemanticVersion implements the SemVer 2.0.0 syntax, including the
// distinct rules for numeric prerelease identifiers and build metadata.
func validSemanticVersion(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, "+")
	if len(parts) > 2 {
		return false
	}
	if len(parts) == 2 && !validVersionIdentifiers(parts[1], false) {
		return false
	}
	coreAndPre := strings.SplitN(parts[0], "-", 2)
	if len(coreAndPre) == 2 && !validVersionIdentifiers(coreAndPre[1], true) {
		return false
	}
	core := strings.Split(coreAndPre[0], ".")
	if len(core) != 3 {
		return false
	}
	for _, number := range core {
		if !numericIdentifier(number, true) {
			return false
		}
	}
	return true
}
func numericIdentifier(s string, noLeadingZero bool) bool {
	if s == "" || noLeadingZero && len(s) > 1 && s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
func validVersionIdentifiers(s string, prerelease bool) bool {
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			c := part[i]
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-') {
				return false
			}
		}
		if prerelease && numericIdentifier(part, false) && !numericIdentifier(part, true) {
			return false
		}
	}
	return true
}
