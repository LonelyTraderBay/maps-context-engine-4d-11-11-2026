package inventory

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

var (
	ErrIgnoreBudget      = errors.New("Git ignore evaluation exceeded its budget")
	ErrIgnoreUnsupported = errors.New("Git ignore input cannot be evaluated safely")
)

// IgnoreSource must already be captured and authorized by its caller. Kind is
// global, info, or directory; Base is empty for global/info and the containing
// relative directory for a .gitignore. This is not discovery of ignore files.
type IgnoreSource struct {
	Kind  string
	Base  string
	Rules []byte
}

type IgnoreBudget struct {
	MaxSources     int
	MaxRules       int
	MaxRuleBytes   int
	MaxQueries     int
	MaxQueryBytes  int
	MaxOutputBytes int
}

// compileIgnoreSources places the supplied, captured rules in Git precedence
// order and anchors nested rule sources in a synthetic empty worktree. The
// caller must establish that this set includes every effective ignore source.
func compileIgnoreSources(sources []IgnoreSource, scanner *policy.Scanner, budget IgnoreBudget) ([]byte, error) {
	if scanner == nil || budget.MaxSources < 1 || budget.MaxSources > 4096 || budget.MaxRules < 1 || budget.MaxRules > 100000 || budget.MaxRuleBytes < 1 || budget.MaxRuleBytes > 4*1024*1024 || len(sources) > budget.MaxSources {
		return nil, ErrIgnoreBudget
	}
	ordered := make([]IgnoreSource, len(sources))
	seen := make(map[string]bool, len(sources))
	inputBytes := 0
	for i, source := range sources {
		if source.Kind != "global" && source.Kind != "info" && source.Kind != "directory" || source.Kind != "directory" && source.Base != "" {
			return nil, ErrIgnoreUnsupported
		}
		if source.Base != "" && model.ValidateRelativePath(source.Base, false) != nil {
			return nil, ErrIgnoreUnsupported
		}
		key := source.Kind + "\x00" + source.Base
		if seen[key] {
			return nil, ErrIgnoreUnsupported
		}
		seen[key] = true
		if len(source.Rules) > budget.MaxRuleBytes-inputBytes {
			return nil, ErrIgnoreBudget
		}
		inputBytes += len(source.Rules)
		if err := scanner.CheckContent(source.Rules); err != nil {
			return nil, err
		}
		ordered[i] = IgnoreSource{Kind: source.Kind, Base: source.Base, Rules: bytes.Clone(source.Rules)}
	}
	priority := func(kind string) int {
		if kind == "global" {
			return 0
		}
		if kind == "info" {
			return 1
		}
		return 2
	}
	depth := func(base string) int {
		if base == "" {
			return 0
		}
		return strings.Count(base, "/") + 1
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if priority(a.Kind) != priority(b.Kind) {
			return priority(a.Kind) < priority(b.Kind)
		}
		if depth(a.Base) != depth(b.Base) {
			return depth(a.Base) < depth(b.Base)
		}
		return a.Base < b.Base
	})
	var output bytes.Buffer
	count := 0
	for _, source := range ordered {
		if bytes.HasPrefix(source.Rules, []byte{0xef, 0xbb, 0xbf}) {
			return nil, ErrIgnoreUnsupported
		}
		lines := bytes.Split(source.Rules, []byte{'\n'})
		for _, raw := range lines {
			count++
			if count > budget.MaxRules {
				return nil, ErrIgnoreBudget
			}
			line := strings.TrimSuffix(string(raw), "\r")
			if !utf8.ValidString(line) {
				return nil, ErrIgnoreUnsupported
			}
			transformed, ok, err := anchorIgnoreRule(source.Base, line)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if len(transformed)+1 > budget.MaxRuleBytes-output.Len() {
				return nil, ErrIgnoreBudget
			}
			output.WriteString(transformed)
			output.WriteByte('\n')
		}
	}
	return output.Bytes(), nil
}

func anchorIgnoreRule(base, line string) (string, bool, error) {
	if line == "" || strings.HasPrefix(line, "#") {
		return "", false, nil
	}
	if base == "" {
		return line, true, nil
	}
	negative := strings.HasPrefix(line, "!")
	body := line
	if negative {
		body = body[1:]
	}
	body = trimGitTrailingSpaces(body)
	if body == "" || body == "/" {
		return "", false, nil
	}
	anchored := strings.HasPrefix(body, "/") || strings.Contains(strings.TrimSuffix(body, "/"), "/")
	if strings.HasPrefix(body, "/") {
		body = body[1:]
	}
	if strings.HasPrefix(body, "/") {
		return "", false, ErrIgnoreUnsupported
	}
	var prefix strings.Builder
	for _, r := range base {
		if strings.ContainsRune(`*?[]\`, r) {
			prefix.WriteByte('\\')
		}
		prefix.WriteRune(r)
	}
	if anchored {
		prefix.WriteByte('/')
	} else {
		prefix.WriteString("/**/")
	}
	if negative {
		return "!" + prefix.String() + body, true, nil
	}
	return prefix.String() + body, true, nil
}

func trimGitTrailingSpaces(value string) string {
	for len(value) > 0 && value[len(value)-1] == ' ' {
		backslashes := 0
		for i := len(value) - 2; i >= 0 && value[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 != 0 {
			break
		}
		value = value[:len(value)-1]
	}
	return value
}
