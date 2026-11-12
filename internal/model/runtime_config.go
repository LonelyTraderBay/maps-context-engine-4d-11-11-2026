package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

type CoreRuntimeLimits struct {
	MaxFileBytes              int64 `json:"max_file_bytes"`
	MaxIndexedFiles           int64 `json:"max_indexed_files"`
	MaxSourceBytesPerSnapshot int64 `json:"max_source_bytes_per_snapshot"`
	MaxParallelIndexJobs      int64 `json:"max_parallel_index_jobs"`
	MaxParseWorkers           int64 `json:"max_parse_workers"`
	MaxLspSessions            int64 `json:"max_lsp_sessions"`
	SqliteBusyTimeoutMs       int64 `json:"sqlite_busy_timeout_ms"`
	WriterLockWaitMs          int64 `json:"writer_lock_wait_ms"`
	QueryTimeoutMs            int64 `json:"query_timeout_ms"`
	LspStartupTimeoutMs       int64 `json:"lsp_startup_timeout_ms"`
	RenameTimeoutMs           int64 `json:"rename_timeout_ms"`
	IndexTimeoutMs            int64 `json:"index_timeout_ms"`
	WatchPollIntervalMs       int64 `json:"watch_poll_interval_ms"`
	FullReconcileIntervalMs   int64 `json:"full_reconcile_interval_ms"`
	MaxOutputBytes            int64 `json:"max_output_bytes"`
	MaxGraphNodes             int64 `json:"max_graph_nodes"`
	MaxGraphEdges             int64 `json:"max_graph_edges"`
	MaxGraphDepth             int64 `json:"max_graph_depth"`
	KeepGenerations           int64 `json:"keep_generations"`
	PackTtlSeconds            int64 `json:"pack_ttl_seconds"`
	MaxWorktreeCacheBytes     int64 `json:"max_worktree_cache_bytes"`
	LogFiles                  int64 `json:"log_files"`
	LogFileBytes              int64 `json:"log_file_bytes"`
	MaintenanceWaitMs         int64 `json:"maintenance_wait_ms"`
	RenameResultMaxBytes      int64 `json:"rename_result_max_bytes"`
	RenameResultsMaxBytes     int64 `json:"rename_results_max_bytes"`
	RenameResultTtlSeconds    int64 `json:"rename_result_ttl_seconds"`
	RenameMaxEdits            int64 `json:"rename_max_edits"`
	ResultTtlSeconds          int64 `json:"result_ttl_seconds"`
	QueryResultCacheBytes     int64 `json:"query_result_cache_bytes"`
	MaxRetainedQueryResults   int64 `json:"max_retained_query_results"`
}
type ExplorerRuntimeLimits struct {
	GraphDefaultNodes   int64 `json:"graph_default_nodes"`
	GraphDefaultEdges   int64 `json:"graph_default_edges"`
	GraphHardNodes      int64 `json:"graph_hard_nodes"`
	GraphHardEdges      int64 `json:"graph_hard_edges"`
	FlowMaxSteps        int64 `json:"flow_max_steps"`
	FlowMaxEdges        int64 `json:"flow_max_edges"`
	HoverHz             int64 `json:"hover_hz"`
	SessionTtlSeconds   int64 `json:"session_ttl_seconds"`
	BearerTtlSeconds    int64 `json:"bearer_ttl_seconds"`
	AckTimeoutMs        int64 `json:"ack_timeout_ms"`
	DetachTimeoutMs     int64 `json:"detach_timeout_ms"`
	PollVisibleMs       int64 `json:"poll_visible_ms"`
	PollHiddenMs        int64 `json:"poll_hidden_ms"`
	PollMissLimit       int64 `json:"poll_miss_limit"`
	BridgeMessageBytes  int64 `json:"bridge_message_bytes"`
	MaxPreviewSessions  int64 `json:"max_preview_sessions"`
	MapWatchPollMs      int64 `json:"map_watch_poll_ms"`
	MapArtifactMaxBytes int64 `json:"map_artifact_max_bytes"`
	MapPendingMax       int64 `json:"map_pending_max"`
	MapWatchDebounceMs  int64 `json:"map_watch_debounce_ms"`
}
type RuntimeConfig struct {
	SchemaVersion int64                 `json:"schema_version"`
	Core          CoreRuntimeLimits     `json:"core"`
	Explorer      ExplorerRuntimeLimits `json:"explorer"`
}

