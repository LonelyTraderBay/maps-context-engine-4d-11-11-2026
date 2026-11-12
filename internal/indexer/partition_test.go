package indexer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func partitionFixture(t *testing.T) (context.Context, PartitionInput) {
	t.Helper()
	before := invalidationSnapshot(t, map[string]string{"a.go": "old", "b.go": "b", "c.go": "c", "d.go": "d"}, nil)
	after := invalidationSnapshot(t, map[string]string{"a.go": "new", "b.go": "b", "c.go": "c", "d.go": "d"}, nil)
	ctx, invalid := invalidationRequest(t, before, after)
	fingerprint := strings.Repeat("a", 64)
	parts := []Partition{
		{ID: "A", Variant: "production", Files: []string{"a.go"}, Imports: []string{}, InputFingerprint: fingerprint},
		{ID: "B", Variant: "production", Files: []string{"b.go"}, Imports: []string{"A", "C"}, InputFingerprint: fingerprint},
		{ID: "C", Variant: "production", Files: []string{"c.go"}, Imports: []string{"B"}, InputFingerprint: fingerprint},
		{ID: "D", Variant: "production", Files: []string{"d.go"}, Imports: []string{}, InputFingerprint: fingerprint},
	}
	afterParts := append([]Partition(nil), parts...)
	afterParts[0].InputFingerprint = strings.Repeat("b", 64)
	return ctx, PartitionInput{Invalidation: invalid, Before: parts, After: afterParts, MaxPartitions: 100, MaxEdges: 100, VerifyPartitions: func(_ context.Context, m model.CaptureManifest, d string) (bool, error) {
		return len(d) == 64 && m.SnapshotDigest != "", nil
	}}
}

func TestPartitionReverseClosureCyclesAndFullFallback(t *testing.T) {
	ctx, input := partitionFixture(t)
	result, err := PlanPartitions(ctx, input)
	if err != nil || result.SemanticMode != "full_rebuild" || !result.ClosureVerified || !result.NegativeCacheInvalidated || !reflect.DeepEqual(result.Affected, []string{"A", "B", "C"}) || !reflect.DeepEqual(result.Components, [][]string{{"A"}, {"B", "C"}, {"D"}}) || !reflect.DeepEqual(result.Reasons, []string{"SEMANTIC_SELECTIVE_NOT_PROVEN"}) {
		t.Fatalf("wrong reverse closure: %+v %v", result, err)
	}
	input.Before[1].Imports = []string{"C", "A"}
	input.After[1].Imports = []string{"C", "A"}
	input.Before[0], input.Before[3] = input.Before[3], input.Before[0]
	input.After[0], input.After[3] = input.After[3], input.After[0]
	again, err := PlanPartitions(ctx, input)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("nondeterministic partition graph: %+v %v", again, err)
	}
}

func TestPartitionUnknownClosureAndConfigAlwaysRebuild(t *testing.T) {
	ctx, input := partitionFixture(t)
	input.VerifyPartitions = nil
	plan, err := PlanPartitions(ctx, input)
	if err != nil || plan.ClosureVerified || plan.SemanticMode != "full_rebuild" || !reflect.DeepEqual(plan.Affected, []string{"A", "B", "C", "D"}) || !containsPartitionReason(plan.Reasons, "PARTITION_CLOSURE_UNVERIFIED") {
		t.Fatalf("unproved closure was treated as selective: %+v %v", plan, err)
	}
	ctx, input = partitionFixture(t)
	input.After[0].Imports = []string{"missing"}
	plan, err = PlanPartitions(ctx, input)
	if err != nil || plan.ClosureVerified || !reflect.DeepEqual(plan.Affected, []string{"A", "B", "C", "D"}) || !containsPartitionReason(plan.Reasons, "UNRESOLVED_DEPENDENCY_BOUNDARY") {
		t.Fatalf("unknown dependency was overlooked: %+v %v", plan, err)
	}
	ctx, input = partitionFixture(t)
	input.After = append([]Partition(nil), input.After[1:]...)
	plan, err = PlanPartitions(ctx, input)
	if err != nil || plan.ClosureVerified || !reflect.DeepEqual(plan.Affected, []string{"A", "B", "C", "D"}) || !containsPartitionReason(plan.Reasons, "UNRESOLVED_DEPENDENCY_BOUNDARY") {
		t.Fatalf("dependency present only in old generation was treated as available: %+v %v", plan, err)
	}
	ctx, input = partitionFixture(t)
	input.Invalidation.After = invalidationSnapshot(t, map[string]string{"a.go": "old", "b.go": "b", "c.go": "c", "d.go": "d"}, func(m *model.CaptureManifest) { m.ProviderDigest = strings.Repeat("e", 64) })
	input.After = append([]Partition(nil), input.Before...)
	plan, err = PlanPartitions(ctx, input)
	if err != nil || plan.SemanticMode != "full_rebuild" || !reflect.DeepEqual(plan.Affected, []string{"A", "B", "C", "D"}) || !plan.NegativeCacheInvalidated {
		t.Fatalf("provider change did not rebuild globally: %+v %v", plan, err)
	}
}

