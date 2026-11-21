// Package uimap verifies private developer-produced UI metadata. A build map
// contains source identities and anchors, with no engine generation claims.
package uimap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

var ErrBuildArtifact = errors.New("unverified UI build manifest")

type Approval struct {
	WorktreeID, ProfileID, ProviderDigest, BuildConfigDigest, InputManifestDigest string
	ArtifactDigest                                                                string
}

type ImportedBuild struct {
	artifactDigest string
	canonical      []byte
}

func (b ImportedBuild) ArtifactDigest() string { return b.artifactDigest }

// Manifest returns an independent copy suitable for an atomic store write.
func (b ImportedBuild) Manifest() (model.BuildManifest, []byte, error) {
	if !model.ValidDigest(b.artifactDigest) || len(b.canonical) == 0 {
		return model.BuildManifest{}, nil, ErrBuildArtifact
	}
	manifest, err := model.DecodeBuildManifestJSON(b.canonical, len(b.canonical))
	if err != nil {
		return model.BuildManifest{}, nil, ErrBuildArtifact
	}
	return manifest, append([]byte(nil), b.canonical...), nil
}

// ReadApprovedArtifact opens a relative regular file under the approved
// private artifact root. No project code, configuration or network runs.
func ReadApprovedArtifact(ctx context.Context, root *paths.Root, name string, approved Approval, limit int) (ImportedBuild, error) {
	if !model.ValidDigest(approved.ArtifactDigest) {
		return ImportedBuild{}, ErrBuildArtifact
	}
	return readArtifact(ctx, root, name, limit, nil, func(m model.BuildManifest, digest string) bool {
		return digest == approved.ArtifactDigest && m.WorktreeID == approved.WorktreeID && m.ProfileID == approved.ProfileID &&
			m.ProviderDigest == approved.ProviderDigest && m.BuildConfigDigest == approved.BuildConfigDigest &&
			m.InputManifestDigest == approved.InputManifestDigest
	})
}

// ReadSelectedArtifact accepts an operator-selected local artifact as data.
// Its build claims still require verification against a retained capture.
func ReadSelectedArtifact(ctx context.Context, root *paths.Root, name, worktree string, limit int, verifyFile func(*os.File) error) (ImportedBuild, error) {
	if !model.ValidUUID(worktree) || verifyFile == nil {
		return ImportedBuild{}, ErrBuildArtifact
	}
	return readArtifact(ctx, root, name, limit, verifyFile, func(m model.BuildManifest, _ string) bool {
		return m.WorktreeID == worktree
	})
}

func readArtifact(ctx context.Context, root *paths.Root, name string, limit int, verifyFile func(*os.File) error, approve func(model.BuildManifest, string) bool) (ImportedBuild, error) {
	if ctx == nil || root == nil || limit < 1024 || limit > 16<<20 {
		return ImportedBuild{}, ErrBuildArtifact
	}
	if err := ctx.Err(); err != nil {
		return ImportedBuild{}, err
	}
	file, err := root.OpenRegular(name)
	if err != nil {
		return ImportedBuild{}, ErrBuildArtifact
	}
	before, statErr := file.Stat()
	if statErr != nil || before.Size() < 1 || before.Size() > int64(limit) || verifyFile != nil && verifyFile(file) != nil {
		_ = file.Close()
		return ImportedBuild{}, ErrBuildArtifact
	}
	body, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	after, statErr := file.Stat()
	if statErr == nil && verifyFile != nil {
		statErr = verifyFile(file)
	}
	closeErr := file.Close()
	if readErr != nil || statErr != nil || closeErr != nil || int64(len(body)) != before.Size() ||
		before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || root.Verify() != nil {
		return ImportedBuild{}, ErrBuildArtifact
	}
	hash := sha256.Sum256(body)
	digest := hex.EncodeToString(hash[:])
	manifest, err := model.DecodeBuildManifestJSON(body, limit)
	if err != nil || !approve(manifest, digest) || model.VerifyBuildMapDigest(manifest, limit) != nil {
		return ImportedBuild{}, ErrBuildArtifact
	}
	manifest, err = model.NormalizeBuildManifest(manifest)
	if err != nil {
		return ImportedBuild{}, ErrBuildArtifact
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || len(canonical) > limit {
		return ImportedBuild{}, ErrBuildArtifact
	}
	if err := ctx.Err(); err != nil {
		return ImportedBuild{}, err
	}
	return ImportedBuild{artifactDigest: digest, canonical: canonical}, nil
}
