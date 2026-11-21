package sql

import "strings"

func parseDeclaration(tokens []token, schema string, profile Profile) declaration {
	if !balancedParentheses(tokens) {
		return unsupported("SQL_STATEMENT_UNBALANCED")
	}
	if keywordAt(tokens, 0, "CREATE") {
		return createDeclaration(tokens, schema, profile)
	}
	if keywordAt(tokens, 0, "ALTER") || keywordAt(tokens, 0, "DROP") {
		return changeDeclaration(tokens, schema, profile)
	}
	if uses, gap, recognized := queryUses(tokens, schema); recognized {
		result := declaration{kind: "query", engine: profile.Engine, uses: uses}
		// Dynamic statements supply a gap rather than a query node with
		// apparent object identity or a fabricated literal target.
		if gap == "SQL_DYNAMIC_STATEMENT" {
			return unsupported(gap)
		}
		result.addGap(gap)
		return result
	}
	return unsupported("SQL_STATEMENT_UNSUPPORTED")
}

func createDeclaration(tokens []token, schema string, profile Profile) declaration {
	at := 1
	replace := keywordAt(tokens, at, "OR") && keywordAt(tokens, at+1, "REPLACE")
	if replace {
		at += 2
	}
	if at >= len(tokens) || tokens[at].kind != bare || replace && !tokens[at].keyword("VIEW") {
		return unsupported("SQL_CREATE_UNSUPPORTED")
	}
	kind := strings.ToLower(tokens[at].text)
	switch kind {
	case "table", "view", "procedure", "function", "trigger", "event":
	default:
		return unsupported("SQL_CREATE_UNSUPPORTED")
	}
	at++
	if keywordAt(tokens, at, "IF") && keywordAt(tokens, at+1, "NOT") && keywordAt(tokens, at+2, "EXISTS") {
		if kind != "table" && kind != "event" {
			return unsupported("SQL_CREATE_OPTION_UNSUPPORTED")
		}
		at += 3
	}
	name, next, ok := readName(tokens, at, schema)
	if !ok || reservedTableToken(tokens[at]) {
		return unsupported("SQL_OBJECT_NAME_UNRESOLVED")
	}
	result := declaration{kind: kind, name: name, engine: profile.Engine}
	switch kind {
	case "table":
		result.readTable(tokens[next:], schema)
	case "view":
		if symbolAt(tokens, next, "(") {
			end := closeParen(tokens, next)
			if end < 0 {
				return unsupported("SQL_VIEW_UNSUPPORTED")
			}
			next = end + 1
		}
		if !keywordAt(tokens, next, "AS") || !keywordAt(tokens, next+1, "SELECT") {
			return unsupported("SQL_VIEW_UNSUPPORTED")
		}
		uses, gap := selectUses(tokens[next+1:], schema)
		result.uses = uses
		result.addGap(gap)
	case "procedure", "function":
		result.readRoutine(tokens, next, schema)
	case "trigger":
		result.readTrigger(tokens, next, schema)
	case "event":
		result.readEvent(tokens, next, schema)
	}
	return result
}

func changeDeclaration(tokens []token, schema string, profile Profile) declaration {
	if !keywordAt(tokens, 1, "TABLE") {
		return unsupported("SQL_CHANGE_UNSUPPORTED")
	}
	at := 2
	if keywordAt(tokens, 0, "DROP") && keywordAt(tokens, at, "IF") && keywordAt(tokens, at+1, "EXISTS") {
		at += 2
	}
	use, next, ok := makeUse(tokens, at, schema, "table", "changes")
	if !ok || reservedTableToken(tokens[at]) {
		return unsupported("SQL_CHANGE_TARGET_UNRESOLVED")
	}
	result := declaration{kind: "migration_change", name: use.name, engine: profile.Engine, uses: []objectUse{use}}
	if keywordAt(tokens, 0, "DROP") {
		if next != len(tokens) {
			result.addGap("SQL_MULTI_TABLE_CHANGE_UNSUPPORTED")
		}
		return result
	}
	if keywordAt(tokens, next, "ADD") {
		next++
		if keywordAt(tokens, next, "COLUMN") {
			next++
		}
		tail := tokens[next:]
		if len(tail) != 0 && !containsTopLevelComma(tail) {
			field, gap := parseColumn(tail)
			if field.name != "" {
				result.columns = append(result.columns, field)
				result.addGap(gap)
				return result
			}
		}
	}
	result.addGap("SQL_CHANGE_DETAIL_UNSUPPORTED")
	return result
}

