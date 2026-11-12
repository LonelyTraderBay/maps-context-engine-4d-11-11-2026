package model

type CoreRuntimePatch struct {
	MaxFileBytes              *int64 `json:"max_file_bytes,omitempty"`
	MaxIndexedFiles           *int64 `json:"max_indexed_files,omitempty"`
	MaxSourceBytesPerSnapshot *int64 `json:"max_source_bytes_per_snapshot,omitempty"`
	MaxParallelIndexJobs      *int64 `json:"max_parallel_index_jobs,omitempty"`
	MaxParseWorkers           *int64 `json:"max_parse_workers,omitempty"`
	MaxLspSessions            *int64 `json:"max_lsp_sessions,omitempty"`
	SqliteBusyTimeoutMs       *int64 `json:"sqlite_busy_timeout_ms,omitempty"`
	WriterLockWaitMs          *int64 `json:"writer_lock_wait_ms,omitempty"`
	QueryTimeoutMs            *int64 `json:"query_timeout_ms,omitempty"`
	LspStartupTimeoutMs       *int64 `json:"lsp_startup_timeout_ms,omitempty"`
	RenameTimeoutMs           *int64 `json:"rename_timeout_ms,omitempty"`
	IndexTimeoutMs            *int64 `json:"index_timeout_ms,omitempty"`
	WatchPollIntervalMs       *int64 `json:"watch_poll_interval_ms,omitempty"`
	FullReconcileIntervalMs   *int64 `json:"full_reconcile_interval_ms,omitempty"`
	MaxOutputBytes            *int64 `json:"max_output_bytes,omitempty"`
	MaxGraphNodes             *int64 `json:"max_graph_nodes,omitempty"`
	MaxGraphEdges             *int64 `json:"max_graph_edges,omitempty"`
	MaxGraphDepth             *int64 `json:"max_graph_depth,omitempty"`
	KeepGenerations           *int64 `json:"keep_generations,omitempty"`
	PackTtlSeconds            *int64 `json:"pack_ttl_seconds,omitempty"`
	MaxWorktreeCacheBytes     *int64 `json:"max_worktree_cache_bytes,omitempty"`
	LogFiles                  *int64 `json:"log_files,omitempty"`
	LogFileBytes              *int64 `json:"log_file_bytes,omitempty"`
	MaintenanceWaitMs         *int64 `json:"maintenance_wait_ms,omitempty"`
	RenameResultMaxBytes      *int64 `json:"rename_result_max_bytes,omitempty"`
	RenameResultsMaxBytes     *int64 `json:"rename_results_max_bytes,omitempty"`
	RenameResultTtlSeconds    *int64 `json:"rename_result_ttl_seconds,omitempty"`
	RenameMaxEdits            *int64 `json:"rename_max_edits,omitempty"`
	ResultTtlSeconds          *int64 `json:"result_ttl_seconds,omitempty"`
	QueryResultCacheBytes     *int64 `json:"query_result_cache_bytes,omitempty"`
	MaxRetainedQueryResults   *int64 `json:"max_retained_query_results,omitempty"`
}
type ExplorerRuntimePatch struct {
	GraphDefaultNodes   *int64 `json:"graph_default_nodes,omitempty"`
	GraphDefaultEdges   *int64 `json:"graph_default_edges,omitempty"`
	GraphHardNodes      *int64 `json:"graph_hard_nodes,omitempty"`
	GraphHardEdges      *int64 `json:"graph_hard_edges,omitempty"`
	FlowMaxSteps        *int64 `json:"flow_max_steps,omitempty"`
	FlowMaxEdges        *int64 `json:"flow_max_edges,omitempty"`
	HoverHz             *int64 `json:"hover_hz,omitempty"`
	SessionTtlSeconds   *int64 `json:"session_ttl_seconds,omitempty"`
	BearerTtlSeconds    *int64 `json:"bearer_ttl_seconds,omitempty"`
	AckTimeoutMs        *int64 `json:"ack_timeout_ms,omitempty"`
	DetachTimeoutMs     *int64 `json:"detach_timeout_ms,omitempty"`
	PollVisibleMs       *int64 `json:"poll_visible_ms,omitempty"`
	PollHiddenMs        *int64 `json:"poll_hidden_ms,omitempty"`
	PollMissLimit       *int64 `json:"poll_miss_limit,omitempty"`
	BridgeMessageBytes  *int64 `json:"bridge_message_bytes,omitempty"`
	MaxPreviewSessions  *int64 `json:"max_preview_sessions,omitempty"`
	MapWatchPollMs      *int64 `json:"map_watch_poll_ms,omitempty"`
	MapArtifactMaxBytes *int64 `json:"map_artifact_max_bytes,omitempty"`
	MapPendingMax       *int64 `json:"map_pending_max,omitempty"`
	MapWatchDebounceMs  *int64 `json:"map_watch_debounce_ms,omitempty"`
}
type RuntimeConfigPatch struct {
	SchemaVersion int64                 `json:"schema_version"`
	Core          *CoreRuntimePatch     `json:"core,omitempty"`
	Explorer      *ExplorerRuntimePatch `json:"explorer,omitempty"`
}
