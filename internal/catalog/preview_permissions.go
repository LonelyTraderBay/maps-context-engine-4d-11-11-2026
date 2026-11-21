package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrPermissionUnavailable = errors.New("preview permission is unavailable")
	ErrPermissionInput       = errors.New("invalid preview permission")
)

const permissionBytes = 32768
const permissionHeads = `SELECT v.profile_id,v.version,v.declared_status,
CASE WHEN length(CAST(v.config_json AS BLOB))<=32768 THEN v.config_json END,
CASE WHEN length(CAST(v.config_digest AS BLOB))=64 THEN v.config_digest END
FROM preview_permission_heads h JOIN preview_permission_versions v ON v.profile_id=h.profile_id AND v.version=h.version`

type permissionScanner interface {
	Scan(...any) error
}

func scanPermission(row permissionScanner, now time.Time) (out model.PreviewPermissionSummary, err error) {
	var raw []byte
	err = row.Scan(&out.ProfileID, &out.Version, &out.DeclaredStatus, &raw, &out.Digest)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrPermissionUnavailable
	}
	if err != nil {
		return out, err
	}
	profile, err := model.DecodePreviewPermissionProfileJSON(raw, permissionBytes)
	if err != nil || profile.ProfileID != out.ProfileID || profile.Version != out.Version {
		return out, ErrUnrecognizedStore
	}
	digest, err := model.PreviewPermissionDigest(profile, permissionBytes)
	attested, attestedErr := time.Parse(time.RFC3339Nano, profile.AttestedAt)
	expiry, expiryErr := time.Parse(time.RFC3339Nano, profile.ExpiresAt)
	if err != nil || digest != out.Digest || attestedErr != nil || expiryErr != nil || !expiry.After(attested) ||
		model.CheckLocalPreviewLocation(profile.ApprovedOrigin, profile.ApprovedPathPrefix) != nil {
		return out, ErrUnrecognizedStore
	}
	out.Profile, out.EffectiveStatus = profile, out.DeclaredStatus
	switch out.DeclaredStatus {
	case "revoked":
	case "approved":
		if now.Before(attested) || !now.Before(expiry) {
			out.EffectiveStatus = "expired"
		}
	default:
		return out, ErrUnrecognizedStore
	}
	return out, nil
}

// ReadPreviewPermission reads the current immutable head on every invocation.
// It never renews the permission or performs a write as part of expiry.
func (s *Store) ReadPreviewPermission(ctx context.Context, id string, now time.Time, authorize func(context.Context) error) (model.PreviewPermissionSummary, error) {
	out := model.PreviewPermissionSummary{}
	if ctx == nil || now.IsZero() || id == "" || len(id) > 768 || authorize == nil {
		return out, ErrPermissionInput
	}
	if err := authorize(ctx); err != nil {
		return out, err
	}
	err := s.read(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = scanPermission(tx.QueryRowContext(ctx, permissionHeads+" WHERE h.profile_id=?", id), now)
		if err != nil {
			return err
		}
		return authorize(ctx)
	})
	if err != nil {
		return model.PreviewPermissionSummary{}, err
	}
	return out, nil
}

func (s *Store) ListPreviewPermissions(ctx context.Context, after string, limit int, now time.Time, authorize func(context.Context) error) ([]model.PreviewPermissionSummary, bool, error) {
	if ctx == nil || now.IsZero() || len(after) > 768 || limit < 1 || limit > 100 || authorize == nil {
		return nil, false, ErrPermissionInput
	}
	if err := authorize(ctx); err != nil {
		return nil, false, err
	}
	out := make([]model.PreviewPermissionSummary, 0, limit+1)
	err := s.read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, permissionHeads+" WHERE h.profile_id>? ORDER BY h.profile_id LIMIT ?", after, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanPermission(rows, now)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		return errors.Join(rows.Err(), rows.Close(), authorize(ctx))
	})
	if err != nil {
		return nil, false, err
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}