// DefaultRuntimeConfig returns the spec defaults before resolving the CPU sentinel.
func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{SchemaVersion: 1,
		Core: CoreRuntimeLimits{
			MaxFileBytes:              2097152,
			MaxIndexedFiles:           100000,
			MaxSourceBytesPerSnapshot: 536870912,
			MaxParallelIndexJobs:      2,
			MaxParseWorkers:           0,
			MaxLspSessions:            1,
			SqliteBusyTimeoutMs:       5000,
			WriterLockWaitMs:          5000,
			QueryTimeoutMs:            10000,
			LspStartupTimeoutMs:       30000,
			RenameTimeoutMs:           30000,
			IndexTimeoutMs:            600000,
			WatchPollIntervalMs:       2000,
			FullReconcileIntervalMs:   60000,
			MaxOutputBytes:            24576,
			MaxGraphNodes:             2000,
			MaxGraphEdges:             10000,
			MaxGraphDepth:             8,
			KeepGenerations:           2,
			PackTtlSeconds:            900,
			MaxWorktreeCacheBytes:     2147483648,
			LogFiles:                  5,
			LogFileBytes:              10485760,
			MaintenanceWaitMs:         5000,
			RenameResultMaxBytes:      33554432,
			RenameResultsMaxBytes:     134217728,
			RenameResultTtlSeconds:    900,
			RenameMaxEdits:            10000,
			ResultTtlSeconds:          900,
			QueryResultCacheBytes:     33554432,
			MaxRetainedQueryResults:   32,
		},
		Explorer: ExplorerRuntimeLimits{
			GraphDefaultNodes:   200,
			GraphDefaultEdges:   500,
			GraphHardNodes:      500,
			GraphHardEdges:      1500,
			FlowMaxSteps:        100,
			FlowMaxEdges:        300,
			HoverHz:             15,
			SessionTtlSeconds:   1800,
			BearerTtlSeconds:    1800,
			AckTimeoutMs:        3000,
			DetachTimeoutMs:     1000,
			PollVisibleMs:       2000,
			PollHiddenMs:        5000,
			PollMissLimit:       2,
			BridgeMessageBytes:  16384,
			MaxPreviewSessions:  4,
			MapWatchPollMs:      2000,
			MapArtifactMaxBytes: 67108864,
			MapPendingMax:       1,
			MapWatchDebounceMs:  300,
		},
	}
}
func validateRuntimeConfig(c RuntimeConfig) error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("unsupported runtime config version")
	}
	if c.Core.MaxFileBytes < 1 || c.Core.MaxFileBytes > 9007199254740991 {
		return fmt.Errorf("invalid core.max_file_bytes")
	}
	if c.Core.MaxIndexedFiles < 1 || c.Core.MaxIndexedFiles > 100000 {
		return fmt.Errorf("invalid core.max_indexed_files")
	}
	if c.Core.MaxSourceBytesPerSnapshot < 1 || c.Core.MaxSourceBytesPerSnapshot > 9007199254740991 {
		return fmt.Errorf("invalid core.max_source_bytes_per_snapshot")
	}
	if c.Core.MaxParallelIndexJobs < 1 || c.Core.MaxParallelIndexJobs > 16 {
		return fmt.Errorf("invalid core.max_parallel_index_jobs")
	}
	if c.Core.MaxParseWorkers < 0 || c.Core.MaxParseWorkers > 64 {
		return fmt.Errorf("invalid core.max_parse_workers")
	}
	if c.Core.MaxLspSessions < 1 || c.Core.MaxLspSessions > 8 {
		return fmt.Errorf("invalid core.max_lsp_sessions")
	}
	if c.Core.SqliteBusyTimeoutMs < 1 || c.Core.SqliteBusyTimeoutMs > 9007199254740991 {
		return fmt.Errorf("invalid core.sqlite_busy_timeout_ms")
	}
	if c.Core.WriterLockWaitMs < 1 || c.Core.WriterLockWaitMs > 9007199254740991 {
		return fmt.Errorf("invalid core.writer_lock_wait_ms")
	}
	if c.Core.QueryTimeoutMs < 1 || c.Core.QueryTimeoutMs > 9007199254740991 {
		return fmt.Errorf("invalid core.query_timeout_ms")
	}
	if c.Core.LspStartupTimeoutMs < 1 || c.Core.LspStartupTimeoutMs > 9007199254740991 {
		return fmt.Errorf("invalid core.lsp_startup_timeout_ms")
	}
	if c.Core.RenameTimeoutMs < 1 || c.Core.RenameTimeoutMs > 9007199254740991 {
		return fmt.Errorf("invalid core.rename_timeout_ms")
	}
	if c.Core.IndexTimeoutMs < 1 || c.Core.IndexTimeoutMs > 9007199254740991 {
		return fmt.Errorf("invalid core.index_timeout_ms")
	}
	if c.Core.WatchPollIntervalMs < 1 || c.Core.WatchPollIntervalMs > 9007199254740991 {
		return fmt.Errorf("invalid core.watch_poll_interval_ms")
	}
	if c.Core.FullReconcileIntervalMs < 1 || c.Core.FullReconcileIntervalMs > 9007199254740991 {
		return fmt.Errorf("invalid core.full_reconcile_interval_ms")
	}
	if c.Core.MaxOutputBytes < 4096 || c.Core.MaxOutputBytes > 131072 {
		return fmt.Errorf("invalid core.max_output_bytes")
	}
	if c.Core.MaxGraphNodes < 1 || c.Core.MaxGraphNodes > 100000 {
		return fmt.Errorf("invalid core.max_graph_nodes")
	}
	if c.Core.MaxGraphEdges < 1 || c.Core.MaxGraphEdges > 1000000 {
		return fmt.Errorf("invalid core.max_graph_edges")
	}
	if c.Core.MaxGraphDepth < 0 || c.Core.MaxGraphDepth > 8 {
		return fmt.Errorf("invalid core.max_graph_depth")
	}
	if c.Core.KeepGenerations < 2 || c.Core.KeepGenerations > 128 {
		return fmt.Errorf("invalid core.keep_generations")
	}
	if c.Core.PackTtlSeconds < 1 || c.Core.PackTtlSeconds > 9007199254740991 {
		return fmt.Errorf("invalid core.pack_ttl_seconds")
	}
	if c.Core.MaxWorktreeCacheBytes < 1 || c.Core.MaxWorktreeCacheBytes > 9007199254740991 {
		return fmt.Errorf("invalid core.max_worktree_cache_bytes")
	}
	if c.Core.LogFiles < 1 || c.Core.LogFiles > 9007199254740991 {
		return fmt.Errorf("invalid core.log_files")
	}
	if c.Core.LogFileBytes < 1 || c.Core.LogFileBytes > 9007199254740991 {
		return fmt.Errorf("invalid core.log_file_bytes")
	}
	if c.Core.MaintenanceWaitMs < 1 || c.Core.MaintenanceWaitMs > 9007199254740991 {
		return fmt.Errorf("invalid core.maintenance_wait_ms")
	}
	if c.Core.RenameResultMaxBytes < 1 || c.Core.RenameResultMaxBytes > 9007199254740991 {
		return fmt.Errorf("invalid core.rename_result_max_bytes")
	}
	if c.Core.RenameResultsMaxBytes < 1 || c.Core.RenameResultsMaxBytes > 9007199254740991 {
		return fmt.Errorf("invalid core.rename_results_max_bytes")
	}
	if c.Core.RenameResultTtlSeconds < 1 || c.Core.RenameResultTtlSeconds > 9007199254740991 {
		return fmt.Errorf("invalid core.rename_result_ttl_seconds")
	}
	if c.Core.RenameMaxEdits < 1 || c.Core.RenameMaxEdits > 10000 {
		return fmt.Errorf("invalid core.rename_max_edits")
	}
	if c.Core.ResultTtlSeconds < 1 || c.Core.ResultTtlSeconds > 9007199254740991 {
		return fmt.Errorf("invalid core.result_ttl_seconds")
	}
	if c.Core.QueryResultCacheBytes < 1 || c.Core.QueryResultCacheBytes > 9007199254740991 {
		return fmt.Errorf("invalid core.query_result_cache_bytes")
	}
	if c.Core.MaxRetainedQueryResults < 1 || c.Core.MaxRetainedQueryResults > 128 {
		return fmt.Errorf("invalid core.max_retained_query_results")
	}
	if c.Explorer.GraphDefaultNodes < 1 || c.Explorer.GraphDefaultNodes > 500 {
		return fmt.Errorf("invalid explorer.graph_default_nodes")
	}
	if c.Explorer.GraphDefaultEdges < 1 || c.Explorer.GraphDefaultEdges > 1500 {
		return fmt.Errorf("invalid explorer.graph_default_edges")
	}
	if c.Explorer.GraphHardNodes < 1 || c.Explorer.GraphHardNodes > 500 {
		return fmt.Errorf("invalid explorer.graph_hard_nodes")
	}
	if c.Explorer.GraphHardEdges < 1 || c.Explorer.GraphHardEdges > 1500 {
		return fmt.Errorf("invalid explorer.graph_hard_edges")
	}
	if c.Explorer.FlowMaxSteps < 1 || c.Explorer.FlowMaxSteps > 100 {
		return fmt.Errorf("invalid explorer.flow_max_steps")
	}
	if c.Explorer.FlowMaxEdges < 1 || c.Explorer.FlowMaxEdges > 300 {
		return fmt.Errorf("invalid explorer.flow_max_edges")
	}
	if c.Explorer.HoverHz < 1 || c.Explorer.HoverHz > 15 {
		return fmt.Errorf("invalid explorer.hover_hz")
	}
	if c.Explorer.SessionTtlSeconds < 1 || c.Explorer.SessionTtlSeconds > 1800 {
		return fmt.Errorf("invalid explorer.session_ttl_seconds")
	}
	if c.Explorer.BearerTtlSeconds < 1 || c.Explorer.BearerTtlSeconds > 1800 {
		return fmt.Errorf("invalid explorer.bearer_ttl_seconds")
	}
	if c.Explorer.AckTimeoutMs < 1 || c.Explorer.AckTimeoutMs > 9007199254740991 {
		return fmt.Errorf("invalid explorer.ack_timeout_ms")
	}
	if c.Explorer.DetachTimeoutMs < 1 || c.Explorer.DetachTimeoutMs > 9007199254740991 {
		return fmt.Errorf("invalid explorer.detach_timeout_ms")
	}
	if c.Explorer.PollVisibleMs < 1 || c.Explorer.PollVisibleMs > 9007199254740991 {
		return fmt.Errorf("invalid explorer.poll_visible_ms")
	}
	if c.Explorer.PollHiddenMs < 1 || c.Explorer.PollHiddenMs > 9007199254740991 {
		return fmt.Errorf("invalid explorer.poll_hidden_ms")
	}
	if c.Explorer.PollMissLimit < 1 || c.Explorer.PollMissLimit > 9007199254740991 {
		return fmt.Errorf("invalid explorer.poll_miss_limit")
	}
	if c.Explorer.BridgeMessageBytes < 1024 || c.Explorer.BridgeMessageBytes > 16384 {
		return fmt.Errorf("invalid explorer.bridge_message_bytes")
	}
	if c.Explorer.MaxPreviewSessions < 1 || c.Explorer.MaxPreviewSessions > 16 {
		return fmt.Errorf("invalid explorer.max_preview_sessions")
	}
	if c.Explorer.MapWatchPollMs < 1 || c.Explorer.MapWatchPollMs > 9007199254740991 {
		return fmt.Errorf("invalid explorer.map_watch_poll_ms")
	}
	if c.Explorer.MapArtifactMaxBytes < 1 || c.Explorer.MapArtifactMaxBytes > 67108864 {
		return fmt.Errorf("invalid explorer.map_artifact_max_bytes")
	}
	if c.Explorer.MapPendingMax < 1 || c.Explorer.MapPendingMax > 1 {
		return fmt.Errorf("invalid explorer.map_pending_max")
	}
	if c.Explorer.MapWatchDebounceMs < 1 || c.Explorer.MapWatchDebounceMs > 9007199254740991 {
		return fmt.Errorf("invalid explorer.map_watch_debounce_ms")
	}
	if c.Core.MaxFileBytes > c.Core.MaxSourceBytesPerSnapshot || c.Core.FullReconcileIntervalMs < c.Core.WatchPollIntervalMs || c.Core.RenameResultMaxBytes > c.Core.RenameResultsMaxBytes || c.Core.RenameResultsMaxBytes > c.Core.MaxWorktreeCacheBytes || c.Explorer.GraphDefaultNodes > c.Explorer.GraphHardNodes || c.Explorer.GraphDefaultEdges > c.Explorer.GraphHardEdges {
		return fmt.Errorf("inconsistent runtime limits")
	}
	return nil
}

