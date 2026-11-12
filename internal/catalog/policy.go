package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const maxAdminBytes = 1 << 20
const maxExactRevision int64 = 9007199254740991

// SetPolicy applies a validated CLI-only policy request with epoch CAS. The
// caller must authorize the actor and bind the request to a registered scope;
// this method is not exposed over MCP or HTTP.
func (s *Store) SetPolicy(ctx context.Context, raw []byte) (model.PolicyAdminData, error) {
	request, err := model.DecodePolicyAdminRequestJSON(raw, maxAdminBytes)
	if err != nil {
		return model.PolicyAdminData{}, err
	}
	if request.ExpectedEpoch >= maxExactRevision {
		return model.PolicyAdminData{}, ErrRevisionConflict
	}
	digest, err := model.PolicyDigest(request.Policy, maxAdminBytes)
	if err != nil {
		return model.PolicyAdminData{}, err
	}
	config, err := json.Marshal(request.Policy)
	if err != nil {
		return model.PolicyAdminData{}, err
	}
	next := request.ExpectedEpoch + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = s.write(ctx, func(tx *sql.Tx) error {
		if err := requireRegistered(ctx, tx, request.WorktreeID); err != nil {
			return err
		}
		var epoch int64
		err := tx.QueryRowContext(ctx, `SELECT policy_epoch FROM worktree_policy_heads WHERE worktree_id=?`, request.WorktreeID).Scan(&epoch)
		missing := errors.Is(err, sql.ErrNoRows)
		if err != nil && !missing {
			return err
		}
		if (missing && request.ExpectedEpoch != 0) || (!missing && epoch != request.ExpectedEpoch) {
			return ErrRevisionConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO policies(worktree_id,policy_digest,config_json,created_at) VALUES(?,?,?,?) ON CONFLICT(worktree_id,policy_digest) DO NOTHING`, request.WorktreeID, digest, string(config), now); err != nil {
			return err
		}
		var stored string
		if err = tx.QueryRowContext(ctx, `SELECT config_json FROM policies WHERE worktree_id=? AND policy_digest=?`, request.WorktreeID, digest).Scan(&stored); err != nil {
			return err
		}
		decoded, err := model.DecodePolicyConfigJSON([]byte(stored), maxAdminBytes)
		if err != nil {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		storedDigest, err := model.PolicyDigest(decoded, maxAdminBytes)
		if err != nil || storedDigest != digest {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		if missing {
			_, err = tx.ExecContext(ctx, `INSERT INTO worktree_policy_heads(worktree_id,policy_digest,policy_epoch,updated_at) VALUES(?,?,?,?)`, request.WorktreeID, digest, next, now)
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE worktree_policy_heads SET policy_digest=?,policy_epoch=?,updated_at=? WHERE worktree_id=? AND policy_epoch=?`, digest, next, now, request.WorktreeID, request.ExpectedEpoch)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrRevisionConflict
		}
		return nil
	})
	if err != nil {
		return model.PolicyAdminData{}, err
	}
	return model.PolicyAdminData{Operation: "policy_admin", WorktreeID: request.WorktreeID, PolicyDigest: digest, PolicyEpoch: next}, nil
}
