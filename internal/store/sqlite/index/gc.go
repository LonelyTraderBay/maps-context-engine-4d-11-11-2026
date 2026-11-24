package index

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var ErrGCIntegrity = errors.New("index garbage collection integrity check failed")

// GCAuthorizer grants maintenance of this exact derived index/profile. It is
// supplied by the trusted owner, never inferred from a path or tool argument.
type GCAuthorizer func(context.Context, string, string) error

type GCRequest struct {
	ProfileID       string
	JobID           string
	KeepGenerations int
	Writer          ports.IndexWriterLease
	Authorize       GCAuthorizer
	purge           bool
}

// PurgeDerivedIndex clears this authenticated worktree index under an
// exclusive lifecycle lease. Catalog, records and source are outside this
// transaction. A live or abandoned BUILDING generation needs recovery first.
func (s *Store) PurgeDerivedIndex(ctx context.Context, job string, writer ports.IndexWriterLease, authorize func(context.Context, string) error) error {
	if authorize == nil {
		return ErrInvalidStore
	}
	request := GCRequest{JobID: job, Writer: writer, purge: true, Authorize: func(ctx context.Context, worktree, _ string) error {
		return authorize(ctx, worktree)
	}}
	return s.gcTransaction(ctx, request, true, func(tx *sql.Tx, check func() error) error {
		rows, err := tx.QueryContext(ctx, `SELECT generation_id,state FROM generations ORDER BY generation_id LIMIT 129`)
		if err != nil {
			return err
		}
		var generations []string
		for rows.Next() {
			var generation, state string
			if err := rows.Scan(&generation, &state); err != nil {
				return errors.Join(err, rows.Close())
			}
			if !validUUID(generation) || state == "BUILDING" || len(generations) == 128 {
				return errors.Join(ErrGenerationConflict, rows.Close())
			}
			generations = append(generations, generation)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM active_generations`); err != nil {
			return err
		}
		for _, generation := range generations {
			result := GCResult{GenerationID: generation, Rows: make(map[string]int64)}
			if err := s.collectGCGeneration(ctx, tx, generation, true, &result, check); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM content_blobs`); err != nil {
			return err
		}
		if s.schema == uiAddonSchema {
			if _, err := tx.ExecContext(ctx, `DELETE FROM ui_build_maps`); err != nil {
				return err
			}
		}
		return checkGCForeignKeys(ctx, tx)
	})
}

type GCPlan struct {
	GenerationIDs []string
	More          bool
}

// GCResult reports logical row/blob removal, not filesystem space reclaimed
// or secure erasure. Each applied generation is its own atomic transaction.
type GCResult struct {
	GenerationID  string
	Applied       bool
	AlreadyAbsent bool
	Rows          map[string]int64
	BlobBytes     int64
}

const gcCandidates = `WITH ranked AS (
 SELECT g.generation_id,ROW_NUMBER() OVER(ORDER BY
 CASE WHEN EXISTS(SELECT 1 FROM active_generations a WHERE a.generation_id=g.generation_id) THEN 0 ELSE 1 END,
 COALESCE(g.completed_at,g.created_at) DESC,g.generation_id DESC) AS position
 FROM generations g WHERE g.profile_id=? AND g.state<>'BUILDING'
) SELECT CASE WHEN length(CAST(generation_id AS BLOB))=36 THEN generation_id END FROM ranked WHERE position>? AND (?='' OR generation_id=?) ORDER BY position DESC LIMIT ?`