// DecodeRuntimeConfigJSON merges only schema-defined resource fields over the
// documented defaults. Authorization and config-file CAS remain separate.
func DecodeRuntimeConfigJSON(data []byte, maxBytes, availableCPUs int) (RuntimeConfig, error) {
	var empty RuntimeConfig
	fields, err := optionalObject(data, maxBytes, "schema_version", "core", "explorer")
	if err != nil {
		return empty, err
	}
	if _, ok := fields["schema_version"]; !ok {
		return empty, fmt.Errorf("missing schema version")
	}
	for _, key := range []string{"core", "explorer"} {
		if raw, ok := fields[key]; ok {
			var values map[string]json.RawMessage
			if err := json.Unmarshal(raw, &values); err != nil || values == nil {
				return empty, fmt.Errorf("invalid %s object", key)
			}
			fieldType := reflect.TypeOf(CoreRuntimeLimits{})
			if key == "explorer" {
				fieldType = reflect.TypeOf(ExplorerRuntimeLimits{})
			}
			allowed := make(map[string]bool, fieldType.NumField())
			for i := 0; i < fieldType.NumField(); i++ {
				allowed[fieldType.Field(i).Tag.Get("json")] = true
			}
			for name, v := range values {
				if !allowed[name] {
					return empty, fmt.Errorf("unknown %s.%s", key, name)
				}
				if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
					return empty, fmt.Errorf("null %s.%s", key, name)
				}
			}
		}
	}
	result := DefaultRuntimeConfig()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return empty, err
	}
	if err := validateRuntimeConfig(result); err != nil {
		return empty, err
	}
	if result.Core.MaxParseWorkers == 0 {
		if availableCPUs < 1 {
			return empty, fmt.Errorf("CPU count unavailable")
		}
		workers := availableCPUs
		if workers > 4 {
			workers = 4
		}
		result.Core.MaxParseWorkers = int64(workers)
	}
	return result, nil
}
