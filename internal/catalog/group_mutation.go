package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrGroupInput = errors.New("invalid group administrative request")

// ChangeGroup applies one full configuration or a tombstone under the
// protected catalog mutation lease. Membership replacement commits with CAS.
func (s *Store) ChangeGroup(ctx context.Context, request model.GroupAdminRequest, now time.Time) (model.GroupSummary, error) {
	var out model.GroupSummary
	if s == nil || ctx == nil || s.lease == nil || s.mutation == nil || s.authorize == nil {
		return out, ErrCatalogWriterRequired
	}
	raw, err := json.Marshal(request)
	if err != nil || now.IsZero() {
		return out, ErrGroupInput
	}
	request, err = model.DecodeGroupAdminRequestJSON(raw, maxAdminBytes)
	if err != nil || (request.Action != "put" && request.Action != "remove") || request.Limit != nil || request.Cursor != nil {
		return out, ErrGroupInput
	}
	expected := *request.ExpectedRevision
	if expected >= maxExactRevision {
		return out, ErrRevisionConflict
	}
	at := now.UTC().Format(time.RFC3339Nano)
	err = s.write(ctx, func(tx *sql.Tx) error {
		if request.Action == "remove" {
			id := *request.GroupID
			result, err := tx.ExecContext(ctx, `UPDATE repo_groups SET status='removed',revision=revision+1,updated_at=?
WHERE group_id=? AND revision=? AND status='active'`, at, id, expected)
			if err := checkGroupCAS(result, err); err != nil {
				return err
			}
			out, err = groupMetadata(ctx, tx, id)
			return err
		}
		config := *request.Config
		if err := putGroup(ctx, tx, config, expected, at); err != nil {
			return err
		}
		out, err = groupMetadata(ctx, tx, config.GroupID)
		return err
	})
	if err != nil {
		return model.GroupSummary{}, err
	}
	return out, nil
}

func putGroup(ctx context.Context, tx *sql.Tx, config model.GroupConfig, expected int64, at string) error {
	var nameTaken int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM repo_groups WHERE name=? AND group_id<>?)`,
		config.Name, config.GroupID).Scan(&nameTaken); err != nil {
		return err
	}
	if nameTaken != 0 {
		return ErrRevisionConflict
	}
	var result sql.Result
	var err error
	if expected == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO repo_groups(group_id,name,status,revision,created_at,updated_at)
VALUES(?,?,'active',1,?,?) ON CONFLICT(group_id) DO NOTHING`, config.GroupID, config.Name, at, at)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE repo_groups SET name=?,status='active',revision=revision+1,updated_at=?
WHERE group_id=? AND revision=?`, config.Name, at, config.GroupID, expected)
	}
	if err := checkGroupCAS(result, err); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_members WHERE group_id=?`, config.GroupID); err != nil {
		return err
	}
	seen := make(map[string]bool, len(config.Members))
	for _, member := range config.Members {
		if seen[member.ServiceID] {
			return ErrGroupInput
		}
		seen[member.ServiceID] = true
		if err := requireRegistered(ctx, tx, member.WorktreeID); err != nil {
			return err
		}
		var profile int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM profiles WHERE worktree_id=? AND profile_id=?)`,
			member.WorktreeID, member.ProfileID).Scan(&profile); err != nil {
			return err
		}
		if profile == 0 {
			return ErrGroupInput
		}
		namespace, err := json.Marshal(member.Namespace)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO group_members(group_id,service_id,worktree_id,profile_id,namespace_json)
VALUES(?,?,?,?,?)`, config.GroupID, member.ServiceID, member.WorktreeID, member.ProfileID, string(namespace)); err != nil {
			return err
		}
	}
	return nil
}

func checkGroupCAS(result sql.Result, err error) error {
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
}
