package index

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

// Authorize the matched route declarations before exposing their components.
// The source site can be omitted later when the graph output hits its budget.
func checkQuestionRoutePaths(ctx context.Context, tx *sql.Tx, generation, route string, checkPath func(string) error) (resultErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT
		CASE WHEN length(CAST(path AS BLOB))<=16384 THEN path END
		FROM symbols WHERE generation_id=? AND kind='route' AND name=? LIMIT 9`, generation, route)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for count := 0; rows.Next(); count++ {
		if count == 8 {
			return ErrCaptureLimit
		}
		var path sql.NullString
		if err := rows.Scan(&path); err != nil {
			return err
		}
		if !path.Valid {
			return ErrCaptureMismatch
		}
		if err := checkPath(path.String); err != nil {
			return err
		}
	}
	return rows.Err()
}

func questionSymbolOrder(question string) (predicate string, filterArgs []any, order string, orderArgs []any) {
	terms, titles := search.QuestionTerms(question)
	if len(terms) == 0 {
		return "0", nil, "s.symbol_id", nil
	}
	matches := make([]string, 0, len(terms))
	scores := make([]string, 0, len(terms)+len(titles))
	for _, term := range terms {
		matches = append(matches, "instr(lower(s.name),?)>0")
		filterArgs = append(filterArgs, term.Text)
		scores = append(scores, "(CASE WHEN lower(s.name)=? THEN ? WHEN instr(lower(s.name),?)>0 THEN ? ELSE 0 END + CASE WHEN instr(lower(s.path),?)>0 THEN 3 ELSE 0 END)")
		orderArgs = append(orderArgs, term.Text, term.Weight+8, term.Text, term.Weight, term.Text)
	}
	for _, title := range titles {
		scores = append(scores, "CASE WHEN instr(lower(s.path),?)>0 THEN 32 ELSE 0 END",
			"CASE WHEN instr(lower(s.qualified_name),'#'||?)>0 THEN 18 ELSE 0 END")
		orderArgs = append(orderArgs, title, title)
	}
	if search.QuestionMentionsEvent(question) {
		scores = append(scores, "CASE WHEN lower(s.name) LIKE 'handle%' THEN 16 WHEN lower(s.name) LIKE 'on%' THEN 8 ELSE 0 END")
	}
	predicate = "s.path IS NOT NULL AND s.kind NOT IN ('file','jsx_site','branch_arm','branch_condition','module','package') AND (" +
		strings.Join(matches, " OR ") + ")"
	order = "(" + strings.Join(scores, " + ") + ") DESC,s.path,s.start_byte,s.symbol_id"
	return predicate, filterArgs, order, orderArgs
}
