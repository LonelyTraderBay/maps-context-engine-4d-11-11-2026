package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrGroupUnavailable = errors.New("group is not available")

type groupMemberRow struct {
	ServiceID  string          `json:"service_id"`
	WorktreeID string          `json:"worktree_id"`
	ProfileID  string          `json:"profile_id"`
	Namespace  json.RawMessage `json:"namespace"`
}

// GroupMetadata reads one declared group in a short catalog transaction. It
// does not resolve generations, grant group/member access, paginate a list, or
// expose an administrative transport; those are later owner-task operations.
// The caller must authorize the actor before invoking it and revalidate the
// revision/incarnation before returning data from a broader operation.
func (s *Store) GroupMetadata(ctx context.Context, groupID string) (model.GroupSummary, error) {
	if !validUUID(groupID) {
		return model.GroupSummary{}, ErrGroupUnavailable
	}
	var output model.GroupSummary
	err := s.write(ctx, func(tx *sql.Tx) error {
		var name, status string
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT name,status,revision FROM repo_groups WHERE group_id=? AND length(name)<=256`, groupID).Scan(&name, &status, &revision)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGroupUnavailable
		}
		if err != nil {
			return err
		}
		if revision < 1 || revision > maxExactRevision || (status != "active" && status != "removed") {
			return ErrUnrecognizedStore
		}
		members, err := readGroupMembers(ctx, tx, groupID)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(struct {
			GroupID string           `json:"group_id"`
			Name    string           `json:"name"`
			Members []groupMemberRow `json:"members"`
		}{groupID, name, members})
		if err != nil {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		config, err := model.DecodeGroupConfigJSON(raw, maxAdminBytes)
		if err != nil {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		output = model.GroupSummary{Config: config, Revision: revision, Status: status}
		return nil
	})
	if err != nil {
		return model.GroupSummary{}, err
	}
	return output, nil
}

func readGroupMembers(ctx context.Context, tx *sql.Tx, groupID string) (members []groupMemberRow, resultErr error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM group_members WHERE group_id=?`, groupID).Scan(&count); err != nil {
		return nil, err
	}
	if count < 1 || count > 32 {
		return nil, ErrUnrecognizedStore
	}
	rows, err := tx.QueryContext(ctx, `SELECT service_id,worktree_id,profile_id,namespace_json FROM group_members WHERE group_id=? AND length(CAST(namespace_json AS BLOB))<=16384 ORDER BY service_id COLLATE BINARY LIMIT 33`, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	members = make([]groupMemberRow, 0, count)
	for rows.Next() {
		var item groupMemberRow
		var namespace string
		if err := rows.Scan(&item.ServiceID, &item.WorktreeID, &item.ProfileID, &namespace); err != nil {
			return nil, err
		}
		item.Namespace = json.RawMessage(namespace)
		members = append(members, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(members) != count {
		return nil, ErrUnrecognizedStore
	}
	return members, nil
}
