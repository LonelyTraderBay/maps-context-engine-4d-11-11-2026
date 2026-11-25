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
	Mode                    string // exact, prefix, text, file, position, catalog, question, or question_route
	Query                   string
	Kinds                   []string // exact symbol kinds; empty means all kinds
	Package                 string   // exact Go import path; empty means all packages
	Path                    string   // exact repository-relative file for a symbol query
	Qualified               bool
	Position                *model.PositionSelector
	Limit                   int
	MaxOutputBytes          int
	MaxManifestBytes        int
	MaxFileBytes            int
	AuthorizeGeneration     func(context.Context, string, string, string, int64) error
	AuthorizePath           func(context.Context, string, string, string, int64) error
	Scanner                 *policy.Scanner
	// Keyset orders by immutable item ID rather than the legacy display order.
	// AfterID is trusted only after the caller verifies a signed continuation.
	Keyset  bool
	AfterID string
}

type SymbolHit struct {
	Symbol model.SymbolFact `json:"symbol"`
	Source *model.SourceRef `json:"source"`
}

// Text hits identify lexical FTS or path matches; neither implies a semantic
// relation or source site.
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
	NextKey          string      `json:"-"` // trusted producer signs this key
}

type Reader interface {
	Search(context.Context, Request) (Result, error)
}
