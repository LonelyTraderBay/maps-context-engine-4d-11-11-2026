package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type GroupPage struct {
	Groups             []model.GroupSummary
	CatalogIncarnation string
	RevisionDigest     string
	More               bool
}

// ListGroups reads the ordered revision inventory and a keyset page inside
// the same catalog snapshot. The digest changes on a put or tombstone.
func (s *Store) ListGroups(ctx context.Context, after string, limit int) (GroupPage, error) {
	if after != "" && !validUUID(after) || limit < 1 || limit > 100 {
		return GroupPage{}, ErrGroupInput
	}
	out := GroupPage{Groups: make([]model.GroupSummary, 0, limit)}
	err := s.read(ctx, func(tx *sql.Tx) error {
		digest := sha256.New()
		digest.Write([]byte("MCE4D-GROUP-LIST1\x00" + s.incarnation + "\x00"))
		rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(group_id AS BLOB))=36 THEN group_id END,revision,status
FROM repo_groups ORDER BY group_id COLLATE BINARY`)
		if err != nil {
			return err
		}
		defer rows.Close()
		next := make([]string, 0, limit+1)
		for rows.Next() {
			var id, status string
			var revision int64
			if err := rows.Scan(&id, &revision, &status); err != nil {
				return err
			}
			if !validUUID(id) || revision < 1 || revision > maxExactRevision || (status != "active" && status != "removed") {
				return ErrUnrecognizedStore
			}
			digest.Write([]byte(id + ":" + strconv.FormatInt(revision, 10) + ":" + status + "\n"))
			if id > after && len(next) < limit+1 {
				next = append(next, id)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		out.CatalogIncarnation = s.incarnation
		out.RevisionDigest = hex.EncodeToString(digest.Sum(nil))
		out.More = len(next) > limit
		for _, id := range next[:min(len(next), limit)] {
			group, err := groupMetadata(ctx, tx, id)
			if err != nil {
				return err
			}
			out.Groups = append(out.Groups, group)
		}
		return nil
	})
	if err != nil {
		return GroupPage{}, err
	}
	return out, nil
}
