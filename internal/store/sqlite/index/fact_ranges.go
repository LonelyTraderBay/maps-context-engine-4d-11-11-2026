package index

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// checkFactRanges checks every local symbol, relation site and module source
// against the immutable captured bytes, with a bounded number of facts.
func checkFactRanges(ctx context.Context, db publicationQueryer, worktree string, capture CaptureWrite, manifest model.CaptureManifest, maxFacts int) error {
	if ctx == nil || db == nil || capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || maxFacts < 1 || maxFacts > 1000000 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	var oversized int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM symbols WHERE generation_id=? AND length(CAST(path AS BLOB))>16384 UNION ALL SELECT 1 FROM relations WHERE generation_id=? AND length(CAST(site_path AS BLOB))>16384 UNION ALL SELECT 1 FROM modules WHERE generation_id=? AND length(CAST(source_path AS BLOB))>16384 LIMIT 1`, capture.GenerationID, capture.GenerationID, capture.GenerationID).Scan(&oversized)
	if err == nil {
		return ErrPublishIntegrity
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	paths := map[string]bool{}
	count := 0
	check := func(name string, start, end int64) error {
		if policy.CheckPath(name) != nil {
			return ErrPublishIntegrity
		}
		if err := capture.Scanner.CheckContent([]byte(name)); err != nil {
			return err
		}
		if err := capture.Authorize(ctx, worktree, name, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
		file, ok := capture.Snapshot.File(name)
		if !ok || start < 0 || end < start || end > int64(file.Len()) {
			return ErrPublishIntegrity
		}
		if err := file.CheckRange(int(start), int(end)); err != nil {
			return ErrPublishIntegrity
		}
		paths[name] = true
		return nil
	}
	queries := []struct {
		sql      string
		optional bool
	}{
		{`SELECT path,start_byte,end_byte FROM symbols WHERE generation_id=? ORDER BY symbol_id`, true},
		{`SELECT site_path,start_byte,end_byte FROM relations WHERE generation_id=? ORDER BY relation_id`, false},
		{`SELECT source_path,start_byte,end_byte FROM modules WHERE generation_id=? ORDER BY module_id`, true},
	}
	for _, query := range queries {
		rows, err := db.QueryContext(ctx, query.sql, capture.GenerationID)
		if err != nil {
			return err
		}
		var scanErr error
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				scanErr = err
				break
			}
			count++
			if count > maxFacts {
				scanErr = ErrCaptureLimit
				break
			}
			var name sql.NullString
			var start, end sql.NullInt64
			if err := rows.Scan(&name, &start, &end); err != nil {
				scanErr = err
				break
			}
			if !name.Valid {
				if !query.optional || start.Valid || end.Valid {
					scanErr = ErrPublishIntegrity
					break
				}
				continue
			}
			if !start.Valid || !end.Valid {
				scanErr = ErrPublishIntegrity
				break
			}
			if err := check(name.String, start.Int64, end.Int64); err != nil {
				scanErr = err
				break
			}
		}
		if err := rows.Err(); err != nil {
			scanErr = errors.Join(scanErr, err)
		}
		if err := rows.Close(); err != nil {
			scanErr = errors.Join(scanErr, err)
		}
		if scanErr != nil {
			return scanErr
		}
	}
	ordered := make([]string, 0, len(paths))
	for name := range paths {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		if err := capture.Authorize(ctx, worktree, name, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
	}
	return ctx.Err()
}
