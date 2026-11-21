package sql

import (
	"strconv"
	"strings"
)

func (d *declaration) readTable(tokens []token, schema string) {
	if keywordAt(tokens, 0, "LIKE") {
		use, next, ok := makeUse(tokens, 1, schema, "table", "derives_from")
		if !ok || next != len(tokens) {
			*d = unsupported("SQL_TABLE_SHAPE_UNSUPPORTED")
			return
		}
		d.uses = append(d.uses, use)
		d.addGap("SQL_TABLE_SHAPE_NOT_EXPANDED")
		return
	}
	end := closeParen(tokens, 0)
	if end < 0 {
		*d = unsupported("SQL_TABLE_SHAPE_UNSUPPORTED")
		return
	}
	parts := splitColumns(tokens[1:end])
	if parts == nil {
		*d = unsupported("SQL_TABLE_COLUMNS_UNSUPPORTED")
		return
	}
	names := make(map[string]bool, len(parts))
	for _, part := range parts {
		if constraintStart(part[0]) {
			d.readConstraint(part, schema)
			continue
		}
		field, gap := parseColumn(part)
		if field.name == "" {
			d.addGap("SQL_COLUMN_UNSUPPORTED")
			continue
		}
		// MySQL column identifiers are case-insensitive. Keep the original
		// spelling in the declaration and reject an ambiguous duplicate.
		key := strings.ToLower(field.name)
		if names[key] {
			d.addGap("SQL_COLUMN_DUPLICATE")
			continue
		}
		names[key] = true
		d.columns = append(d.columns, field)
		d.addGap(gap)
	}
	options := tokens[end+1:]
	at := clause(options, 0, "ENGINE")
	if at >= 0 {
		next := at + 1
		if symbolAt(options, next, "=") {
			next++
		}
		engine, ok := identifierAt(options, next)
		chosen := engineName(engine)
		if !ok || chosen == "" || chosen == "unknown" {
			d.engine = "unknown"
			d.addGap("SQL_TABLE_ENGINE_UNSUPPORTED")
		} else {
			d.engine = chosen
		}
		if at != 0 || next+1 != len(options) {
			d.addGap("SQL_TABLE_OPTIONS_UNMODELED")
		}
	} else if len(options) > 0 {
		d.addGap("SQL_TABLE_OPTIONS_UNMODELED")
	}
}

func splitColumns(tokens []token) [][]token {
	if len(tokens) == 0 {
		return nil
	}
	parts := make([][]token, 0, 4)
	start := 0
	for i := 0; i < len(tokens); i++ {
		if tokens[i].symbol("(") {
			end := closeParen(tokens, i)
			if end < 0 {
				return nil
			}
			i = end
			continue
		}
		if tokens[i].symbol(",") {
			if start == i {
				return nil
			}
			parts = append(parts, tokens[start:i])
			start = i + 1
		}
	}
	if start == len(tokens) {
		return nil
	}
	return append(parts, tokens[start:])
}

func containsTopLevelComma(tokens []token) bool {
	return len(splitColumns(tokens)) != 1
}

func constraintStart(t token) bool {
	if t.kind != bare {
		return false
	}
	switch strings.ToUpper(t.text) {
	case "PRIMARY", "UNIQUE", "KEY", "INDEX", "FOREIGN", "CONSTRAINT", "CHECK", "FULLTEXT", "SPATIAL":
		return true
	default:
		return false
	}
}

