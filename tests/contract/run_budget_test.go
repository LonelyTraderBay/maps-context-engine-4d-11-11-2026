package contract

import (
	"errors"
	"math"
	"testing"
)

// admissionFixture is an independent RUN-03 oracle; it is not runtime enforcement.
type admissionFixture struct {
	available, block, warning, peak, others, reserve int64
	measured                                         bool
}

func (b admissionFixture) decide() (bool, bool, error) {
	if !b.measured || b.available < 0 || b.block < 0 || b.warning < b.block || b.peak < 0 || b.others < 0 || b.reserve < 0 {
		return false, false, errors.New("invalid or unknown measurement")
	}
	if b.peak > math.MaxInt64-b.others || b.peak+b.others > math.MaxInt64-b.reserve {
		return false, false, errors.New("budget overflow")
	}
	required := b.peak + b.others + b.reserve
	if b.block > required {
		required = b.block
	}
	return b.available >= required, b.available < b.warning, nil
}
func TestRunBudgetContractBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name             string
		b                admissionFixture
		allow, warn, bad bool
	}{
		{"exact boundary", admissionFixture{100, 10, 100, 40, 30, 30, true}, true, false, false},
		{"one byte short", admissionFixture{99, 10, 100, 40, 30, 30, true}, false, true, false},
		{"blocking threshold", admissionFixture{40, 50, 60, 1, 1, 1, true}, false, true, false},
		{"unknown", admissionFixture{100, 1, 1, 1, 1, 1, false}, false, false, true},
		{"negative", admissionFixture{100, 1, 1, -1, 1, 1, true}, false, false, true},
		{"warning invalid", admissionFixture{100, 2, 1, 1, 1, 1, true}, false, false, true},
		{"overflow", admissionFixture{100, 1, 1, math.MaxInt64, 1, 0, true}, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allow, warn, err := tc.b.decide()
			if (err != nil) != tc.bad || allow != tc.allow || warn != tc.warn {
				t.Fatalf("admission=%v warning=%v err=%v", allow, warn, err)
			}
		})
	}
}
func TestRunBudgetRemainingReservationAndVolumes(t *testing.T) {
	// Capacity already reflects 40 bytes written; only the 10 bytes remaining
	// from the other job contribute to future demand.
	shared := admissionFixture{available: 60, block: 0, warning: 0, peak: 40, others: 10, reserve: 10, measured: true}
	separate := admissionFixture{available: 50, block: 0, warning: 0, peak: 30, others: 10, reserve: 10, measured: true}
	for _, volume := range []admissionFixture{shared, separate} {
		ok, _, err := volume.decide()
		if err != nil || !ok {
			t.Fatalf("valid volume rejected: %v", err)
		}
	}
	separate.available--
	ok, _, err := separate.decide()
	if err != nil || ok {
		t.Fatal("unavailable destination must block admission")
	}
}

// aggregateFixture uses resolved backing-volume IDs supplied by a trusted meter;
// it does not discover volumes or coordinate external writers.
func aggregateFixture(paths map[string]string, volumes map[string]admissionFixture) bool {
	checked := map[string]bool{}
	for _, volume := range paths {
		if volume == "" {
			return false
		}
		if checked[volume] {
			continue
		}
		checked[volume] = true
		state, ok := volumes[volume]
		if !ok {
			return false
		}
		allowed, _, err := state.decide()
		if err != nil || !allowed {
			return false
		}
	}
	return len(checked) > 0
}
func TestRunBudgetAggregatesBackingVolumes(t *testing.T) {
	paths := map[string]string{"repo": "disk-a", "cache": "disk-a", "temp": "disk-b"}
	volumes := map[string]admissionFixture{"disk-a": {available: 100, block: 20, warning: 30, peak: 40, others: 30, reserve: 30, measured: true}, "disk-b": {available: 20, block: 20, warning: 20, peak: 10, others: 0, reserve: 10, measured: true}}
	if !aggregateFixture(paths, volumes) {
		t.Fatal("valid grouped volumes rejected")
	}
	delete(volumes, "disk-b")
	if aggregateFixture(paths, volumes) {
		t.Fatal("unmeasured destination accepted")
	}
	paths["temp"] = ""
	if aggregateFixture(paths, volumes) {
		t.Fatal("unknown volume accepted")
	}
}

// This is a small admission model for the verified coordinator scope only;
// it cannot make claims about unrelated processes or global filesystem usage.
type reservationFixture struct {
	output    string
	remaining int64
}

func canAdmitFixture(output string, available, peak, reserve int64, jobs []reservationFixture) (bool, string) {
	if output == "" {
		return false, "unknown_output"
	}
	var other int64
	for _, job := range jobs {
		if job.output == output {
			return false, "shared_output_requires_lock"
		}
		if job.remaining < 0 || other > math.MaxInt64-job.remaining {
			return false, "invalid_reservation"
		}
		other += job.remaining
	}
	ok, _, err := (admissionFixture{available: available, block: 0, warning: 0, peak: peak, others: other, reserve: reserve, measured: true}).decide()
	if err != nil {
		return false, "invalid_budget"
	}
	if !ok {
		return false, "insufficient"
	}
	return true, "coordinated_scope_only"
}
func TestRunAdmissionSeparateAndSharedOutput(t *testing.T) {
	jobs := []reservationFixture{{output: "job-a", remaining: 20}}
	if ok, scope := canAdmitFixture("job-b", 100, 60, 20, jobs); !ok || scope != "coordinated_scope_only" {
		t.Fatal("independent output incorrectly blocked")
	}
	if ok, scope := canAdmitFixture("job-a", 100, 1, 1, jobs); ok || scope != "shared_output_requires_lock" {
		t.Fatal("shared output admitted without lock")
	}
	if ok, _ := canAdmitFixture("job-b", 99, 60, 20, jobs); ok {
		t.Fatal("remaining demand exceeded capacity")
	}
}
