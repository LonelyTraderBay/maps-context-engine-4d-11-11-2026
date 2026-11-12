package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var (
	ErrInvalidGitMetadata = errors.New("Git metadata cannot be verified")
	ErrRegistrationExists = errors.New("worktree is already registered")
	ErrRootNotAllowed     = errors.New("worktree is outside policy roots")
	ErrRootRebindRequired = errors.New("registered root identity changed; explicit rebind required")
)

// Registration is an internal result; no CLI or MCP route is exposed here.
type Registration struct {
	RepoID          string
	WorktreeID      string
	BindingRevision int64
	PolicyDigest    string
	PolicyEpoch     int64
	ProfileID       string
	ProfileRevision int64
}

// Register creates a worktree, policy and profile atomically. The caller must
// authorize the administrator, both roots and policy roots, hold the lifecycle lease, and
// pass roots opened by the secure paths adapter. A nonempty linkedRelative
// identifies the explicit worktrees/<name> child in the approved common root.
// This function never executes Git or changes the target worktree.
func (s *Store) Register(ctx context.Context, worktree, common *paths.Root, linkedRelative, profileName string, policyJSON, profileJSON []byte) (Registration, error) {
	if s == nil || s.db == nil {
		return Registration{}, ErrUnrecognizedStore
	}
	if ctx == nil || worktree == nil || common == nil {
		return Registration{}, ErrInvalidGitMetadata
	}
	if err := ctx.Err(); err != nil {
		return Registration{}, err
	}
	policy, err := model.DecodePolicyConfigJSON(policyJSON, maxAdminBytes)
	if err != nil {
		return Registration{}, err
	}
	effective, err := model.DecodeEffectiveProfileJSON(profileJSON, maxAdminBytes)
	if err != nil {
		return Registration{}, err
	}
	profile, ok := effective.(*model.GoProfile)
	if !ok {
		return Registration{}, ErrUnsupportedProfile
	}
	worktreeID, err := newUUID()
	if err != nil {
		return Registration{}, err
	}
	profileRequest, err := json.Marshal(model.ProfileAdminRequest{WorktreeID: worktreeID, ProfileName: profileName, Profile: profile})
	if err != nil {
		return Registration{}, err
	}
	if _, err = model.DecodeProfileAdminRequestJSON(profileRequest, maxAdminBytes); err != nil {
		return Registration{}, err
	}
	policyDigest, err := model.PolicyDigest(policy, maxAdminBytes)
	if err != nil {
		return Registration{}, err
	}
	profileID, err := model.ProfileDigest(profile, maxAdminBytes)
	if err != nil {
		return Registration{}, err
	}
	policyData, err := json.Marshal(policy)
	if err != nil {
		return Registration{}, err
	}
	profileData, err := json.Marshal(profile)
	if err != nil {
		return Registration{}, err
	}
	worktreePath, worktreeIdentity, err := directoryIdentity(worktree)
	if err != nil {
		return Registration{}, err
	}
	commonPath, commonIdentity, err := directoryIdentity(common)
	if err != nil {
		return Registration{}, err
	}
	allowed, err := policyContainsRoot(policy.AllowedRoots, worktree, worktreePath)
	if err != nil {
		return Registration{}, err
	}
	if !allowed {
		return Registration{}, ErrRootNotAllowed
	}
	if err = verifyCommonGitDirectory(common); err != nil {
		return Registration{}, errors.Join(ErrInvalidGitMetadata, err)
	}
	var verifyBinding func() error
	var closeBinding func() error
	if linkedRelative == "" {
		local, err := fsruntime.OpenLocalGitMetadata(worktree)
		if err != nil {
			return Registration{}, errors.Join(ErrInvalidGitMetadata, err)
		}
		closeBinding = local.Close
		verifyBinding = func() error {
			same, err := sameDirectory(local, common)
			if err != nil || !same {
				return errors.Join(ErrInvalidGitMetadata, err)
			}
			return nil
		}
	} else {
		binding, err := fsruntime.BindLinkedGitMetadata(worktree, common, linkedRelative)
		if err != nil {
			return Registration{}, errors.Join(ErrInvalidGitMetadata, err)
		}
		closeBinding = binding.Close
		verifyBinding = binding.Verify
	}
	if err = verifyBinding(); err != nil {
		return Registration{}, errors.Join(err, closeBinding())
	}
	// Close is explicitly checked before opening the write transaction; root
	// handles remain pinned and the binding is reopened for the final check.
	if err = closeBinding(); err != nil {
		return Registration{}, err
	}
	repoCandidate, err := newUUID()
	if err != nil {
		return Registration{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result := Registration{WorktreeID: worktreeID, BindingRevision: 1, PolicyDigest: policyDigest, PolicyEpoch: 1, ProfileID: profileID, ProfileRevision: 1}
	err = s.write(ctx, func(tx *sql.Tx) error {
		var repoID, existingPath string
		err := tx.QueryRowContext(ctx, `SELECT repo_id,common_dir FROM repositories WHERE filesystem_identity=?`, commonIdentity).Scan(&repoID, &existingPath)
		if errors.Is(err, sql.ErrNoRows) {
			var existingID string
			err = tx.QueryRowContext(ctx, `SELECT repo_id FROM repositories WHERE common_dir=?`, commonPath).Scan(&existingID)
			if err == nil {
				return ErrRootRebindRequired
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			repoID = repoCandidate
			if _, err = tx.ExecContext(ctx, `INSERT INTO repositories(repo_id,common_dir,filesystem_identity,created_at) VALUES(?,?,?,?)`, repoID, commonPath, commonIdentity, now); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if existingPath != commonPath {
			return ErrRootRebindRequired
		}
		var existingRoot, existingIdentity string
		err = tx.QueryRowContext(ctx, `SELECT canonical_root,filesystem_identity FROM worktrees WHERE canonical_root=? OR filesystem_identity=? LIMIT 1`, worktreePath, worktreeIdentity).Scan(&existingRoot, &existingIdentity)
		if err == nil {
			if existingRoot != worktreePath || existingIdentity != worktreeIdentity {
				return ErrRootRebindRequired
			}
			return ErrRegistrationExists
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO worktrees(worktree_id,repo_id,canonical_root,filesystem_identity,binding_revision,status,created_at,updated_at) VALUES(?,?,?,?,1,'registered',?,?)`, worktreeID, repoID, worktreePath, worktreeIdentity, now, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO policies(worktree_id,policy_digest,config_json,created_at) VALUES(?,?,?,?)`, worktreeID, policyDigest, string(policyData), now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO worktree_policy_heads(worktree_id,policy_digest,policy_epoch,updated_at) VALUES(?,?,1,?)`, worktreeID, policyDigest, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO profiles(worktree_id,profile_id,config_json,toolchain_digest,provider_digest,created_at) VALUES(?,?,?,?,?,?)`, worktreeID, profileID, string(profileData), profile.ToolchainDigest, profile.ProviderDigest, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO profile_heads(worktree_id,profile_name,profile_id,revision) VALUES(?,?,?,1)`, worktreeID, profileName, profileID); err != nil {
			return err
		}
		if err = verifyRegistrationBinding(worktree, common, linkedRelative, worktreePath, worktreeIdentity, commonPath, commonIdentity); err != nil {
			return err
		}
		result.RepoID = repoID
		return nil
	})
	if err != nil {
		return Registration{}, err
	}
	return result, nil
}

func verifyRegistrationBinding(worktree, common *paths.Root, linked, worktreePath, worktreeIdentity, commonPath, commonIdentity string) error {
	path, identity, err := directoryIdentity(worktree)
	if err != nil || path != worktreePath || identity != worktreeIdentity {
		return errors.Join(ErrRootRebindRequired, err)
	}
	path, identity, err = directoryIdentity(common)
	if err != nil || path != commonPath || identity != commonIdentity {
		return errors.Join(ErrRootRebindRequired, err)
	}
	if linked != "" {
		binding, err := fsruntime.BindLinkedGitMetadata(worktree, common, linked)
		if err != nil {
			return errors.Join(ErrInvalidGitMetadata, err)
		}
		return binding.Close()
	}
	local, err := fsruntime.OpenLocalGitMetadata(worktree)
	if err != nil {
		return errors.Join(ErrInvalidGitMetadata, err)
	}
	same, verifyErr := sameDirectory(local, common)
	closeErr := local.Close()
	if verifyErr != nil || closeErr != nil || !same {
		return errors.Join(ErrInvalidGitMetadata, verifyErr, closeErr)
	}
	return nil
}

func policyContainsRoot(allowed []string, worktree *paths.Root, worktreePath string) (bool, error) {
	for _, path := range allowed {
		root, err := paths.OpenRoot(path)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(path, worktreePath)
		if err != nil || (relative != "." && model.ValidateRelativePath(filepath.ToSlash(relative), false) != nil) {
			if closeErr := root.Close(); closeErr != nil {
				return false, closeErr
			}
			continue
		}
		candidate := root
		if relative != "." {
			candidate, err = root.OpenDirectory(filepath.ToSlash(relative))
			if err != nil {
				if closeErr := root.Close(); closeErr != nil {
					return false, closeErr
				}
				continue
			}
		}
		same, compareErr := sameDirectory(candidate, worktree)
		var closeErr error
		if candidate != root {
			closeErr = candidate.Close()
		}
		closeErr = errors.Join(closeErr, root.Close())
		if compareErr != nil || closeErr != nil {
			return false, errors.Join(compareErr, closeErr)
		}
		if same {
			return true, nil
		}
	}
	return false, nil
}

func verifyCommonGitDirectory(common *paths.Root) (resultErr error) {
	objects, err := common.OpenDirectory("objects")
	if err != nil {
		return err
	}
	if err = objects.Close(); err != nil {
		return err
	}
	head, err := common.OpenRegular("HEAD")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, head.Close()) }()
	data, err := io.ReadAll(io.LimitReader(head, 1025))
	if err != nil || len(data) == 0 || len(data) > 1024 {
		return errors.Join(ErrInvalidGitMetadata, err)
	}
	if err = paths.VerifyRegularFile(head); err != nil {
		return err
	}
	if err = common.Verify(); err != nil {
		return err
	}
	line := strings.TrimSuffix(string(data), "\n")
	line = strings.TrimSuffix(line, "\r")
	if strings.HasPrefix(line, "ref: refs/") {
		if model.ValidateRelativePath(strings.TrimPrefix(line, "ref: "), false) == nil {
			return nil
		}
		return ErrInvalidGitMetadata
	}
	if len(line) != 40 && len(line) != 64 {
		return ErrInvalidGitMetadata
	}
	for _, c := range line {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return ErrInvalidGitMetadata
		}
	}
	return nil
}
