package inventory

import (
	"context"
	"errors"
)

var (
	ErrWalkBudget           = errors.New("worktree enumeration exceeded its budget")
	ErrWalkChanged          = errors.New("worktree changed while enumerating")
	ErrWalkAuthorityChanged = errors.New("worktree enumeration authority changed")
)

// Candidate is only a filesystem observation. It is not yet a tracked or
// untracked file, an ignore decision, or a captured source file.
type Candidate struct {
	Path      string
	Kind      string // regular, symlink, nested_root, excluded, hardlink, unsupported, unreadable
	Directory bool
}

type CandidateWalk struct {
	Candidates         []Candidate
	DeniedCount        int
	InvalidPathCount   int
	IgnoreRulesUnknown bool
}

type WalkBudget struct {
	MaxVisited        int
	MaxDirectories    int
	MaxDepth          int
	MaxPathBytes      int
	MaxTotalPathBytes int
}

type WalkAuthorization func(ctx context.Context, path string, directory bool) (bool, error)
type WalkExclusion func(path string, directory bool) bool