// ChangePreviewPermission runs an append and head CAS in the authorized
// catalog transaction. actor is supplied by the installation owner.
func (s *Store) ChangePreviewPermission(ctx context.Context, request model.PreviewPermissionAdminRequest, actor string, now time.Time) (model.PreviewPermissionSummary, error) {
	out := model.PreviewPermissionSummary{}
	if s == nil || ctx == nil || s.lease == nil || s.mutation == nil || s.authorize == nil || actor == "" || now.IsZero() {
		return out, ErrCatalogWriterRequired
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return out, ErrPermissionInput
	}
	request, err = model.DecodePreviewPermissionAdminRequestJSON(raw, permissionBytes)
	if err != nil || (request.Action != "put" && request.Action != "revoke") {
		return out, ErrPermissionInput
	}
	var profile model.PreviewPermissionProfile
	var id, reason string
	expected := *request.ExpectedVersion
	if request.Reason != nil {
		reason = *request.Reason
	}
	status := "approved"
	if request.Action == "put" {
		profile, id = *request.Profile, request.Profile.ProfileID
		if profile.AttestedBy != actor || checkNewPermission(profile, now) != nil {
			return out, ErrPermissionInput
		}
	} else {
		id, status = *request.ProfileID, "revoked"
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		current, err := scanPermission(tx.QueryRowContext(ctx, permissionHeads+" WHERE h.profile_id=?", id), now)
		if errors.Is(err, ErrPermissionUnavailable) {
			if expected != 0 {
				return ErrRevisionConflict
			}
		} else if err != nil {
			return err
		} else if expected != current.Version {
			return ErrRevisionConflict
		}
		if status == "revoked" {
			if current.DeclaredStatus != "approved" || expected >= 9007199254740991 {
				return ErrRevisionConflict
			}
			profile = current.Profile
			profile.Version++
		} else {
			for _, worktree := range profile.SourceWorktreeIDs {
				if err := requireRegistered(ctx, tx, worktree); err != nil {
					return err
				}
			}
		}
		raw, err := json.Marshal(profile)
		if err != nil {
			return err
		}
		digest, err := model.PreviewPermissionDigest(profile, permissionBytes)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO preview_permission_versions
 (profile_id,version,declared_status,config_json,config_digest,actor_id,reason,created_at)
 VALUES(?,?,?,?,?,?,?,?)`, id, profile.Version, status, string(raw), digest, actor, reason, now.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if expected == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO preview_permission_heads(profile_id,version) VALUES(?,?)`, id, profile.Version)
		} else {
			var updated sql.Result
			updated, err = tx.ExecContext(ctx, `UPDATE preview_permission_heads SET version=? WHERE profile_id=? AND version=?`, profile.Version, id, expected)
			if err == nil {
				n, countErr := updated.RowsAffected()
				if countErr != nil || n != 1 {
					return errors.Join(ErrRevisionConflict, countErr)
				}
			}
		}
		if err != nil {
			return err
		}
		out, err = scanPermission(tx.QueryRowContext(ctx, permissionHeads+" WHERE h.profile_id=?", id), now)
		return err
	})
	if err != nil {
		return model.PreviewPermissionSummary{}, err
	}
	return out, nil
}

func checkNewPermission(profile model.PreviewPermissionProfile, now time.Time) error {
	attested, aErr := time.Parse(time.RFC3339Nano, profile.AttestedAt)
	expires, eErr := time.Parse(time.RFC3339Nano, profile.ExpiresAt)
	if aErr != nil || eErr != nil || now.Before(attested) || !now.Before(expires) ||
		model.CheckLocalPreviewLocation(profile.ApprovedOrigin, profile.ApprovedPathPrefix) != nil ||
		!profile.AllowAttach && (profile.AllowInteract || profile.AllowScroll) {
		return ErrPermissionInput
	}
	for _, dest := range profile.AllowedDevDestinations {
		if model.CheckPreviewLocation(dest, "/") != nil {
			return ErrPermissionInput
		}
	}
	return nil
}
