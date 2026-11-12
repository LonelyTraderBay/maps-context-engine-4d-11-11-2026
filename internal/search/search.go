// Package search describes bounded internal search results. It is not the
// public MCP/CLI contract and does not assert live-workspace freshness.
package search

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

type Request struct {
	ProfileID, GenerationID string
	Mode                    string // exact, prefix, text, or position
	Query                   string
	Qualified               bool
	Position                *model.PositionSelector
	Limit                   int
	MaxOutputBytes          int
	MaxManifestBytes        int
	MaxFileBytes            int
	AuthorizeGeneration     func(context.Context, string, string, string, int64) error
	AuthorizePath           func(context.Context, string, string, string, int64) error
	Scanner                 *policy.Scanner
}

type SymbolHit struct {
	Symbol model.SymbolFact `json:"symbol"`
	Source *model.SourceRef `json:"source"`
}

// Text hits identify files containing an FTS token phrase. They are lexical
// evidence only and deliberately carry no inferred semantic relation or site.
type TextHit struct {
	Path         string `json:"path"`
	ContentHash  string `json:"content_hash"`
	EvidenceKind string `json:"evidence_kind"`
}

type Result struct {
	WorktreeID       string      `json:"worktree_id"`
	IndexIncarnation string      `json:"index_incarnation"`
	ProfileID        string      `json:"profile_id"`
	GenerationID     string      `json:"generation_id"`
	SnapshotDigest   string      `json:"snapshot_digest"`
	State            string      `json:"state"`
	Coverage         string      `json:"coverage"`  // unknown until coverage is composed
	Freshness        string      `json:"freshness"` // not_checked; this reads a snapshot
	Symbols          []SymbolHit `json:"symbols"`
	Text             []TextHit   `json:"text"`
	Truncated        bool        `json:"truncated"`
	Ambiguous        bool        `json:"ambiguous"`
}

type Reader interface {
	Search(context.Context, Request) (Result, error)
}
