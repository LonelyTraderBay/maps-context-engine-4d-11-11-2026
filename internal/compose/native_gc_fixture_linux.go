//go:build linux && mce4d_gc_fixture

package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

// ExerciseProtectedGCRollbackFixture is available only in the acceptance
// build. The controller provisions a disposable profile and writes the witness.
func ExerciseProtectedGCRollbackFixture(ctx context.Context, worktree, profile, generation string) (checks int, resultErr error) {
	owner, err := os.ReadFile("/result/gc-rollback-fixture")
	if err != nil || string(owner) != generation || os.Geteuid() != 1000 ||
		os.Getenv("HOME") != "/profiles/runtime/home" ||
		!model.ValidUUID(worktree) || !model.ValidDigest(profile) || !model.ValidUUID(generation) {
		return 0, errors.New("invalid protected GC fixture")
	}
	scope, err := openNativeScope(ctx, "/profiles/runtime", "", worktree, profile, 4)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, scope.Close()) }()
	_, lease, _, err := scope.stores.CacheResources(ctx)
	if err != nil {
		return 0, err
	}
	job, err := nativeOperationID()
	if err != nil {
		return 0, err
	}
	writer, err := runtime.AcquireWriter(ctx, lease, job, time.Second)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	store, err := scope.stores.Index.Store()
	if err != nil || store.SchemaVersion() != 4 {
		return 0, fmt.Errorf("V4 index owner required: %v", err)
	}
	durable, err := scope.stores.Records.Store()
	if err != nil {
		return 0, err
	}
	observeRecords := func() (records.State, error) {
		return durable.ReadState(ctx, 20, func(ctx context.Context, id records.Identity) error {
			if id.WorktreeID != worktree {
				return ErrAuthority
			}
			return scope.recheckBinding(ctx)
		})
	}
	authorize := func(ctx context.Context, requestedWorktree, requestedProfile string) error {
		if requestedWorktree != worktree || requestedProfile != profile {
			return ErrAuthority
		}
		return scope.recheckBinding(ctx)
	}
	request := index.GCRequest{ProfileID: profile, JobID: job, KeepGenerations: int(scope.effective.Core.KeepGenerations),
		Writer: writer, Authorize: authorize}
	report, err := store.CollectGeneration(ctx, request, generation, false)
	if err != nil || report.AlreadyAbsent || report.Rows["ui_build_bindings"] != 1 || report.Rows["modules"] != 0 {
		return 0, fmt.Errorf("unexpected V4 fixture candidate: %+v %v", report, err)
	}
	beforeRecords, err := observeRecords()
	if err != nil || beforeRecords.ApprovedHeads != 1 {
		return 0, fmt.Errorf("expected exactly one approved durable record: %v", err)
	}

	// The V4 no-module fixture visits one check per table. The blob entry
	// in report.Rows is handled by the final foreign-key check. The four
	// checks outside the table loop are admission, retire, FK and precommit.
	checks = len(report.Rows) - 1 + 4
	injected := errors.New("fixture GC authority denied")
	for boundary := 1; boundary <= checks; boundary++ {
		for _, expected := range []error{injected, context.Canceled} {
			attempt, stop := context.WithCancel(ctx)
			failing := request
			calls := 0
			failing.Authorize = func(ctx context.Context, w, p string) error {
				if err := authorize(ctx, w, p); err != nil {
					return err
				}
				calls++
				if calls != boundary {
					return nil
				}
				if expected == context.Canceled {
					stop()
					return ctx.Err()
				}
				return expected
			}
			_, err := store.CollectGeneration(attempt, failing, generation, true)
			stop()
			if calls != boundary || !errors.Is(err, expected) {
				return 0, fmt.Errorf("GC check %d, reached %d: expected %v, got %v", boundary, calls, expected, err)
			}
			after, err := store.CollectGeneration(ctx, request, generation, false)
			afterRecords, recordErr := observeRecords()
			if err != nil || recordErr != nil || !reflect.DeepEqual(after, report) || afterRecords != beforeRecords {
				return 0, fmt.Errorf("GC check %d failed to roll back: %+v %v", boundary, after, errors.Join(err, recordErr))
			}
		}
	}
	return checks, nil
}
