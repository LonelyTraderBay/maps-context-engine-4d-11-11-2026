package inventory

import "errors"

var ErrGitConfigUnsupported = errors.New("Git configuration cannot be verified safely")

// GitConfigFields contains only settings needed for safe inventory. Paths are
// still untrusted data and must be resolved within separately approved roots.
type GitConfigFields struct {
	IgnoreCase     *bool
	ExcludesFile   *string
	ObjectFormat   *string
	WorktreeConfig *bool
	Worktree       *string
	Bare           *bool
}
