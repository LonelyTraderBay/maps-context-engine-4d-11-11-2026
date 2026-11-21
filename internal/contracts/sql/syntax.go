package sql

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type objectName struct {
	schema string
	name   string
}

func (n objectName) key() string {
	quote := func(part string) string { return "`" + strings.ReplaceAll(part, "`", "``") + "`" }
	return quote(n.schema) + "." + quote(n.name)
}

type objectUse struct {
	name       objectName
	kind       string
	relation   string
	start, end int
}

type column struct {
	name       string
	typ        string
	nullable   *bool
	precision  *int64
	start, end int
}

type declaration struct {
	name    objectName
	kind    string
	engine  string
	columns []column
	uses    []objectUse
	gaps    []string
}

func keywordAt(tokens []token, at int, word string) bool {
	return at >= 0 && at < len(tokens) && tokens[at].keyword(word)
}

func symbolAt(tokens []token, at int, symbol string) bool {
	return at >= 0 && at < len(tokens) && tokens[at].symbol(symbol)
}

func identifierAt(tokens []token, at int) (string, bool) {
	if at < 0 || at >= len(tokens) {
		return "", false
	}
	value := tokens[at]
	if value.kind == identifier {
		return value.text, validIdentifier(value.text)
	}
	if value.kind != bare || !validIdentifier(value.text) {
		return "", false
	}
	first, _ := utf8.DecodeRuneInString(value.text)
	return value.text, unicode.IsLetter(first) || first == '_' || first == '$'
}

func readName(tokens []token, at int, schema string) (objectName, int, bool) {
	name, ok := identifierAt(tokens, at)
	if !ok {
		return objectName{}, at, false
	}
	next := at + 1
	if symbolAt(tokens, next, ".") {
		object, ok := identifierAt(tokens, next+1)
		if !ok || symbolAt(tokens, next+2, ".") {
			return objectName{}, at, false
		}
		return objectName{schema: name, name: object}, next + 2, true
	}
	if schema == "" {
		return objectName{}, at, false
	}
	return objectName{schema: schema, name: name}, next, true
}

func closeParen(tokens []token, at int) int {
	if !symbolAt(tokens, at, "(") {
		return -1
	}
	depth := 0
	for i := at; i < len(tokens); i++ {
		switch {
		case tokens[i].symbol("("):
			depth++
			if depth > 64 {
				return -1
			}
		case tokens[i].symbol(")"):
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func balancedParentheses(tokens []token) bool {
	depth := 0
	for _, item := range tokens {
		if item.symbol("(") {
			depth++
		} else if item.symbol(")") {
			depth--
		}
		if depth < 0 || depth > 64 {
			return false
		}
	}
	return depth == 0
}

func clause(tokens []token, from int, words ...string) int {
	for i := from; i < len(tokens); i++ {
		if tokens[i].symbol("(") {
			end := closeParen(tokens, i)
			if end < 0 {
				return -1
			}
			i = end
			continue
		}
		for _, word := range words {
			if tokens[i].keyword(word) {
				return i
			}
		}
	}
	return -1
}

func makeUse(tokens []token, at int, schema, kind, relation string) (objectUse, int, bool) {
	name, next, ok := readName(tokens, at, schema)
	if !ok {
		return objectUse{}, at, false
	}
	return objectUse{name: name, kind: kind, relation: relation, start: tokens[at].start, end: tokens[next-1].end}, next, true
}
