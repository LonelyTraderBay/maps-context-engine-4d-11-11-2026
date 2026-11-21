package inventory

import (
	"bytes"
	"context"
	"regexp"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

type windowsIgnorePattern struct {
	glob                       *regexp.Regexp
	negative, basename, folder bool
}

// evaluateWindowsIgnores uses the captured, precedence-ordered Git inputs.
// It checks each directory prefix before testing a candidate. A negation
// within an ignored directory cannot re-include a child on its own.
func evaluateWindowsIgnores(ctx context.Context, sources []IgnoreSource, walk CandidateWalk, scanner *policy.Scanner, ignoreCase bool) ([]bool, error) {
	if ignoreCase {
		for _, item := range sources {
			// Rule bases come from NTFS path spelling, which can differ from
			// Git's byte-wise case folding on non-ASCII names.
			if !asciiWindowsName(item.Base) {
				return nil, ErrIgnoreUnsupported
			}
		}
	}
	compiled, err := compileIgnoreSources(sources, scanner, IgnoreBudget{MaxSources: 1024, MaxRules: 4096, MaxRuleBytes: 1 << 20})
	if err != nil {
		return nil, err
	}
	patterns := []windowsIgnorePattern{}
	for _, line := range bytes.Split(compiled, []byte{'\n'}) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rule := trimGitTrailingSpaces(string(line))
		if rule == "" || rule == "/" || rule == "!" {
			continue
		}
		p, err := compileWindowsIgnorePattern(rule, ignoreCase)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	result := make([]bool, len(walk.Candidates))
	if len(patterns) == 0 {
		return result, ctx.Err()
	}
	// Reuse decisions for directory prefixes across candidates. Bound the
	// remaining work as well as the captured input and pattern sizes.
	parents := map[string]bool{}
	var evaluations int
	match := func(name string, isDirectory bool) (bool, error) {
		evaluations += len(patterns)
		if evaluations > 2_000_000 {
			return false, ErrIgnoreBudget
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		full := windowsGitBytes(name, ignoreCase)
		base := full[strings.LastIndexByte(full, '/')+1:]
		excluded := false
		for _, rule := range patterns {
			if rule.folder && !isDirectory {
				continue
			}
			value := full
			if rule.basename {
				value = base
			}
			if rule.glob.MatchString(value) {
				excluded = !rule.negative
			}
		}
		return excluded, nil
	}
	for i, candidate := range walk.Candidates {
		path := candidate.Path
		for end := 0; end < len(path); end++ {
			if path[end] != '/' {
				continue
			}
			name := path[:end]
			excluded, found := parents[name]
			if !found {
				excluded, err = match(name, true)
				if err != nil {
					return nil, err
				}
				parents[name] = excluded
			}
			if excluded {
				result[i] = true
				break
			}
		}
		if !result[i] {
			result[i], err = match(path, candidate.Directory)
			if err != nil {
				return nil, err
			}
		}
	}
	return result, ctx.Err()
}

func compileWindowsIgnorePattern(rule string, ignoreCase bool) (windowsIgnorePattern, error) {
	var p windowsIgnorePattern
	body, negative := strings.CutPrefix(rule, "!")
	p.negative = negative
	body, p.folder = strings.CutSuffix(body, "/")
	anchored := strings.HasPrefix(body, "/")
	body = strings.TrimPrefix(body, "/")
	if len(body) == 0 || len(body) > 1024 || strings.ContainsAny(body, "\x00\r") || strings.HasPrefix(body, "/") {
		return p, ErrIgnoreUnsupported
	}
	p.basename = !anchored && !strings.Contains(body, "/")
	var pattern strings.Builder
	pattern.WriteString("^")
	for i := 0; i < len(body); {
		switch body[i] {
		case '\\':
			i++
			if i >= len(body) || body[i] == '/' {
				return p, ErrIgnoreUnsupported
			}
			pattern.WriteString(regexp.QuoteMeta(windowsGitBytes(body[i:i+1], ignoreCase)))
			i++
		case '*':
			end := i + 1
			for end < len(body) && body[end] == '*' {
				end++
			}
			componentStart := i == 0 || body[i-1] == '/'
			switch {
			case end-i >= 2 && componentStart && end < len(body) && body[end] == '/':
				pattern.WriteString("(?:[^/]+/)*")
				end++
			case end-i >= 2 && i > 0 && componentStart && end == len(body):
				pattern.WriteString(".*")
			default:
				pattern.WriteString("[^/]*")
			}
			i = end
		case '?':
			pattern.WriteString("[^/]")
			i++
		case '[':
			class, end, err := windowsIgnoreClass(body, i, ignoreCase)
			if err != nil {
				return p, err
			}
			pattern.WriteString(class)
			i = end
		default:
			pattern.WriteString(regexp.QuoteMeta(windowsGitBytes(body[i:i+1], ignoreCase)))
			i++
		}
	}
	pattern.WriteString("$")
	var err error
	p.glob, err = regexp.Compile(pattern.String())
	if err != nil {
		return p, ErrIgnoreUnsupported
	}
	return p, nil
}

func windowsIgnoreClass(body string, start int, ignoreCase bool) (string, int, error) {
	i := start + 1
	if i >= len(body) {
		return "", 0, ErrIgnoreUnsupported
	}
	negative := body[i] == '!' || body[i] == '^'
	if negative {
		i++
	}
	begin := i
	// Use explicit ranges for the portable class subset. Reject ambiguous
	// escape, POSIX class and non-ASCII range syntax before capture.
	for ; i < len(body) && body[i] != ']'; i++ {
		if body[i] >= 128 || strings.ContainsRune(`[\/^`, rune(body[i])) {
			return "", 0, ErrIgnoreUnsupported
		}
	}
	if i == begin || i == len(body) {
		return "", 0, ErrIgnoreUnsupported
	}
	class := body[begin:i]
	for j := 1; j+1 < len(class); j++ {
		if class[j] == '-' && class[j-1] <= '/' && class[j+1] >= '/' {
			return "", 0, ErrIgnoreUnsupported
		}
	}
	if ignoreCase && class != strings.ToLower(class) && strings.Contains(class, "-") {
		// Folding mixed-case ranges can expand or shrink Git's byte set.
		return "", 0, ErrIgnoreUnsupported
	}
	class = windowsGitBytes(class, ignoreCase)
	if negative {
		class = "^/" + class
	}
	return "[" + class + "]", i + 1, nil
}

func asciiWindowsName(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] >= 128 {
			return false
		}
	}
	return true
}

// Map each source byte to one regexp rune to keep '?' byte-wise on UTF-8.
func windowsGitBytes(value string, ignoreCase bool) string {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if ignoreCase && b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		result.WriteRune(rune(b))
	}
	return result.String()
}
