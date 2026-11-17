// Package telemetry records bounded operational measurements without source,
// query text, filenames, tokens, or arbitrary diagnostic strings.
package telemetry

import "sync"

type Metric string

const maxMetricValue int64 = 9007199254740991

const (
	InventoryFiles          Metric = "inventory_files"
	FilesExcludedByReason   Metric = "files_excluded_by_reason"
	BytesHashed             Metric = "bytes_hashed"
	CacheHit                Metric = "cache_hit"
	Noop                    Metric = "noop"
	FilesReparsed           Metric = "files_reparsed"
	SemanticRebuildMode     Metric = "semantic_rebuild_mode"
	Nodes                   Metric = "nodes"
	Edges                   Metric = "edges"
	Unresolved              Metric = "unresolved"
	FreshnessDuration       Metric = "freshness_duration"
	FreshnessResult         Metric = "freshness_result"
	QueryLatency            Metric = "query_latency"
	OutputBytes             Metric = "output_bytes"
	RequiredPending         Metric = "required_pending"
	CursorErrors            Metric = "cursor_errors"
	LockWaitMS              Metric = "lock_wait_ms"
	BusyCount               Metric = "busy_count"
	IndexDuration           Metric = "index_duration"
	CancellationLatency     Metric = "cancellation_latency"
	ChildProcessCount       Metric = "child_process_count"
	DiskBytes               Metric = "disk_bytes"
	RecoveryCount           Metric = "recovery_count"
	HierarchyQueryScope     Metric = "hierarchy_query_scope"
	PartitionCacheHit       Metric = "partition_cache_hit"
	InvalidationClosureSize Metric = "invalidation_closure_size"
	SnapshotAnalysisCount   Metric = "snapshot_analysis_count"
	LiveValidationCount     Metric = "live_validation_count"
	SelfIndexRevision       Metric = "self_index_analyzer_revision"
)

var numericMetrics = map[Metric]bool{
	InventoryFiles: true, BytesHashed: true, CacheHit: true, Noop: true,
	FilesReparsed: true, Nodes: true, Edges: true, Unresolved: true,
	FreshnessDuration: true, QueryLatency: true, OutputBytes: true,
	RequiredPending: true, CursorErrors: true, LockWaitMS: true, BusyCount: true,
	IndexDuration: true, CancellationLatency: true, ChildProcessCount: true,
	DiskBytes: true, RecoveryCount: true, PartitionCacheHit: true,
	InvalidationClosureSize: true, SnapshotAnalysisCount: true, LiveValidationCount: true,
}

type ExclusionReason string

const (
	ExcludedPolicy      ExclusionReason = "policy"
	ExcludedSecret      ExclusionReason = "secret"
	ExcludedIgnored     ExclusionReason = "ignored"
	ExcludedSize        ExclusionReason = "size"
	ExcludedUnsupported ExclusionReason = "unsupported"
	ExcludedOther       ExclusionReason = "other"
)

type Measurement struct {
	Observations int64 `json:"observations"`
	Total        int64 `json:"total"`
	Last         int64 `json:"last"`
	Maximum      int64 `json:"maximum"`
	Saturated    bool  `json:"saturated"`
}

type MetricSnapshot struct {
	Measurements      map[Metric]Measurement    `json:"measurements"`
	Excluded          map[ExclusionReason]int64 `json:"files_excluded_by_reason"`
	ExcludedSaturated map[ExclusionReason]bool  `json:"excluded_saturated"`
	States            map[Metric]string         `json:"states"`
}

// Metrics has fixed cardinality. A missing measurement means not observed,
// rather than an assertion that the value was zero.
type Metrics struct {
	mu               sync.Mutex
	values           map[Metric]Measurement
	exclude          map[ExclusionReason]int64
	excludeSaturated map[ExclusionReason]bool
	states           map[Metric]string
}

func NewMetrics() *Metrics {
	return &Metrics{values: make(map[Metric]Measurement), exclude: make(map[ExclusionReason]int64), excludeSaturated: make(map[ExclusionReason]bool), states: make(map[Metric]string)}
}

func addSaturating(a, b int64) (int64, bool) {
	if b > maxMetricValue-a {
		return maxMetricValue, true
	}
	return a + b, false
}

// Observe returns false for invalid inputs; instrumentation callers need not
// propagate that rejection into the operation being measured.
func (m *Metrics) Observe(name Metric, value int64) bool {
	if m == nil || !numericMetrics[name] || value < 0 || value > maxMetricValue {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		return false
	}
	item := m.values[name]
	var overflow bool
	item.Total, overflow = addSaturating(item.Total, value)
	item.Saturated = item.Saturated || overflow
	item.Observations, overflow = addSaturating(item.Observations, 1)
	item.Saturated = item.Saturated || overflow
	item.Last = value
	if value > item.Maximum {
		item.Maximum = value
	}
	m.values[name] = item
	return true
}

func (m *Metrics) Exclude(reason ExclusionReason, count int64) bool {
	if m == nil || count < 0 || count > maxMetricValue {
		return false
	}
	switch reason {
	case ExcludedPolicy, ExcludedSecret, ExcludedIgnored, ExcludedSize, ExcludedUnsupported, ExcludedOther:
	default:
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exclude == nil || m.excludeSaturated == nil {
		return false
	}
	value, overflow := addSaturating(m.exclude[reason], count)
	m.exclude[reason] = value
	m.excludeSaturated[reason] = m.excludeSaturated[reason] || overflow
	return true
}

func (m *Metrics) State(name Metric, value string) bool {
	if m == nil || !validMetricState(name, value) {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states == nil {
		return false
	}
	m.states[name] = value
	return true
}

func validMetricState(name Metric, value string) bool {
	switch name {
	case SemanticRebuildMode:
		switch value {
		case "full", "selective", "none", "partial", "unknown":
			return true
		}
	case FreshnessResult:
		switch value {
		case "observed_current", "changed", "unknown", "not_checked":
			return true
		}
	case HierarchyQueryScope:
		switch value {
		case "package", "module", "worktree", "group", "unknown":
			return true
		}
	case SelfIndexRevision:
		return value == "unknown" || lowerHex(value, 40) || lowerHex(value, 64)
	}
	return false
}

func (m *Metrics) Snapshot() MetricSnapshot {
	result := MetricSnapshot{Measurements: map[Metric]Measurement{}, Excluded: map[ExclusionReason]int64{}, ExcludedSaturated: map[ExclusionReason]bool{}, States: map[Metric]string{}}
	if m == nil {
		return result
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, value := range m.values {
		result.Measurements[key] = value
	}
	for key, value := range m.exclude {
		result.Excluded[key] = value
	}
	for key, value := range m.excludeSaturated {
		result.ExcludedSaturated[key] = value
	}
	for key, value := range m.states {
		result.States[key] = value
	}
	return result
}
