package index

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// ActiveGenerationAbsent reports only whether the active pointer is absent.
// A pointer to an invalid or unavailable generation is not an absence proof.
// The caller must independently authorize catalog scope and postcheck before
// exposing the observation; this is not an atomic cross-store snapshot.
func (s *Store) ActiveGenerationAbsent(ctx context.Context, profileID string, authorize func(context.Context, Identity, string) error) (absent bool, resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || !validHash(profileID) || authorize == nil {
		return false, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return false, ErrInvalidStore
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return false, err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			absent = false
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return false, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			absent = false
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, err := inspectStore(ctx, tx, s.identity.WorktreeID)
	if err != nil {
		return false, err
	}
	if version != supportedSchema || identity != s.identity {
		return false, ErrStoreChanged
	}
	if err := authorize(ctx, identity, profileID); err != nil {
		return false, err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM active_generations WHERE profile_id=?)`, profileID).Scan(&exists); err != nil {
		return false, err
	}
	if exists != 0 && exists != 1 {
		return false, ErrPublishIntegrity
	}
	if err := authorize(ctx, identity, profileID); err != nil {
		return false, err
	}
	if err := s.lease.Check(ctx, identity.WorktreeID, ports.SharedLease); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return exists == 0, nil
}