func TestPartitionNoopRequiresAllProofsAndRechecks(t *testing.T) {
	ctx, input := partitionFixture(t)
	input.Invalidation.After = input.Invalidation.Before
	input.After = append([]Partition(nil), input.Before...)
	checks := 0
	input.Invalidation.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) {
		checks++
		return true, nil
	}
	plan, err := PlanPartitions(ctx, input)
	if err != nil || checks != 3 || plan.SemanticMode != "reuse_published" || plan.NegativeCacheInvalidated || len(plan.Affected) != 0 {
		t.Fatalf("verified noop incorrect: %+v %v", plan, err)
	}
	checks = 0
	input.Invalidation.VerifyReuse = func(context.Context, model.CaptureManifest, model.CaptureManifest) (bool, error) {
		checks++
		return checks < 3, nil
	}
	if plan, err := PlanPartitions(ctx, input); !errors.Is(err, ErrInvalidationAuthority) || plan.SemanticMode != "" {
		t.Fatalf("revoked noop proof leaked plan: %+v %v", plan, err)
	}
	input.Invalidation.VerifyReuse = nil
	plan, err = PlanPartitions(ctx, input)
	if err != nil || plan.SemanticMode != "full_rebuild" || !plan.NegativeCacheInvalidated || len(plan.Affected) != 4 {
		t.Fatalf("unproved noop did not rebuild: %+v %v", plan, err)
	}
}

func TestPartitionRejectsBadMembershipRevocationAndLimits(t *testing.T) {
	ctx, input := partitionFixture(t)
	input.After[0].Files = []string{"not-captured.go"}
	if result, err := PlanPartitions(ctx, input); !errors.Is(err, ErrInvalidationInput) || result.SemanticMode != "" {
		t.Fatalf("uncaptured file accepted: %v", err)
	}
	ctx, input = partitionFixture(t)
	input.After[0].Imports = []string{"B", "B"}
	if _, err := PlanPartitions(ctx, input); !errors.Is(err, ErrInvalidationInput) {
		t.Fatalf("duplicate dependency accepted: %v", err)
	}
	ctx, input = partitionFixture(t)
	input.MaxEdges = 2
	if _, err := PlanPartitions(ctx, input); !errors.Is(err, ErrPartitionLimit) {
		t.Fatalf("edge budget ignored: %v", err)
	}
	ctx, input = partitionFixture(t)
	checks := 0
	input.VerifyPartitions = func(context.Context, model.CaptureManifest, string) (bool, error) {
		checks++
		if checks == 3 {
			return false, errors.New("private proof")
		}
		return true, nil
	}
	if result, err := PlanPartitions(ctx, input); !errors.Is(err, ErrInvalidationAuthority) || result.SemanticMode != "" || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked graph proof leaked: %v", err)
	}
	ctx, input = partitionFixture(t)
	checks = 0
	input.VerifyPartitions = func(context.Context, model.CaptureManifest, string) (bool, error) { checks++; return checks < 3, nil }
	if result, err := PlanPartitions(ctx, input); !errors.Is(err, ErrInvalidationAuthority) || result.SemanticMode != "" {
		t.Fatalf("changed graph proof was accepted: %v", err)
	}
	ctx, input = partitionFixture(t)
	checks = 0
	input.Invalidation.AuthorizeScope = func(context.Context, string, string, string, int64) error {
		checks++
		if checks == 5 {
			return errors.New("private policy")
		}
		return nil
	}
	if result, err := PlanPartitions(ctx, input); !errors.Is(err, ErrInvalidationAuthority) || result.SemanticMode != "" || strings.Contains(err.Error(), "private") {
		t.Fatalf("revoked partition scope leaked: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := PlanPartitions(canceled, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}

func containsPartitionReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func TestPartitionScaleFanoutSynthetic(t *testing.T) {
	const units = 2000
	makeCapture := func(changed bool) *snapshot.Snapshot {
		m := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
		files := make([]snapshot.File, 0, units)
		for i := 0; i < units; i++ {
			name := fmt.Sprintf("p%04d/unit.go", i)
			body := "package p\n"
			if changed && i == 0 {
				body += "// changed export\n"
			}
			file, err := snapshot.NewFile(name, []byte(body), 1024)
			if err != nil {
				t.Fatal(err)
			}
			hash, size := file.ContentHash(), int64(file.Len())
			m.Files = append(m.Files, model.ManifestFile{Path: name, Status: "captured", ContentHash: &hash, ByteLength: &size})
			files = append(files, file)
		}
		captured, err := snapshot.Freeze(m, files, 2*1024*1024, 4*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		return captured
	}
	before, after := makeCapture(false), makeCapture(true)
	parts := make([]Partition, 0, units)
	for i := 0; i < units; i++ {
		imports := []string{}
		if i > 0 {
			imports = append(imports, "unit-0000")
		}
		parts = append(parts, Partition{ID: fmt.Sprintf("unit-%04d", i), Variant: "production", Files: []string{fmt.Sprintf("p%04d/unit.go", i)}, Imports: imports, InputFingerprint: strings.Repeat("a", 64)})
	}
	afterParts := append([]Partition(nil), parts...)
	afterParts[0].InputFingerprint = strings.Repeat("b", 64)
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	invalid := InvalidationInput{Before: before, After: after, PolicyEpoch: 1, Scanner: scanner, MaxManifestBytes: 2 * 1024 * 1024, MaxChanges: 10000, MaxOutputBytes: 4 * 1024 * 1024,
		AuthorizeScope: func(context.Context, string, string, string, int64) error { return nil }, AuthorizePath: func(context.Context, string, string, string, string, int64) error { return nil }}
	plan, err := PlanPartitions(ctx, PartitionInput{Invalidation: invalid, Before: parts, After: afterParts, MaxPartitions: units, MaxEdges: units * 3, VerifyPartitions: func(context.Context, model.CaptureManifest, string) (bool, error) { return true, nil }})
	if err != nil || len(plan.Affected) != units || len(plan.Components) != units || plan.SemanticMode != "full_rebuild" || !plan.NegativeCacheInvalidated {
		t.Fatalf("fanout closure lost consumers: affected=%d components=%d error=%v", len(plan.Affected), len(plan.Components), err)
	}
}
