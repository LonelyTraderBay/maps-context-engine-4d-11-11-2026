package sql

import "strings"

func queryUses(tokens []token, schema string) (uses []objectUse, gap string, recognized bool) {
	if len(tokens) == 0 {
		return nil, "", false
	}
	switch {
	case tokens[0].keyword("SELECT"):
		uses, gap := selectUses(tokens, schema)
		return uses, gap, true
	case tokens[0].keyword("PREPARE"), tokens[0].keyword("EXECUTE"), tokens[0].keyword("DEALLOCATE"):
		return nil, "SQL_DYNAMIC_STATEMENT", true
	case tokens[0].keyword("CALL"):
		use, next, ok := makeUse(tokens, 1, schema, "procedure", "calls")
		if !ok || next != len(tokens) && (closeParen(tokens, next) != len(tokens)-1) {
			return nil, "SQL_UNRESOLVED_ROUTINE", true
		}
		return []objectUse{use}, "", true
	case tokens[0].keyword("INSERT"), tokens[0].keyword("UPDATE"), tokens[0].keyword("DELETE"):
		return writeUses(tokens, schema)
	default:
		return nil, "", false
	}
}

func selectUses(tokens []token, schema string) ([]objectUse, string) {
	for _, t := range tokens[1:] {
		if t.keyword("SELECT") || t.keyword("UNION") || t.keyword("INTO") {
			return nil, "SQL_COMPLEX_QUERY"
		}
	}
	from := clause(tokens, 1, "FROM")
	if from == 1 {
		return nil, "SQL_SELECT_PROJECTION_UNSUPPORTED"
	}
	if from < 0 {
		return nil, ""
	}
	uses := make([]objectUse, 0, 1)
	for at := from; at < len(tokens); {
		use, next, ok := makeUse(tokens, at+1, schema, "table_or_view", "reads")
		if !ok || reservedTableToken(tokens[at+1]) || symbolAt(tokens, next, "(") {
			return uses, "SQL_UNRESOLVED_TABLE"
		}
		uses = append(uses, use)
		at = scanJoin(tokens, next)
		if at == -2 {
			return uses, "SQL_COMPLEX_FROM"
		}
		if at < 0 {
			return uses, ""
		}
	}
	return uses, ""
}

func reservedTableToken(t token) bool {
	if t.kind != bare {
		return false
	}
	switch strings.ToUpper(t.text) {
	case "AS", "SELECT", "FROM", "JOIN", "ON", "WHERE", "GROUP", "ORDER", "LIMIT", "SET", "VALUES", "USING", "INTO", "END":
		return true
	default:
		return false
	}
}

// scanJoin returns the next supported explicit join. Comma joins and
// malformed or unsupported join bodies leave an explicit gap.
func scanJoin(tokens []token, from int) int {
	for i := from; i < len(tokens); i++ {
		t := tokens[i]
		if t.kind == bare {
			switch strings.ToUpper(t.text) {
			case "JOIN", "STRAIGHT_JOIN":
				return i
			case "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "FOR", "LOCK", "WINDOW":
				return -1
			case "USING":
				end := closeParen(tokens, i+1)
				if end < 0 {
					return -2
				}
				i = end
				continue
			}
		}
		if t.symbol(",") {
			return -2
		}
		if t.symbol("(") {
			end := closeParen(tokens, i)
			if end < 0 {
				return -2
			}
			i = end
		}
	}
	return -1
}

func writeUses(tokens []token, schema string) ([]objectUse, string, bool) {
	first := strings.ToUpper(tokens[0].text)
	at := 1
	if first == "INSERT" && keywordAt(tokens, at, "INTO") || first == "DELETE" && keywordAt(tokens, at, "FROM") {
		at++
	} else if first != "UPDATE" {
		return nil, "SQL_UNSUPPORTED_WRITE", true
	}
	use, next, ok := makeUse(tokens, at, schema, "table_or_view", "writes")
	if !ok || reservedTableToken(tokens[at]) {
		return nil, "SQL_UNRESOLVED_TABLE", true
	}
	switch first {
	case "INSERT":
		if symbolAt(tokens, next, "(") {
			end := closeParen(tokens, next)
			if end < 0 {
				return nil, "SQL_UNSUPPORTED_WRITE", true
			}
			next = end + 1
		}
		if keywordAt(tokens, next, "SELECT") {
			inputs, gap := selectUses(tokens[next:], schema)
			return append([]objectUse{use}, inputs...), gap, true
		}
		if !keywordAt(tokens, next, "VALUES") && !keywordAt(tokens, next, "VALUE") && !keywordAt(tokens, next, "SET") {
			return nil, "SQL_UNSUPPORTED_WRITE", true
		}
	case "UPDATE":
		if !keywordAt(tokens, simpleAlias(tokens, next), "SET") {
			return nil, "SQL_UNSUPPORTED_WRITE", true
		}
	case "DELETE":
		next = simpleAlias(tokens, next)
		if next != len(tokens) && !keywordAt(tokens, next, "WHERE") && !keywordAt(tokens, next, "ORDER") && !keywordAt(tokens, next, "LIMIT") {
			return nil, "SQL_UNSUPPORTED_WRITE", true
		}
	}
	for _, t := range tokens[next:] {
		if t.keyword("SELECT") {
			return []objectUse{use}, "SQL_COMPLEX_QUERY", true
		}
	}
	return []objectUse{use}, "", true
}

func simpleAlias(tokens []token, at int) int {
	if keywordAt(tokens, at, "AS") {
		if _, ok := identifierAt(tokens, at+1); ok {
			return at + 2
		}
		return at
	}
	if _, ok := identifierAt(tokens, at); ok && !reservedTableToken(tokens[at]) && !keywordAt(tokens, at, "ORDER") {
		return at + 1
	}
	return at
}
