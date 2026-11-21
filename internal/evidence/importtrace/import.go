// Package importtrace admits explicitly approved local trace exports.
package importtrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

const (
	MaxExportBytes   = 1 << 20
	MaxSpans         = 1024
	MaxLinksPerSpan  = 32
	SanitizerVersion = "runtime-trace-v1"
)

var (
	ErrApproval = errors.New("runtime export approval unavailable")
	ErrExport   = errors.New("runtime export is invalid or outside the approved metadata format")
	ErrDigest   = errors.New("runtime export differs from the approved digest")
	slug        = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]{0,79}$`)
)

// Approval comes from the owner. Recheck must authorize this root, path and
// expected input digest against current policy. Labels are owner-approved, not
// inferred from user-provided trace attributes or payloads.
type Approval struct {
	InputHash                         string
	Application, Service, Environment string
	Collector, Instrumentation        string
	Operations                        []string
	Recheck                           func(context.Context, *paths.Root, string, string) error
}

type Metadata struct {
	SchemaVersion   int    `json:"schema_version"`
	Application     string `json:"application"`
	Service         string `json:"service"`
	Environment     string `json:"environment"`
	Revision        string `json:"revision"`
	BuildDigest     string `json:"build_digest"`
	ProfileID       string `json:"profile_id"`
	Collector       string `json:"collector"`
	Instrumentation string `json:"instrumentation"`
	WindowStart     string `json:"window_start"`
	WindowEnd       string `json:"window_end"`
	SampleRatePPM   int    `json:"sample_rate_ppm"`
	Sampling        string `json:"sampling"`
}

type SpanKey struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

type Span struct {
	SpanKey
	ParentSpanID  string    `json:"parent_span_id"`
	InteractionID string    `json:"interaction_id"`
	Operation     string    `json:"operation"`
	StartedAt     string    `json:"started_at"`
	EndedAt       string    `json:"ended_at"`
	Status        string    `json:"status"`
	Links         []SpanKey `json:"links"`
}

// Artifact contains only validated metadata. Export claims cannot establish a
// static source binding. The storage owner must resolve and authorize one
// independently before any projection treats an observation as bound.
type Artifact struct {
	hash     string
	metadata Metadata
	spans    []Span
}

type traceWire struct {
	Metadata json.RawMessage   `json:"metadata"`
	Spans    []json.RawMessage `json:"spans"`
}

func (a Artifact) InputHash() string  { return a.hash }
func (a Artifact) Metadata() Metadata { return a.metadata }
func (a Artifact) Spans() []Span {
	spans := make([]Span, len(a.spans))
	copy(spans, a.spans)
	for i := range spans {
		spans[i].Links = append([]SpanKey{}, spans[i].Links...)
	}
	return spans
}

// Read opens a single no-follow regular file beneath an approved local root.
// The digest pins the exact bytes, including when an open file changes.
func Read(ctx context.Context, root *paths.Root, relativePath string, approved Approval) (Artifact, error) {
	if ctx == nil || root == nil || approved.Recheck == nil || !model.ValidDigest(approved.InputHash) {
		return Artifact{}, ErrApproval
	}
	if err := approved.Recheck(ctx, root, relativePath, approved.InputHash); err != nil {
		return Artifact{}, ErrApproval
	}
	scanner, err := policy.NewScanner(MaxExportBytes)
	if err != nil {
		return Artifact{}, err
	}
	raw, err := scanner.ReadSource(root, relativePath)
	if err != nil {
		return Artifact{}, ErrExport
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != approved.InputHash {
		return Artifact{}, ErrDigest
	}
	artifact, err := decode(raw, approved)
	if err != nil {
		return Artifact{}, err
	}
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	if err := approved.Recheck(ctx, root, relativePath, approved.InputHash); err != nil {
		return Artifact{}, ErrApproval
	}
	return artifact, nil
}

// DecodeStored checks the format of normalized rows from an authenticated
// records snapshot. The caller is responsible for authorization and store
// integrity. The original export bytes are unavailable at this boundary.
func DecodeStored(inputHash string, metadata json.RawMessage, spans []json.RawMessage) (Artifact, error) {
	if !model.ValidDigest(inputHash) || len(spans) == 0 || len(spans) > MaxSpans {
		return Artifact{}, ErrExport
	}
	total := len(metadata)
	for _, raw := range spans {
		total += len(raw)
		if total > MaxExportBytes {
			return Artifact{}, ErrExport
		}
	}
	var labels Metadata
	if json.Unmarshal(metadata, &labels) != nil {
		return Artifact{}, ErrExport
	}
	approval := Approval{InputHash: inputHash, Application: labels.Application, Service: labels.Service,
		Environment: labels.Environment, Collector: labels.Collector, Instrumentation: labels.Instrumentation}
	for _, raw := range spans {
		var row struct {
			Operation string `json:"operation"`
		}
		if json.Unmarshal(raw, &row) != nil {
			return Artifact{}, ErrExport
		}
		approval.Operations = append(approval.Operations, row.Operation)
	}
	raw, err := json.Marshal(traceWire{Metadata: metadata, Spans: spans})
	if err != nil {
		return Artifact{}, ErrExport
	}
	return decode(raw, approval)
}

func decode(raw []byte, approved Approval) (Artifact, error) {
	if _, err := model.DecodeIdentityJSON(raw, MaxExportBytes); err != nil {
		return Artifact{}, ErrExport
	}
	var wire traceWire
	if !objectFields(raw, "metadata", "spans") || json.Unmarshal(raw, &wire) != nil ||
		len(wire.Spans) == 0 || len(wire.Spans) > MaxSpans {
		return Artifact{}, ErrExport
	}
	artifact := Artifact{hash: approved.InputHash, spans: make([]Span, 0, len(wire.Spans))}
	if !objectFields(wire.Metadata, "schema_version", "application", "service", "environment", "revision",
		"build_digest", "profile_id", "collector", "instrumentation", "window_start", "window_end", "sample_rate_ppm", "sampling") ||
		json.Unmarshal(wire.Metadata, &artifact.metadata) != nil {
		return Artifact{}, ErrExport
	}
	windowStart, windowEnd, err := validateMetadata(artifact.metadata, approved)
	if err != nil {
		return Artifact{}, err
	}
	operations := make(map[string]bool, len(approved.Operations))
	for _, operation := range approved.Operations {
		if !slug.MatchString(operation) {
			return Artifact{}, ErrApproval
		}
		operations[operation] = true
	}
	seen := make(map[SpanKey]bool, len(wire.Spans))
	for _, item := range wire.Spans {
		span, err := decodeSpan(item, windowStart, windowEnd, operations)
		if err != nil || seen[span.SpanKey] {
			return Artifact{}, ErrExport
		}
		seen[span.SpanKey] = true
		artifact.spans = append(artifact.spans, span)
	}
	return artifact, nil
}

func objectFields(raw []byte, exact ...string) bool {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != len(exact) {
		return false
	}
	for _, key := range exact {
		value, found := values[key]
		if !found || string(value) == "null" {
			return false
		}
	}
	return true
}

func validateMetadata(m Metadata, approved Approval) (time.Time, time.Time, error) {
	if m.Application != approved.Application || m.Service != approved.Service || m.Environment != approved.Environment ||
		m.Collector != approved.Collector || m.Instrumentation != approved.Instrumentation {
		return time.Time{}, time.Time{}, ErrApproval
	}
	for _, label := range []string{m.Application, m.Service, m.Collector, m.Instrumentation} {
		if !slug.MatchString(label) {
			return time.Time{}, time.Time{}, ErrExport
		}
	}
	switch m.Environment {
	case "development", "test", "staging", "production":
	default:
		return time.Time{}, time.Time{}, ErrExport
	}
	switch m.Sampling {
	case "all":
		if m.SampleRatePPM != 1_000_000 {
			return time.Time{}, time.Time{}, ErrExport
		}
	case "head", "tail", "unknown":
	default:
		return time.Time{}, time.Time{}, ErrExport
	}
	start, end, err := interval(m.WindowStart, m.WindowEnd)
	if err != nil || m.SchemaVersion != 1 || m.SampleRatePPM < 0 || m.SampleRatePPM > 1_000_000 ||
		(m.Revision != "" && !opaqueHex(m.Revision, 40) && !model.ValidDigest(m.Revision)) ||
		(m.BuildDigest != "" && !model.ValidDigest(m.BuildDigest)) ||
		(m.ProfileID != "" && !model.ValidDigest(m.ProfileID)) {
		return time.Time{}, time.Time{}, ErrExport
	}
	return start, end, nil
}

func decodeSpan(raw json.RawMessage, windowStart, windowEnd time.Time, operations map[string]bool) (Span, error) {
	var span Span
	if !objectFields(raw, "trace_id", "span_id", "parent_span_id", "interaction_id", "operation",
		"started_at", "ended_at", "status", "links") || json.Unmarshal(raw, &span) != nil ||
		!validSpanKey(span.SpanKey) || !operations[span.Operation] ||
		(span.ParentSpanID != "" && (!opaqueHex(span.ParentSpanID, 16) || span.ParentSpanID == span.SpanID)) ||
		(span.InteractionID != "" && !model.ValidUUID(span.InteractionID)) {
		return Span{}, ErrExport
	}
	start, end, err := interval(span.StartedAt, span.EndedAt)
	if err != nil || start.Before(windowStart) || end.After(windowEnd) {
		return Span{}, ErrExport
	}
	switch span.Status {
	case "unset", "ok", "error":
	default:
		return Span{}, ErrExport
	}
	var item struct {
		Links []json.RawMessage `json:"links"`
	}
	if json.Unmarshal(raw, &item) != nil || len(item.Links) > MaxLinksPerSpan {
		return Span{}, ErrExport
	}
	seen := make(map[SpanKey]bool, len(item.Links))
	for i, link := range item.Links {
		key := span.Links[i]
		if !objectFields(link, "trace_id", "span_id") || !validSpanKey(key) || key == span.SpanKey || seen[key] {
			return Span{}, ErrExport
		}
		seen[key] = true
	}
	return span, nil
}

func validSpanKey(key SpanKey) bool {
	return opaqueHex(key.TraceID, 32) && opaqueHex(key.SpanID, 16)
}

func opaqueHex(value string, width int) bool {
	if len(value) != width || value == strings.Repeat("0", width) {
		return false
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func interval(from, through string) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339Nano, from)
	if err != nil || !strings.HasSuffix(from, "Z") {
		return time.Time{}, time.Time{}, ErrExport
	}
	end, err := time.Parse(time.RFC3339Nano, through)
	if err != nil || !strings.HasSuffix(through, "Z") || end.Before(start) {
		return time.Time{}, time.Time{}, ErrExport
	}
	return start, end, nil
}
