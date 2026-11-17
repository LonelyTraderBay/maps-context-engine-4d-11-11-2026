package telemetry

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrInvalid     = errors.New("invalid telemetry request")
	ErrUnavailable = errors.New("telemetry storage unavailable")
	ErrUnsupported = errors.New("telemetry configuration or platform unsupported")
)

// Event deliberately has no arbitrary text, source, path, query, token, or
// error message. Callers own Counts until Submit returns and must not mutate it
// concurrently with Submit. Timestamp is assigned by the collector.
type Event struct {
	Level           string           `json:"level"`
	RequestID       string           `json:"request_id,omitempty"`
	JobID           string           `json:"job_id,omitempty"`
	WorktreeID      string           `json:"worktree_id"`
	ProfileID       string           `json:"profile_id,omitempty"`
	GenerationID    string           `json:"generation_id,omitempty"`
	Phase           string           `json:"phase"`
	DurationMS      int64            `json:"duration_ms"`
	Outcome         string           `json:"outcome"`
	ErrorCode       string           `json:"error_code,omitempty"`
	Counts          map[Metric]int64 `json:"counts,omitempty"`
	Budget          *EventBudget     `json:"budget,omitempty"`
	ProviderVersion string           `json:"provider_version,omitempty"`
}

type EventBudget struct {
	MaxOutputBytes  int64 `json:"max_output_bytes"`
	OutputBytes     int64 `json:"output_bytes"`
	RequiredPending int64 `json:"required_pending"`
}

type loggedEvent struct {
	Timestamp string `json:"timestamp"`
	Event
}

func lowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := range len(s) {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func uuid(s string) bool {
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i := range len(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
		} else if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func optionalUUID(s string) bool { return s == "" || uuid(s) }
func validValue(n int64) bool    { return n >= 0 && n <= maxMetricValue }

func validVersion(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 32 {
		return false
	}
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 9 {
			return false
		}
		for i := range len(p) {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}

func validErrorCode(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 128 {
		return false
	}
	data, err := json.Marshal(model.ErrorInfo{Code: s, Message: "operation failed", SuggestedAction: "none", Details: []model.Gap{}})
	if err != nil {
		return false
	}
	_, err = model.DecodeErrorInfoJSON(data, 2048)
	return err == nil
}

func (e Event) validate(worktree string) bool {
	if !uuid(worktree) || e.WorktreeID != worktree || !optionalUUID(e.RequestID) || !optionalUUID(e.JobID) || !optionalUUID(e.GenerationID) || (e.ProfileID != "" && !lowerHex(e.ProfileID, 64)) || !validValue(e.DurationMS) || !validErrorCode(e.ErrorCode) || !validVersion(e.ProviderVersion) {
		return false
	}
	switch e.Level {
	case "debug", "info", "warn", "error":
	default:
		return false
	}
	switch e.Phase {
	case "registration", "inventory", "capture", "index", "query", "freshness", "rename", "recovery", "maintenance", "transport", "provider", "policy", "cursor":
	default:
		return false
	}
	switch e.Outcome {
	case "started", "ok", "partial", "blocked", "cancelled", "timeout", "error":
	default:
		return false
	}
	if len(e.Counts) > len(numericMetrics) {
		return false
	}
	for name, value := range e.Counts {
		if !numericMetrics[name] || !validValue(value) {
			return false
		}
	}
	if e.Budget != nil && (!validValue(e.Budget.MaxOutputBytes) || !validValue(e.Budget.OutputBytes) || !validValue(e.Budget.RequiredPending)) {
		return false
	}
	return true
}

func encodeEvent(e Event, worktree string, now time.Time) ([]byte, error) {
	if !e.validate(worktree) {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(loggedEvent{Timestamp: now.UTC().Format(time.RFC3339Nano), Event: e})
	if err != nil || len(raw) > 8191 {
		return nil, ErrInvalid
	}
	return append(raw, '\n'), nil
}