func (d *declaration) readRoutine(tokens []token, begin int, schema string) {
	close := closeParen(tokens, begin)
	if close < 0 {
		d.addGap("SQL_ROUTINE_PARAMETERS_UNSUPPORTED")
		return
	}
	body := clause(tokens, close+1, "BEGIN", "SELECT", "INSERT", "UPDATE", "DELETE", "CALL", "PREPARE", "EXECUTE", "RETURN")
	if body < 0 {
		d.addGap("SQL_ROUTINE_BODY_UNSUPPORTED")
		return
	}
	d.readBody(tokens[body:], schema)
}

func (d *declaration) readTrigger(tokens []token, begin int, schema string) {
	timing := keywordAt(tokens, begin, "BEFORE") || keywordAt(tokens, begin, "AFTER")
	event := keywordAt(tokens, begin+1, "INSERT") || keywordAt(tokens, begin+1, "UPDATE") || keywordAt(tokens, begin+1, "DELETE")
	if !timing || !event || !keywordAt(tokens, begin+2, "ON") {
		d.addGap("SQL_TRIGGER_HEADER_UNSUPPORTED")
		return
	}
	use, next, ok := makeUse(tokens, begin+3, schema, "table", "triggers_on")
	if !ok || reservedTableToken(tokens[begin+3]) || !keywordAt(tokens, next, "FOR") ||
		!keywordAt(tokens, next+1, "EACH") || !keywordAt(tokens, next+2, "ROW") {
		d.addGap("SQL_TRIGGER_TARGET_UNRESOLVED")
		return
	}
	d.uses = append(d.uses, use)
	next += 3
	if keywordAt(tokens, next, "FOLLOWS") || keywordAt(tokens, next, "PRECEDES") {
		d.addGap("SQL_TRIGGER_ORDER_UNSUPPORTED")
		if _, ok := identifierAt(tokens, next+1); !ok {
			return
		}
		next += 2
	}
	d.readBody(tokens[next:], schema)
}

func (d *declaration) readEvent(tokens []token, begin int, schema string) {
	if !keywordAt(tokens, begin, "ON") || !keywordAt(tokens, begin+1, "SCHEDULE") {
		d.addGap("SQL_EVENT_HEADER_UNSUPPORTED")
		return
	}
	body := clause(tokens, begin+2, "DO")
	if body < 0 || body == begin+2 {
		d.addGap("SQL_EVENT_BODY_UNSUPPORTED")
		return
	}
	d.readBody(tokens[body+1:], schema)
}

func (d *declaration) readBody(tokens []token, schema string) {
	if len(tokens) == 0 {
		d.addGap("SQL_PROGRAM_BODY_UNSUPPORTED")
		return
	}
	if keywordAt(tokens, 0, "BEGIN") {
		if !keywordAt(tokens, len(tokens)-1, "END") {
			d.addGap("SQL_PROGRAM_BODY_UNTERMINATED")
			return
		}
		tokens = tokens[1 : len(tokens)-1]
	}
	for _, t := range tokens {
		if t.keyword("BEGIN") || t.keyword("IF") || t.keyword("LOOP") || t.keyword("CASE") ||
			t.keyword("WHILE") || t.keyword("REPEAT") {
			d.addGap("SQL_PROGRAM_CONTROL_UNSUPPORTED")
			return
		}
	}
	for start := 0; start < len(tokens); {
		end := start
		for end < len(tokens) && !tokens[end].symbol(";") {
			end++
		}
		part := tokens[start:end]
		start = end + 1
		if len(part) == 0 {
			continue
		}
		uses, gap, recognized := queryUses(part, schema)
		if !recognized {
			d.addGap("SQL_PROGRAM_STATEMENT_UNSUPPORTED")
			continue
		}
		d.uses = append(d.uses, uses...)
		d.addGap(gap)
	}
}

func (d *declaration) addGap(code string) {
	if code != "" {
		d.gaps = append(d.gaps, code)
	}
}

func unsupported(code string) declaration {
	return declaration{gaps: []string{code}}
}
