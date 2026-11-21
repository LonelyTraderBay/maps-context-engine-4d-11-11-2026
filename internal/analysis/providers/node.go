package providers

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrAuthority   = errors.New("provider input or installation is not authorized")
	ErrProcess     = errors.New("provider process did not finish cleanly")
	ErrBudget      = errors.New("provider output budget exceeded")
	ErrUnsupported = errors.New("provider process unavailable on this platform")
)

type NodeConfig struct {
	NodePath, Entrypoint, ScratchParent        string
	NodeDigest, ProviderDigest, CompilerDigest string
	ProviderID                                 string
	Compiler                                   CompilerInstallation
}

// CompilerInstallation is the approved local TypeScript distribution and its
// independently pinned, sorted file-hash manifest from A047.
type CompilerInstallation struct {
	Root, ArchivePath, ManifestPath, ManifestDigest string
}

// NodeProvider launches an approved S43 worker from pinned files.
// Configuration is installation-owned. Callers cannot add flags or environment.
type NodeProvider struct{ config NodeConfig }

func NewNodeProvider(config NodeConfig) *NodeProvider { return &NodeProvider{config: config} }

var _ ports.LanguageProvider = (*NodeProvider)(nil)

type snapshotInput struct{ *snapshot.Snapshot }

// SnapshotInput gives the provider the canonical manifest and verified ranges
// while keeping the ports contract independent of snapshot implementation.
func SnapshotInput(captured *snapshot.Snapshot) ports.ProviderCapture {
	return snapshotInput{captured}
}

func (s snapshotInput) CheckCapturedRange(path string, span model.ByteRange) (string, error) {
	file, ok := s.File(path)
	if !ok || span.StartByte < 0 || span.EndByte < span.StartByte || span.EndByte > int64(file.Len()) {
		return "", ErrAuthority
	}
	if err := file.CheckRange(int(span.StartByte), int(span.EndByte)); err != nil {
		return "", ErrAuthority
	}
	return file.ContentHash(), nil
}

type factScope struct {
	capture     ports.ProviderCapture
	packages    map[string]bool
	symbols     map[string]bool
	relations   map[string]bool
	diagnostics map[string]bool
}

func (s *factScope) batch(ctx context.Context, batch *model.ProviderBatch) error {
	for _, item := range batch.Packages {
		if s.packages[item.PackageKey] {
			return ErrProtocol
		}
		s.packages[item.PackageKey] = true
	}
	for _, item := range batch.Symbols {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.symbols[item.SymbolID] || item.PackageKey != nil && !s.packages[*item.PackageKey] {
			return ErrProtocol
		}
		if item.Path != nil {
			if item.Range == nil {
				return ErrProtocol
			}
			if _, err := s.capture.CheckCapturedRange(*item.Path, *item.Range); err != nil {
				return ErrAuthority
			}
		}
		s.symbols[item.SymbolID] = true
	}
	for _, item := range batch.Relations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.relations[item.RelationID] || !s.symbols[item.SourceSymbolID] || item.TargetSymbolID != nil && !s.symbols[*item.TargetSymbolID] {
			return ErrProtocol
		}
		if _, err := s.capture.CheckCapturedRange(item.SitePath, item.Range); err != nil {
			return ErrAuthority
		}
		s.relations[item.RelationID] = true
	}
	for _, item := range batch.Diagnostics {
		if s.diagnostics[item.DiagnosticID] || item.PackageKey != nil && !s.packages[*item.PackageKey] {
			return ErrProtocol
		}
		if item.Path != nil {
			if _, err := s.capture.CheckCapturedRange(*item.Path, model.ByteRange{}); err != nil {
				return ErrAuthority
			}
		}
		s.diagnostics[item.DiagnosticID] = true
	}
	return ctx.Err()
}

func checkDone(job ports.ProviderJob, worktree string, done *model.ProviderDone) error {
	for _, capability := range done.Capabilities {
		if capability.ProviderID != job.Start.ProviderID {
			return ErrAuthority
		}
	}
	gaps := done.Gaps
	if done.Error != nil {
		gaps = append(append([]model.Gap{}, gaps...), done.Error.Details...)
	}
	for _, gap := range gaps {
		for _, ref := range gap.SourceRefs {
			hash, err := job.Capture.CheckCapturedRange(ref.Path, ref.Range)
			if err != nil || ref.WorktreeID != worktree || ref.ProfileID != job.Start.ProfileID ||
				ref.GenerationID != job.Start.GenerationID || ref.ContentHash != hash {
				return ErrAuthority
			}
		}
	}
	return nil
}
