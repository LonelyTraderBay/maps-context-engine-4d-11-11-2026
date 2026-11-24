package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func profileFingerprints(profile model.EffectiveProfile) (toolchain, provider string) {
	switch p := profile.(type) {
	case *model.GoProfile:
		return p.ToolchainDigest, p.ProviderDigest
	case *model.TSProfile:
		return p.NodeDigest, p.ProviderDigest
	case *model.PythonProfile:
		return p.InterpreterDigest, p.ProviderDigest
	case *model.PowerShellProfile:
		return p.ExecutableDigest, p.ProviderDigest
	default:
		return "", ""
	}
}

// SetProfile serves the Go-only registration owners.
func (s *Store) SetProfile(ctx context.Context, raw []byte) (model.ProfileAdminData, error) {
	request, err := model.DecodeProfileAdminRequestJSON(raw, maxAdminBytes)
	if err != nil {
		return model.ProfileAdminData{}, err
	}
	if _, ok := request.Profile.(*model.GoProfile); !ok {
		return model.ProfileAdminData{}, ErrUnsupportedProfile
	}
	return s.setProfile(ctx, request)
}

// SetAddonProfile requires the native language owner to verify installed material
// and authorize the scope before storing the immutable profile.
func (s *Store) SetAddonProfile(ctx context.Context, raw []byte) (model.ProfileAdminData, error) {
	request, err := model.DecodeProfileAdminRequestJSON(raw, maxAdminBytes)
	if err != nil {
		return model.ProfileAdminData{}, err
	}
	switch request.Profile.(type) {
	case *model.TSProfile, *model.PythonProfile, *model.PowerShellProfile:
		return s.setProfile(ctx, request)
	default:
		return model.ProfileAdminData{}, ErrUnsupportedProfile
	}
}

func (s *Store) setProfile(ctx context.Context, request model.ProfileAdminRequest) (model.ProfileAdminData, error) {
	if !validUUID(request.WorktreeID) || request.ExpectedRevision < 0 || request.ExpectedRevision >= maxExactRevision {
		return model.ProfileAdminData{}, ErrRevisionConflict
	}
	profile := request.Profile
	digest, err := model.ProfileDigest(profile, maxAdminBytes)
	if err != nil {
		return model.ProfileAdminData{}, err
	}
	toolchainDigest, providerDigest := profileFingerprints(profile)
	config, err := json.Marshal(profile)
	if err != nil {
		return model.ProfileAdminData{}, err
	}
	next := request.ExpectedRevision + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = s.write(ctx, func(tx *sql.Tx) error {
		if err := requireRegistered(ctx, tx, request.WorktreeID); err != nil {
			return err
		}
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM profile_heads WHERE worktree_id=? AND profile_name=?`, request.WorktreeID, request.ProfileName).Scan(&revision)
		missing := errors.Is(err, sql.ErrNoRows)
		if err != nil && !missing {
			return err
		}
		if (missing && request.ExpectedRevision != 0) || (!missing && revision != request.ExpectedRevision) {
			return ErrRevisionConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO profiles(worktree_id,profile_id,config_json,toolchain_digest,provider_digest,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(worktree_id,profile_id) DO NOTHING`, request.WorktreeID, digest, string(config), toolchainDigest, providerDigest, now); err != nil {
			return err
		}
		var stored, toolchain, provider string
		if err = tx.QueryRowContext(ctx, `SELECT config_json,toolchain_digest,provider_digest FROM profiles WHERE worktree_id=? AND profile_id=?`, request.WorktreeID, digest).Scan(&stored, &toolchain, &provider); err != nil {
			return err
		}
		decoded, err := model.DecodeEffectiveProfileJSON([]byte(stored), maxAdminBytes)
		if err != nil {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		storedDigest, err := model.ProfileDigest(decoded, maxAdminBytes)
		if err != nil || storedDigest != digest || toolchain != toolchainDigest || provider != providerDigest {
			return errors.Join(ErrUnrecognizedStore, err)
		}
		if missing {
			_, err = tx.ExecContext(ctx, `INSERT INTO profile_heads(worktree_id,profile_name,profile_id,revision) VALUES(?,?,?,?)`, request.WorktreeID, request.ProfileName, digest, next)
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE profile_heads SET profile_id=?,revision=? WHERE worktree_id=? AND profile_name=? AND revision=?`, digest, next, request.WorktreeID, request.ProfileName, request.ExpectedRevision)
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
		return model.ProfileAdminData{}, err
	}
	return model.ProfileAdminData{Operation: "profile_admin", WorktreeID: request.WorktreeID, ProfileName: request.ProfileName, ProfileID: digest, Revision: next}, nil
}
