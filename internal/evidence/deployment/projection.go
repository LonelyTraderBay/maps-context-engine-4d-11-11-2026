// Package deployment maps declared deployment metadata and explicitly approved
// local observations. It does not query or execute any deployment system.
package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const maxManifestBytes = 8 << 20

var (
	ErrEvidence  = errors.New("deployment metadata is invalid")
	ErrAuthority = errors.New("deployment evidence approval unavailable")
	scanner, _   = policy.NewScanner(MaxEvidenceBytes)
)

type Subject struct {
	Kind, Path, ContentHash string
}

// Authorize is a host check for the principal and current scope, policy and
// path of the exact artifact. It is called before decoding and before return.
type Authorize func(context.Context, model.ScopeVector, Subject) error

type LocalObservation struct {
	Path, ApprovedHash string
	Bytes              []byte
}

type Observation struct {
	Kind           string            `json:"kind"`
	SchemaVersion  int               `json:"schema_version"`
	WorktreeID     string            `json:"worktree_id"`
	ProfileID      string            `json:"profile_id"`
	SnapshotDigest string            `json:"snapshot_digest"`
	Environment    string            `json:"environment"`
	ObservedAt     string            `json:"observed_at"`
	Services       []ObservedService `json:"services"`
}

type ObservedService struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type Observed struct {
	ArtifactPath string      `json:"artifact_path"`
	ArtifactHash string      `json:"artifact_hash"`
	Alignment    string      `json:"alignment"`
	Evidence     Observation `json:"evidence"`
}

type Projection struct {
	ScopeDigest     string          `json:"scope_digest"`
	Source          model.SourceRef `json:"source"`
	Declared        Manifest        `json:"declared"`
	LiveEnvironment string          `json:"live_environment"`
	Observed        *Observed       `json:"observed"`
}

// Build consumes a whole, pinned metadata manifest from the supplied capture.
// An optional observation is separate evidence, even at a matching capture.
// The owner may choose to retry without an observation rejected by policy.
func Build(ctx context.Context, scope model.ScopeVector, captured *snapshot.Snapshot, ref model.SourceRef,
	observation *LocalObservation, authorize Authorize) (Projection, error) {
	subject := Subject{Kind: "declared", Path: ref.Path, ContentHash: ref.ContentHash}
	if ctx == nil || authorize == nil || scope.Kind != "single" || len(scope.Members) != 1 ||
		!allowedPath(ref.Path, ".deployment.json") || !allowed(ctx, scope, subject, authorize) {
		return Projection{}, ErrAuthority
	}
	file, ok := captured.File(ref.Path)
	if !ok || ref.Range.StartByte != 0 || ref.Range.EndByte != int64(file.Len()) || file.Len() > MaxEvidenceBytes ||
		snapshot.ValidateCapturedSources(ctx, scope, []snapshot.ExpectedSource{{Ref: ref}}, captured, maxManifestBytes) != nil {
		return Projection{}, ErrEvidence
	}
	declared, err := manifest(file.Bytes())
	if err != nil {
		return Projection{}, err
	}
	report := Projection{ScopeDigest: scope.VectorDigest, Source: ref, Declared: declared, LiveEnvironment: "unknown"}
	if observation != nil {
		report.Observed, err = readObservation(ctx, scope, *observation, authorize)
		if err != nil {
			return Projection{}, err
		}
	}
	if !allowed(ctx, scope, subject, authorize) {
		return Projection{}, ErrAuthority
	}
	if report.Observed != nil && !allowed(ctx, scope, Subject{Kind: "observed", Path: report.Observed.ArtifactPath,
		ContentHash: report.Observed.ArtifactHash}, authorize) {
		return Projection{}, ErrAuthority
	}
	return report, nil
}

func readObservation(ctx context.Context, scope model.ScopeVector, material LocalObservation, authorize Authorize) (*Observed, error) {
	subject := Subject{Kind: "observed", Path: material.Path, ContentHash: material.ApprovedHash}
	if !allowedPath(subject.Path, ".deployment-observation.json") || !model.ValidDigest(subject.ContentHash) ||
		!allowed(ctx, scope, subject, authorize) {
		return nil, ErrAuthority
	}
	if len(material.Bytes) == 0 || len(material.Bytes) > MaxEvidenceBytes {
		return nil, ErrEvidence
	}
	actual := sha256.Sum256(material.Bytes)
	if hex.EncodeToString(actual[:]) != material.ApprovedHash {
		return nil, ErrEvidence
	}
	var evidence Observation
	member := scope.Members[0]
	if err := decode(material.Bytes, &evidence); err != nil || evidence.Kind != "deployment-observation" || evidence.SchemaVersion != 1 ||
		evidence.WorktreeID != member.WorktreeID || evidence.ProfileID != member.ProfileID || !model.ValidDigest(evidence.SnapshotDigest) ||
		!label.MatchString(evidence.Environment) || evidence.Services == nil || len(evidence.Services) > MaxServices {
		return nil, ErrEvidence
	}
	if _, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt); err != nil || !strings.HasSuffix(evidence.ObservedAt, "Z") {
		return nil, ErrEvidence
	}
	slices.SortFunc(evidence.Services, func(a, b ObservedService) int { return strings.Compare(a.ID, b.ID) })
	for i, service := range evidence.Services {
		if !label.MatchString(service.ID) || !label.MatchString(service.Version) || i > 0 && evidence.Services[i-1].ID == service.ID {
			return nil, ErrEvidence
		}
	}
	alignment := "historical"
	if evidence.SnapshotDigest == member.SnapshotDigest {
		alignment = "matching_capture"
	}
	return &Observed{ArtifactPath: material.Path, ArtifactHash: material.ApprovedHash, Alignment: alignment, Evidence: evidence}, nil
}

func allowedPath(path, suffix string) bool {
	return strings.HasSuffix(path, suffix) && policy.CheckPath(path) == nil && scanner.CheckContent([]byte(path)) == nil
}

func allowed(ctx context.Context, scope model.ScopeVector, subject Subject, authorize Authorize) bool {
	return ctx.Err() == nil && authorize(ctx, scope, subject) == nil && ctx.Err() == nil
}
