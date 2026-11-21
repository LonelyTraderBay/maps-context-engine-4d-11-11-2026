package sqlite

import "time"

type ProtectedRestoreSummary struct {
	OperationID    string   `json:"operation_id"`
	Target         string   `json:"target"`
	WorktreeID     string   `json:"worktree_id"`
	Outcome        string   `json:"outcome"`
	CompletedSteps []string `json:"completed_steps"`
}

const RestoreMaxWait = 30 * time.Second
