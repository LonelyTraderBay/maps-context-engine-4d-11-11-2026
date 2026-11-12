package indexer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

var errRecoveryFixture = errors.New("recovery fixture failure")

type recoveryFixtureLease struct{}
type recoveryFixtureGuard struct{}

func (recoveryFixtureLease) CheckRecovery(context.Context, string, string) error { return nil }
func (recoveryFixtureLease) HoldRecovery(context.Context, string, string) (ports.LeaseGuard, error) {
	return recoveryFixtureGuard{}, nil
}
func (recoveryFixtureGuard) Close() error { return nil }

type recoveryFixtureStore struct {
	items    []ports.RecoveryCandidate
	failID   string
	attempts map[string]int
}

func (s *recoveryFixtureStore) ListBuildingForRecovery(_ context.Context, _ string, _ ports.IndexRecoveryLease, after string, limit int, _ ports.RecoveryAuthorizer) ([]ports.RecoveryCandidate, error) {
	var result []ports.RecoveryCandidate
	for _, candidate := range s.items {
		if candidate.GenerationID > after && s.attempts[candidate.GenerationID] == 0 && len(result) < limit {
			result = append(result, candidate)
		}
	}
	return result, nil
}
func (s *recoveryFixtureStore) FailAbandonedGeneration(_ context.Context, _ string, _ ports.IndexRecoveryLease, candidate ports.RecoveryCandidate, _ ports.RecoveryAuthorizer) error {
	s.attempts[candidate.GenerationID]++
	if candidate.GenerationID == s.failID {
		return errRecoveryFixture
	}
	return nil
}
func (*recoveryFixtureStore) ObserveRecoveryCandidate(context.Context, string, ports.IndexRecoveryLease, ports.RecoveryCandidate, ports.RecoveryAuthorizer) (string, error) {
	return "building", nil
}

func TestRecoverAbandonedHonorsBudgetAndDoesNotRetryUncertainWrite(t *testing.T) {
	store := &recoveryFixtureStore{items: []ports.RecoveryCandidate{{GenerationID: "a"}, {GenerationID: "b"}, {GenerationID: "c"}}, attempts: map[string]int{}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := RecoveryRequest{RecoveryID: "fixture", Lease: recoveryFixtureLease{}, Store: store, Authorize: func(context.Context, string, ports.RecoveryCandidate) error { return nil }, BatchSize: 2, MaxCandidates: 2}
	report, err := RecoverAbandoned(ctx, request)
	if err != nil || report.Failed != 2 || report.LastGenerationID != "b" || !report.MayHaveMore || report.Pending != nil {
		t.Fatalf("wrong bounded report: %+v %v", report, err)
	}
	request.AfterGenerationID, request.MaxCandidates = report.LastGenerationID, 2
	store.failID = "c"
	report, err = RecoverAbandoned(ctx, request)
	if !errors.Is(err, errRecoveryFixture) || report.Failed != 0 || report.Pending == nil || report.Pending.GenerationID != "c" || store.attempts["c"] != 1 {
		t.Fatalf("lost uncertain candidate or retried: %+v %v", report, err)
	}
}
