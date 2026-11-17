package sqlite

import "errors"

var (
	ErrProfileInput      = errors.New("invalid protected profile input")
	ErrProfileExists     = errors.New("protected profile already exists")
	ErrProfileIncomplete = errors.New("protected profile requires recovery")
	ErrProfileDenied     = errors.New("protected profile authority denied")
)

// ProtectedProfileOptions is offline administrator input, not a runtime wire
// request or permission grant inferred from source files.
type ProtectedProfileOptions struct {
	Root             string
	EngineUID        uint32
	EngineGID        uint32
	ClientUID        uint32
	TargetRoots      []string
	WorkerExecutable string
}

type ProtectedProfileSummary struct {
	ProfileID          string `json:"profile_id"`
	Stage              string `json:"stage"`
	CatalogIncarnation string `json:"catalog_incarnation,omitempty"`
	CatalogVerified    bool   `json:"catalog_verified"`
	TrustedHead        bool   `json:"trusted_head"`
}

type ProtectedWorktreeSummary struct {
	WorktreeID         string `json:"worktree_id"`
	IndexIncarnation   string `json:"index_incarnation"`
	RecordsIncarnation string `json:"records_incarnation"`
	Verified           bool   `json:"verified"`
}
