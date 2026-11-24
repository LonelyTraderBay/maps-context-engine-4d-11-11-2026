package model

// LocalWorktree is administrative metadata from an authorized catalog. Listing
// an entry or its profile IDs does not grant access to its source or cache.
type LocalWorktree struct {
	WorktreeID      string   `json:"worktree_id"`
	RepoID          string   `json:"repo_id"`
	Root            string   `json:"root"`
	Status          string   `json:"status"`
	BindingRevision int64    `json:"binding_revision"`
	Profiles        []string `json:"profile_ids"`
}

// LocalCacheUsage reports measured file lengths, not filesystem allocation or
// the conservative charge a future transaction must fit.
type LocalCacheUsage struct {
	WorktreeID string `json:"worktree_id"`
	ProfileID  string `json:"profile_id"`
	LimitBytes int64  `json:"limit_bytes"`
	IndexBytes int64  `json:"index_bytes"`
	WALBytes   int64  `json:"wal_bytes"`
	SHMBytes   int64  `json:"shm_bytes"`
	OtherBytes int64  `json:"other_bytes"`
}