func (d *declaration) readConstraint(tokens []token, schema string) {
	foreign := clause(tokens, 0, "FOREIGN")
	if foreign < 0 || !keywordAt(tokens, foreign+1, "KEY") {
		d.addGap("SQL_TABLE_CONSTRAINT_UNMODELED")
		return
	}
	local := foreign + 2
	if _, ok := identifierAt(tokens, local); ok {
		local++
	}
	lastColumn := closeParen(tokens, local)
	ref := lastColumn + 1
	if lastColumn < 0 || !keywordAt(tokens, ref, "REFERENCES") {
		d.addGap("SQL_FOREIGN_KEY_UNRESOLVED")
		return
	}
	use, next, ok := makeUse(tokens, ref+1, schema, "table", "references")
	if !ok || closeParen(tokens, next) < 0 {
		d.addGap("SQL_FOREIGN_KEY_UNRESOLVED")
		return
	}
	d.uses = append(d.uses, use)
}

func parseColumn(tokens []token) (column, string) {
	name, ok := identifierAt(tokens, 0)
	if !ok || constraintStart(tokens[0]) || len(tokens) < 2 || tokens[1].kind != bare {
		return column{}, "SQL_COLUMN_UNSUPPORTED"
	}
	base := strings.ToUpper(tokens[1].text)
	if !columnType(base) {
		return column{}, "SQL_COLUMN_TYPE_UNSUPPORTED"
	}
	result := column{name: name, typ: base, start: tokens[0].start, end: tokens[len(tokens)-1].end}
	next := 2
	gap := ""
	if symbolAt(tokens, next, "(") {
		close := closeParen(tokens, next)
		if close < 0 {
			return column{}, "SQL_COLUMN_TYPE_UNSUPPORTED"
		}
		args := tokens[next+1 : close]
		numbers, valid := numericTypeArgs(args)
		if valid && (base == "DECIMAL" || base == "NUMERIC") {
			precision, _ := strconv.ParseInt(numbers[0], 10, 64)
			valid = precision >= 1 && precision <= 65
			if len(numbers) == 2 {
				scale, _ := strconv.ParseInt(numbers[1], 10, 64)
				valid = valid && scale <= 30 && scale <= precision
			}
			if valid {
				result.precision = &precision
			}
		}
		if valid {
			result.typ += "(" + strings.Join(numbers, ",") + ")"
		} else {
			gap = "SQL_COLUMN_TYPE_DETAIL_UNSUPPORTED"
		}
		next = close + 1
	}
	if keywordAt(tokens, next, "UNSIGNED") {
		result.typ += " UNSIGNED"
		next++
	}
	for i := next; i < len(tokens); i++ {
		if tokens[i].symbol("(") {
			end := closeParen(tokens, i)
			if end < 0 {
				return column{}, "SQL_COLUMN_DECLARATION_UNSUPPORTED"
			}
			i = end
			continue
		}
		if keywordAt(tokens, i, "NULL") {
			if keywordAt(tokens, i-1, "DEFAULT") {
				continue
			}
			nullable := !keywordAt(tokens, i-1, "NOT")
			result.nullable = &nullable
		}
	}
	return result, gap
}

func numericTypeArgs(tokens []token) ([]string, bool) {
	if len(tokens) != 1 && (len(tokens) != 3 || !tokens[1].symbol(",")) {
		return nil, false
	}
	parts := make([]string, 0, 2)
	for i := 0; i < len(tokens); i += 2 {
		if tokens[i].kind != bare {
			return nil, false
		}
		value, err := strconv.ParseUint(tokens[i].text, 10, 32)
		if err != nil {
			return nil, false
		}
		parts = append(parts, strconv.FormatUint(value, 10))
	}
	return parts, true
}

func columnType(value string) bool {
	switch value {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "DECIMAL", "NUMERIC",
		"FLOAT", "DOUBLE", "BIT", "BOOL", "BOOLEAN", "DATE", "TIME", "TIMESTAMP", "DATETIME", "YEAR",
		"CHAR", "VARCHAR", "BINARY", "VARBINARY", "TINYTEXT", "TEXT", "MEDIUMTEXT", "LONGTEXT",
		"TINYBLOB", "BLOB", "MEDIUMBLOB", "LONGBLOB", "JSON", "ENUM", "SET":
		return true
	default:
		return false
	}
}
