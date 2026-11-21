// Package report serializes bounded, read-only reports from the existing
// change, deployment and architecture services.
package report

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/domain/architecture"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/evidence/deployment"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/history"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const (
	MaxRows      = 1024
	MaxBytes     = 1 << 20
	scopeMaxSize = 256 << 10
)

var (
	ErrLimit   = errors.New("machine report exceeds its budget")
	ErrScope   = errors.New("machine report scope is unavailable")
	ErrContent = errors.New("machine report content is denied")
	scanner, _ = policy.NewScanner(MaxBytes)
)

type Budget struct {
	Rows  int
	Bytes int
}

// The owner must check current authority to the exact retained vector,
// including the principal, policy and record epochs.
type Service struct {
	Diff         history.DiffResolver
	Architecture architecture.Reader
	Authorize    func(context.Context, model.ScopeVector) error
}

type DeploymentInput struct {
	Scope       model.ScopeVector
	Capture     *snapshot.Snapshot
	Source      model.SourceRef
	Observation *deployment.LocalObservation
	Authorize   deployment.Authorize
}

type envelope struct {
	Version     int    `json:"schema_version"`
	Kind        string `json:"kind"`
	ScopeDigest string `json:"scope_digest"`
	Status      string `json:"status"`
	Data        any    `json:"data"`
}

// ChangesJSON keeps ambiguous and partial outcomes from the retained diff.
func (s Service) ChangesJSON(ctx context.Context, req history.DiffRequest, budget Budget) ([]byte, error) {
	scopes := []model.ScopeVector{req.Left, req.Right}
	if err := s.preflight(ctx, budget, scopes); err != nil {
		return nil, err
	}
	req.MaxChanges = min(req.MaxChanges, budget.Rows)
	diff, err := s.Diff.Compare(ctx, req)
	if err != nil {
		return nil, err
	}
	if model.ComparePinnedScope(req.Left, diff.Left, scopeMaxSize) != nil || model.ComparePinnedScope(req.Right, diff.Right, scopeMaxSize) != nil {
		return nil, ErrScope
	}
	count := len(diff.Changes) + len(diff.ProviderDeltas) + len(diff.Decisions) + len(diff.Consumers) + len(diff.Gaps)
	if count > budget.Rows {
		return nil, ErrLimit
	}
	status := "complete"
	if diff.Partial {
		status = "partial"
	}
	data := changeData{LeftScopeDigest: diff.Left.VectorDigest, ExactMatches: diff.ExactMatches,
		Changes: diff.Changes, ProviderDeltas: diff.ProviderDeltas, Decisions: diff.Decisions, Consumers: diff.Consumers, Gaps: diff.Gaps}
	return s.finish(ctx, scopes, budget, "change", status, data)
}

// DeploymentJSON preserves the separate artifact provenance and live unknown.
func (s Service) DeploymentJSON(ctx context.Context, in DeploymentInput, budget Budget) ([]byte, error) {
	scopes := []model.ScopeVector{in.Scope}
	if err := s.preflight(ctx, budget, scopes); err != nil {
		return nil, err
	}
	data, err := deployment.Build(ctx, in.Scope, in.Capture, in.Source, in.Observation, in.Authorize)
	if err != nil {
		return nil, err
	}
	count := len(data.Declared.Services)
	for _, service := range data.Declared.Services {
		count += len(service.Ports) + len(service.Config) + len(service.Permissions)
	}
	if data.Observed != nil {
		count += len(data.Observed.Evidence.Services)
	}
	if count > budget.Rows {
		return nil, ErrLimit
	}
	return s.finish(ctx, scopes, budget, "deployment", "declared", data)
}

// ArchitectureJSON uses the currently approved architecture declaration and
// retains the reader's observed witnesses, gaps and status.
func (s Service) ArchitectureJSON(ctx context.Context, selection architecture.Selection, budget Budget) ([]byte, error) {
	scopes := []model.ScopeVector{selection.Scope}
	if err := s.preflight(ctx, budget, scopes); err != nil {
		return nil, err
	}
	checked, err := s.Architecture.Check(ctx, selection)
	if err != nil {
		return nil, err
	}
	if model.ComparePinnedScope(selection.Scope, checked.Scope, scopeMaxSize) != nil {
		return nil, ErrScope
	}
	count := len(checked.Components) + len(checked.Findings) + len(checked.Gaps)
	for _, finding := range checked.Findings {
		count += len(finding.Witness)
	}
	if count > budget.Rows {
		return nil, ErrLimit
	}
	data := architectureData{Source: checked.Source, RecordID: checked.Approval.RecordID, RecordVersion: checked.Approval.Version,
		CheckedAt: checked.CheckedAt, Components: checked.Components, Findings: checked.Findings, Gaps: checked.Gaps}
	return s.finish(ctx, scopes, budget, "architecture", checked.Status, data)
}

type changeData struct {
	LeftScopeDigest string                   `json:"left_scope_digest"`
	ExactMatches    int                      `json:"exact_matches"`
	Changes         []history.SemanticChange `json:"changes"`
	ProviderDeltas  []history.ProviderDelta  `json:"provider_deltas"`
	Decisions       []history.DecisionDrift  `json:"decisions"`
	Consumers       []history.KnownConsumer  `json:"consumers"`
	Gaps            []model.Gap              `json:"gaps"`
}

type architectureData struct {
	Source        model.SourceRef                `json:"source"`
	RecordID      string                         `json:"record_id"`
	RecordVersion int64                          `json:"record_version"`
	CheckedAt     string                         `json:"checked_at"`
	Components    []architecture.ComponentStatus `json:"components"`
	Findings      []architecture.Finding         `json:"findings"`
	Gaps          []model.Gap                    `json:"gaps"`
}

func (s Service) preflight(ctx context.Context, budget Budget, scopes []model.ScopeVector) error {
	if budget.Rows < 1 || budget.Rows > MaxRows || budget.Bytes < 1 || budget.Bytes > MaxBytes {
		return ErrLimit
	}
	for _, scope := range scopes {
		if model.VerifyScopeVectorDigest(scope, scopeMaxSize) != nil {
			return ErrScope
		}
	}
	return s.check(ctx, scopes)
}

func (s Service) check(ctx context.Context, scopes []model.ScopeVector) error {
	if ctx == nil || s.Authorize == nil {
		return ErrScope
	}
	for _, scope := range scopes {
		if ctx.Err() != nil || s.Authorize(ctx, scope) != nil || ctx.Err() != nil {
			return ErrScope
		}
	}
	return nil
}

func (s Service) finish(ctx context.Context, scopes []model.ScopeVector, budget Budget, kind, status string, data any) ([]byte, error) {
	body, err := json.Marshal(envelope{Version: 1, Kind: kind, ScopeDigest: scopes[len(scopes)-1].VectorDigest, Status: status, Data: data})
	if err != nil {
		return nil, ErrContent
	}
	if len(body) > budget.Bytes {
		return nil, ErrLimit
	}
	if scanner.CheckContent(body) != nil {
		return nil, ErrContent
	}
	if err := s.check(ctx, scopes); err != nil {
		return nil, err
	}
	return body, nil
}
