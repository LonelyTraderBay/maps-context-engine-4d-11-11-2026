package sql

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind uint8

const (
	bare tokenKind = iota
	identifier
	literal
	punctuation
)

type token struct {
	kind       tokenKind
	text       string
	start, end int
}

func (t token) keyword(word string) bool {
	return t.kind == bare && strings.EqualFold(t.text, word)
}

func (t token) symbol(value string) bool {
	return t.kind == punctuation && t.text == value
}

type statement struct {
	tokens     []token
	start, end int
	gap        string
}

const (
	maxTokens     = 65536
	maxStatements = 2048
)

func splitStatements(ctx context.Context, text string, mode string) ([]statement, error) {
	result := make([]statement, 0)
	current := statement{start: -1}
	delimiter := ";"
	lineStart := true
	used := 0
	finish := func(end int) error {
		if current.start >= 0 {
			if len(result) == maxStatements {
				return ErrLimit
			}
			current.end = end
			result = append(result, current)
		}
		current = statement{start: -1}
		return nil
	}
	for i := 0; i < len(text); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c := text[i]
		if isSpace(c) {
			if c == '\n' {
				lineStart = true
			}
			i++
			continue
		}
		if lineStart && current.start < 0 && delimiterDirective(text[i:]) {
			end := lineEnd(text, i)
			next := strings.TrimSpace(text[i+len("delimiter") : end])
			if !validDelimiter(next) {
				current.start, current.gap = i, "SQL_UNSUPPORTED_DELIMITER"
				if err := finish(len(text)); err != nil {
					return nil, err
				}
				break
			}
			delimiter, i = next, end
			continue
		}
		lineStart = false
		if strings.HasPrefix(text[i:], delimiter) {
			if err := finish(i); err != nil {
				return nil, err
			}
			i += len(delimiter)
			continue
		}
		if c == '#' || strings.HasPrefix(text[i:], "--") && i+2 < len(text) && isSpace(text[i+2]) {
			i = lineEnd(text, i)
			continue
		}
		if strings.HasPrefix(text[i:], "/*") {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				if current.start < 0 {
					current.start = i
				}
				current.gap = "SQL_UNTERMINATED_COMMENT"
				if err := finish(len(text)); err != nil {
					return nil, err
				}
				break
			}
			if strings.HasPrefix(text[i:], "/*!") {
				if current.start < 0 {
					current.start = i
				}
				current.gap = "SQL_EXECUTABLE_COMMENT"
			}
			i += 2 + end + 2
			continue
		}
		if current.start < 0 {
			current.start = i
		}
		start := i
		var value token
		switch {
		case c == '\'' || c == '"' || c == '`':
			isIdentifier := c == '`' || c == '"' && mode == "ansi_quotes"
			end := quotedEnd(text, i, c, !isIdentifier && mode != "no_backslash_escapes")
			if end < 0 {
				current.gap = "SQL_UNTERMINATED_QUOTE"
				if err := finish(len(text)); err != nil {
					return nil, err
				}
				return result, nil
			}
			value.kind, i = literal, end
			if isIdentifier {
				value.kind = identifier
				quote := string(c)
				value.text = strings.ReplaceAll(text[start+1:i-1], quote+quote, quote)
			}
		case wordRune(text[i:]):
			for i < len(text) && wordRune(text[i:]) && !strings.HasPrefix(text[i:], delimiter) {
				_, width := utf8.DecodeRuneInString(text[i:])
				i += width
			}
			value.kind, value.text = bare, text[start:i]
		default:
			_, width := utf8.DecodeRuneInString(text[i:])
			i += width
			value.kind, value.text = punctuation, text[start:i]
		}
		value.start, value.end = start, i
		used++
		if used > maxTokens {
			return nil, ErrLimit
		}
		current.tokens = append(current.tokens, value)
	}
	if err := finish(len(text)); err != nil {
		return nil, err
	}
	return result, nil
}

func delimiterDirective(text string) bool {
	const keyword = "delimiter"
	return len(text) > len(keyword) && strings.EqualFold(text[:len(keyword)], keyword) && isSpace(text[len(keyword)])
}

func validDelimiter(value string) bool {
	if len(value) == 0 || len(value) > 8 {
		return false
	}
	for _, c := range value {
		if !strings.ContainsRune("$/;|~^", c) {
			return false
		}
	}
	return true
}

func lineEnd(text string, start int) int {
	n := strings.IndexByte(text[start:], '\n')
	if n < 0 {
		return len(text)
	}
	return start + n
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\n' || c == '\r'
}

func quotedEnd(text string, start int, quote byte, escapes bool) int {
	for i := start + 1; i < len(text); i++ {
		if text[i] == '\\' && escapes {
			i++
			continue
		}
		if text[i] != quote {
			continue
		}
		if i+1 < len(text) && text[i+1] == quote {
			i++
			continue
		}
		return i + 1
	}
	return -1
}

func wordRune(text string) bool {
	r, _ := utf8.DecodeRuneInString(text)
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
