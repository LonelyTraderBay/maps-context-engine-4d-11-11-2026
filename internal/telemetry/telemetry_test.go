package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const worktree = "11111111-1111-4111-8111-111111111111"

func goodEvent() Event {
	return Event{Level: "info", WorktreeID: worktree, Phase: "query", Outcome: "ok", ProviderVersion: "v0.24.0", Counts: map[Metric]int64{InventoryFiles: 1}}
}

func TestEventClosedSchemaAndRedaction(t *testing.T) {
	secret := strings.Repeat("secret_filename_query_token_", 40000)
	good := goodEvent()
	good.RequestID = "22222222-2222-4222-8222-222222222222"
	good.ProfileID = strings.Repeat("a", 64)
	good.Budget = &EventBudget{MaxOutputBytes: 1024, OutputBytes: 23, RequiredPending: 2}
	good.ErrorCode = "INDEX_STALE"
	raw, err := encodeEvent(good, worktree, time.Date(2026, 1, 1, 1, 2, 3, 4, time.FixedZone("x", 3600)))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(bytes.TrimSuffix(raw, []byte("\n")), &decoded) != nil || decoded["timestamp"] != "2026-01-01T00:02:03.000000004Z" {
		t.Fatalf("bad event: %s", raw)
	}
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatal("unexpected secret")
	}
	cases := []Event{
		func() Event { e := goodEvent(); e.Phase = secret; return e }(),
		func() Event { e := goodEvent(); e.ErrorCode = secret; return e }(),
		func() Event { e := goodEvent(); e.ProviderVersion = secret; return e }(),
		func() Event { e := goodEvent(); e.WorktreeID = secret; return e }(),
		func() Event { e := goodEvent(); e.Counts = map[Metric]int64{Metric(secret): 1}; return e }(),
		func() Event { e := goodEvent(); e.Counts = map[Metric]int64{BytesHashed: -1}; return e }(),
		func() Event { e := goodEvent(); e.ErrorCode = "UNTRUSTED_ERROR"; return e }(),
		func() Event { e := goodEvent(); e.ProviderVersion = "v1.2.3-secret"; return e }(),
		func() Event { e := goodEvent(); e.Budget = &EventBudget{OutputBytes: maxMetricValue + 1}; return e }(),
	}
	for i, event := range cases {
		out, err := encodeEvent(event, worktree, time.Now())
		if !errors.Is(err, ErrInvalid) || len(out) != 0 || strings.Contains(err.Error(), secret[:20]) {
			t.Fatalf("case %d leaked or accepted", i)
		}
	}
}

func TestMetricsBoundedConcurrentAndSnapshotIsolation(t *testing.T) {
	m := NewMetrics()
	if len(m.Snapshot().Measurements) != 0 {
		t.Fatal("unobserved value became zero")
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				if !m.Observe(BytesHashed, 2) || !m.Exclude(ExcludedSecret, 1) {
					t.Error("valid measurement rejected")
					return
				}
			}
		}()
	}
	wg.Wait()
	if !m.Observe(BytesHashed, maxMetricValue) || !m.Exclude(ExcludedSecret, maxMetricValue) || !m.State(SemanticRebuildMode, "full") || !m.State(SelfIndexRevision, strings.Repeat("a", 64)) {
		t.Fatal("valid values rejected")
	}
	for _, status := range []string{"observed_current", "changed", "unknown", "not_checked"} {
		if !m.State(FreshnessResult, status) {
			t.Fatalf("freshness status %q rejected", status)
		}
	}
	if m.Observe(Metric("secret"), 1) || m.Observe(BytesHashed, -1) || m.State(FreshnessResult, "/private/path") || m.Exclude(ExclusionReason("private"), 1) {
		t.Fatal("invalid values accepted")
	}
	snapshot := m.Snapshot()
	if snapshot.Measurements[BytesHashed].Total != maxMetricValue || !snapshot.Measurements[BytesHashed].Saturated || snapshot.Excluded[ExcludedSecret] != maxMetricValue || !snapshot.ExcludedSaturated[ExcludedSecret] {
		t.Fatal("saturation not reported")
	}
	delete(snapshot.Measurements, BytesHashed)
	snapshot.States[SemanticRebuildMode] = "private"
	if m.Snapshot().Measurements[BytesHashed].Observations != 32001 || m.Snapshot().States[SemanticRebuildMode] != "full" {
		t.Fatal("snapshot mutates live values")
	}
}

type blockingSink struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	fail    bool
	mu      sync.Mutex
	records [][]byte
}

func (s *blockingSink) appendEvent(ctx context.Context, raw []byte) error {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.fail {
		return errors.New("private/path secret token")
	}
	s.mu.Lock()
	s.records = append(s.records, append([]byte(nil), raw...))
	s.mu.Unlock()
	return nil
}

func TestCollectorBackpressureAndSanitizedFailure(t *testing.T) {
	sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{}), fail: true}
	c, err := newCollector(sink, worktree, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	debug := goodEvent()
	debug.Level = "debug"
	if c.Submit(debug) {
		t.Fatal("debug must be opt in")
	}
	if !c.Submit(goodEvent()) {
		t.Fatal("first submission")
	}
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if !c.Submit(goodEvent()) || c.Submit(goodEvent()) {
		t.Fatal("queue not bounded")
	}
	bad := goodEvent()
	bad.Outcome = "private/path secret"
	if c.Submit(bad) {
		t.Fatal("invalid event accepted")
	}
	close(sink.release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	stats := c.Stats()
	if stats.Written != 0 || stats.Dropped != 1 || stats.Invalid != 1 || stats.SinkFailures != 2 {
		t.Fatalf("unexpected counters: %+v", stats)
	}
	msg := c.TakeDiagnostic()
	if msg == "" || strings.Contains(msg, "private") || strings.Contains(msg, "secret") || c.TakeDiagnostic() != "" {
		t.Fatal("diagnostic not sanitized or not consumed")
	}
	if c.Submit(goodEvent()) {
		t.Fatal("closed collector accepted event")
	}
}

func TestCollectorCancelledCloseEventuallyStops(t *testing.T) {
	sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
	c, err := newCollector(sink, worktree, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Submit(goodEvent()) {
		t.Fatal("submit")
	}
	<-sink.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(c.Close(ctx), context.Canceled) {
		t.Fatal("expected cancellation")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := c.Close(ctx2); err != nil {
		t.Fatal(err)
	}
	if c.Stats().SinkFailures != 1 {
		t.Fatal("missing sink failure")
	}
}

func TestCollectorRepeatedStartStop(t *testing.T) {
	for range 100 {
		sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
		close(sink.release)
		c, err := newCollector(sink, worktree, 4, true)
		if err != nil {
			t.Fatal(err)
		}
		c.Submit(goodEvent())
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = c.Close(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-c.done:
		default:
			t.Fatal("worker still running")
		}
	}
}
