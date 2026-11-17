package policy

import (
	"path"
	"strings"
)

// CheckConfiguredPath applies additive deny globs after the built-in baseline.
// Slash-separated patterns support path.Match segments and whole-segment **.
// A denied directory also denies descendants. Slash-free patterns match any
// component. Negation, invalid syntax and oversized work fail closed.
func CheckConfiguredPath(name string, denied []string) error {
	if CheckPath(name) != nil || len(denied) > 256 {
		return ErrDeniedPath
	}
	parts := strings.Split(name, "/")
	if len(parts) > 128 {
		return ErrDeniedPath
	}
	for _, rule := range denied {
		if len(rule) == 0 || len(rule) > 512 || strings.HasPrefix(rule, "!") || strings.ContainsAny(rule, "\x00\r\n") {
			return ErrDeniedPath
		}
		anchored := strings.HasPrefix(rule, "/")
		rule = strings.TrimSuffix(strings.TrimPrefix(rule, "/"), "/")
		pattern := strings.Split(rule, "/")
		for _, segment := range pattern {
			if segment == "" || segment == "." || segment == ".." {
				return ErrDeniedPath
			}
			if _, err := path.Match(segment, ""); err != nil {
				return ErrDeniedPath
			}
		}
		if len(pattern) == 1 && !anchored {
			for _, component := range parts {
				match, _ := path.Match(pattern[0], component)
				if match || pattern[0] == "**" {
					return ErrDeniedPath
				}
			}
			continue
		}
		if (len(pattern)+1)*(len(parts)+1) > 65536 {
			return ErrDeniedPath
		}
		// dp[i][j]: the first i pattern segments match j path components.
		previous := make([]bool, len(parts)+1)
		previous[0] = true
		for _, segment := range pattern {
			next := make([]bool, len(parts)+1)
			for j := 0; j <= len(parts); j++ {
				if segment == "**" {
					next[j] = previous[j] || j > 0 && next[j-1]
				} else if j > 0 && previous[j-1] {
					next[j], _ = path.Match(segment, parts[j-1])
				}
			}
			previous = next
		}
		for prefix := 1; prefix <= len(parts); prefix++ {
			if previous[prefix] {
				return ErrDeniedPath
			}
		}
	}
	return nil
}
