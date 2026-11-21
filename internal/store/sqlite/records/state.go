package records

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

// ReadState observes identity, epoch and the number of declared-approved
// current heads in one short read transaction. It does not claim that those
// records are effective or fresh and never materializes record bodies. The
// caller must perform its own cross-store optimistic postcheck before output.
func (s *Store) ReadState(ctx context.Context, maxHeads int, authorize StateAuthorizer) (state State, resultErr error) {
	if s == nil || s.db == nil || s.lease == nil || ctx == nil || authorize == nil || maxHeads < 1 || maxHeads > 100000 || s.db.Stats().MaxOpenConnections != 1 {
		return State{}, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return State{}, ErrInvalidStore
	}
	if err := authorize(ctx, s.identity); err != nil {
		return State{}, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return State{}, err
	}
	conn, err := borrow(ctx, s.db, true)
	if err != nil {
		return State{}, err
	}
	defer func() {
		if err := release(ctx, conn, true); err != nil {
			state = State{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := s.requireReadJournal(ctx, conn); err != nil {
		return State{}, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return State{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			state = State{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, epoch, err := inspect(ctx, tx, s.identity.WorktreeID)
	if err != nil || version == 0 || identity != s.identity {
		return State{}, errors.Join(ErrStoreChanged, err)
	}
	var count, invalid, approved int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM record_heads LIMIT ?)`, maxHeads+1).Scan(&count); err != nil {
		return State{}, err
	}
	if count > int64(maxHeads) {
		return State{}, ErrLimit
	}
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(CASE WHEN v.record_id IS NULL OR v.declared_status NOT IN ('proposed','approved','revoked','superseded') THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN v.declared_status='approved' THEN 1 ELSE 0 END),0) FROM record_heads AS h LEFT JOIN record_versions AS v ON v.record_id=h.record_id AND v.version=h.version`).Scan(&invalid, &approved); err != nil {
		return State{}, err
	}
	if invalid != 0 || (epoch == 0 && count != 0) || approved > count {
		return State{}, ErrIntegrity
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return State{}, err
	}
	if err := authorize(ctx, identity); err != nil {
		return State{}, err
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return State{Identity: identity, RecordEpoch: epoch, ApprovedHeads: approved}, nil
}
