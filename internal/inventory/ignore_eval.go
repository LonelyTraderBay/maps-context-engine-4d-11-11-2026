package inventory

// IgnoreQuery identifies an already-authorized portable worktree path.
type IgnoreQuery struct {
	Path        string
	IsDirectory bool
}

// IgnoreOptions must be derived from an authorized effective Git config.
type IgnoreOptions struct {
	IgnoreCase bool
}
