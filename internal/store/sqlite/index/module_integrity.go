package index

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// checkModuleAcyclic validates the same-generation parent forest with bounded
// memory. SQLite foreign keys alone do not reject cycles.
func checkModuleAcyclic(ctx context.Context, db publicationQueryer, generation string, maxModules int, scanner *policy.Scanner) (resultErr error) {
	if ctx == nil || db == nil || scanner == nil || !validUUID(generation) || maxModules < 1 || maxModules > 100000 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	var oversized int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM modules WHERE generation_id=? AND (length(CAST(module_id AS BLOB))>768 OR length(CAST(parent_module_id AS BLOB))>768 OR length(CAST(label AS BLOB))>2048 OR length(CAST(kind AS BLOB))>768 OR length(CAST(plane AS BLOB))>64) LIMIT 1`, generation).Scan(&oversized)
	if err == nil {
		return ErrPublishIntegrity
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rows, err := db.QueryContext(ctx, `SELECT module_id,parent_module_id,label,kind,plane FROM modules WHERE generation_id=? ORDER BY module_id`, generation)
	if err != nil {
		return err
	}
	parents := make(map[string]sql.NullString)
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var id, label, kind, plane string
		var parent sql.NullString
		if err := rows.Scan(&id, &parent, &label, &kind, &plane); err != nil {
			return err
		}
		if !validModuleID(id) || parent.Valid && !validModuleID(parent.String) {
			return ErrPublishIntegrity
		}
		if !utf8.ValidString(label) || utf8.RuneCountInString(label) < 1 || utf8.RuneCountInString(label) > 512 || !validModuleID(kind) || (plane != "intended" && plane != "observed_static") {
			return ErrPublishIntegrity
		}
		for _, value := range []string{id, label, kind, plane} {
			if err := scanner.CheckContent([]byte(value)); err != nil {
				return err
			}
		}
		if parent.Valid {
			if err := scanner.CheckContent([]byte(parent.String)); err != nil {
				return err
			}
		}
		if len(parents) >= maxModules {
			return ErrCaptureLimit
		}
		if _, exists := parents[id]; exists {
			return ErrPublishIntegrity
		}
		parents[id] = parent
	}
	if err := rows.Err(); err != nil {
		return err
	}
	colors := make(map[string]uint8, len(parents))
	for id := range parents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if colors[id] == 2 {
			continue
		}
		current := id
		var stack []string
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			if colors[current] == 1 {
				return ErrPublishIntegrity
			}
			if colors[current] == 2 {
				break
			}
			parent, exists := parents[current]
			if !exists {
				return ErrPublishIntegrity
			}
			colors[current] = 1
			stack = append(stack, current)
			if !parent.Valid {
				break
			}
			current = parent.String
		}
		for _, member := range stack {
			colors[member] = 2
		}
	}
	return nil
}

func validModuleID(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 1 && utf8.RuneCountInString(value) <= 192
}