// PlanGC is read-only, bounded and deterministic. Retention always includes
// the active generation, even if it is older than other terminal generations.
// BUILDING generations are recovery-owned and never GC candidates.
func (s *Store) PlanGC(ctx context.Context, request GCRequest, limit int) (output GCPlan, resultErr error) {
	if limit < 1 || limit > 128 {
		return output, ErrInvalidStore
	}
	err := s.gcTransaction(ctx, request, false, func(tx *sql.Tx, check func() error) error {
		rows, err := tx.QueryContext(ctx, gcCandidates, request.ProfileID, request.KeepGenerations, "", "", limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var generation string
			if err := rows.Scan(&generation); err != nil {
				return err
			}
			if !validUUID(generation) {
				return ErrGCIntegrity
			}
			output.GenerationIDs = append(output.GenerationIDs, generation)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		output.More = len(output.GenerationIDs) > limit
		if output.More {
			output.GenerationIDs = output.GenerationIDs[:limit]
		}
		return errors.Join(rows.Close(), check())
	})
	if err != nil {
		return GCPlan{}, err
	}
	return output, nil
}

// CollectGeneration rechecks retention and authority in the same transaction
// as deletion. apply=false performs SELECTs only; it does not retire rows or
// simulate deletion with rollback. No catalog, records or source is opened.
func (s *Store) CollectGeneration(ctx context.Context, request GCRequest, generation string, apply bool) (output GCResult, resultErr error) {
	if !validUUID(generation) {
		return output, ErrInvalidStore
	}
	output = GCResult{GenerationID: generation, Rows: make(map[string]int64)}
	err := s.gcTransaction(ctx, request, apply, func(tx *sql.Tx, check func() error) error {
		var candidate string
		err := tx.QueryRowContext(ctx, gcCandidates, request.ProfileID, request.KeepGenerations, generation, generation, 1).Scan(&candidate)
		if errors.Is(err, sql.ErrNoRows) {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM generations WHERE generation_id=?`, generation).Scan(&exists); err != nil {
				return err
			}
			if exists != 0 {
				return ErrGenerationConflict
			}
			output.AlreadyAbsent = true
			return check()
		}
		if err != nil || candidate != generation {
			return errors.Join(ErrGCIntegrity, err)
		}
		return s.collectGCGeneration(ctx, tx, generation, apply, &output, check)
	})
	if err != nil {
		return GCResult{GenerationID: generation}, err
	}
	output.Applied = apply && !output.AlreadyAbsent
	return output, nil
}

func (s *Store) collectGCGeneration(ctx context.Context, tx *sql.Tx, generation string, apply bool, output *GCResult, check func() error) error {
	scanner, err := policy.NewScanner(2048)
	if err != nil {
		return err
	}
	if err := checkModuleAcyclic(ctx, tx, generation, 100000, scanner); err != nil {
		return errors.Join(ErrGCIntegrity, err)
	}
	tables := []string{"relations", "module_members", "search_documents", "diagnostics", "capability_results", "symbols", "modules", "files", "packages", "generations"}
	if s.schema == uiAddonSchema {
		tables = []string{"ui_build_bindings", "entity_links", "state_transitions", "contract_fields",
			"relations", "module_members", "search_documents", "diagnostics", "capability_results",
			"symbols", "domain_entities", "modules", "files", "packages", "generations"}
	}
	for _, table := range tables {
		var count int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+table+gcGenerationFilter(table), generation).Scan(&count); err != nil {
			return err
		}
		output.Rows[table] = count
	}
	var blobs int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(byte_length),0) FROM content_blobs b WHERE NOT EXISTS(SELECT 1 FROM files f WHERE f.content_hash=b.content_hash AND f.generation_id<>?)`, generation).Scan(&blobs, &output.BlobBytes); err != nil {
		return err
	}
	output.Rows["content_blobs"] = blobs
	if !apply {
		return check()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE generations SET state='RETIRED' WHERE generation_id=? AND state IN ('READY','READY_PARTIAL')`, generation); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	for _, table := range tables {
		if table == "modules" {
			if err := deleteGCModules(ctx, tx, generation, output.Rows[table], check); err != nil {
				return err
			}
		} else {
			result, err := tx.ExecContext(ctx, `DELETE FROM `+table+gcGenerationFilter(table), generation)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil || n != output.Rows[table] {
				return errors.Join(ErrGCIntegrity, err)
			}
		}
		if err := check(); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM content_blobs WHERE NOT EXISTS(SELECT 1 FROM files f WHERE f.content_hash=content_blobs.content_hash)`)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n != output.Rows["content_blobs"] {
		return errors.Join(ErrGCIntegrity, err)
	}
	return errors.Join(checkGCForeignKeys(ctx, tx), check())
}

func gcGenerationFilter(table string) string {
	if table == "ui_build_bindings" {
		return " WHERE source_generation_id=?"
	}
	return " WHERE generation_id=?"
}

func deleteGCModules(ctx context.Context, tx *sql.Tx, generation string, remaining int64, check func() error) error {
	for remaining > 0 {
		result, err := tx.ExecContext(ctx, `DELETE FROM modules WHERE generation_id=? AND module_id IN (SELECT m.module_id FROM modules m WHERE m.generation_id=? AND NOT EXISTS(SELECT 1 FROM modules child WHERE child.generation_id=m.generation_id AND child.parent_module_id=m.module_id) ORDER BY m.module_id LIMIT 256)`, generation, generation)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n < 1 || n > remaining {
			return errors.Join(ErrGCIntegrity, err)
		}
		remaining -= n
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func checkGCForeignKeys(ctx context.Context, tx *sql.Tx) (resultErr error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	if rows.Next() {
		return ErrGCIntegrity
	}
	return rows.Err()
}

func (s *Store) gcTransaction(ctx context.Context, request GCRequest, apply bool, action func(*sql.Tx, func() error) error) (resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || !validUUID(request.JobID) || request.Writer == nil || request.Authorize == nil {
		return ErrInvalidStore
	}
	mode := ports.SharedLease
	if request.purge {
		mode = ports.ExclusiveLease
	} else if !validHash(request.ProfileID) || request.KeepGenerations < 1 || request.KeepGenerations > 1000 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.lease.Check(ctx, s.identity.WorktreeID, mode); err != nil {
			return err
		}
		if err := request.Writer.CheckWriter(ctx, s.identity.WorktreeID, request.JobID); err != nil {
			return err
		}
		return request.Authorize(ctx, s.identity.WorktreeID, request.ProfileID)
	}
	guard, err := request.Writer.HoldWriter(ctx, s.identity.WorktreeID, request.JobID)
	if err != nil {
		return err
	}
	if guard == nil {
		return ErrInvalidStore
	}
	committed := false
	defer func() {
		if err := guard.Close(); err != nil {
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := check(); err != nil {
		return err
	}
	conn, err := borrowConfigured(ctx, s.writer, !apply)
	if err != nil {
		return err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, !apply); err != nil {
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: !apply})
	if err != nil {
		return err
	}
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	_, err = s.inspectPinned(ctx, tx)
	if err != nil {
		return err
	}
	var invalid int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM active_generations a LEFT JOIN generations g ON g.generation_id=a.generation_id AND g.profile_id=a.profile_id WHERE g.generation_id IS NULL OR g.state NOT IN ('READY','READY_PARTIAL')`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return ErrGCIntegrity
	}
	if err := checkGCForeignKeys(ctx, tx); err != nil {
		return err
	}
	if err := action(tx, check); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if !apply {
		return nil
	}
	if err := tx.Commit(); err != nil {
		return errors.Join(ErrCommitUnknown, err)
	}
	committed = true
	return nil
}
