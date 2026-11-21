//go:build linux

package records

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestMappingCASReplayAndRollback(t *testing.T) {
	const approveA = "33333333-3333-4333-8333-333333333333"
	const approveB = "44444444-4444-4444-8444-444444444444"
	const failID = "55555555-5555-4555-8555-555555555555"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	statePath := t.TempDir()
	root, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, root, fixtureWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(statePath, "records.sqlite")}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	writer, err := runtime.AcquireWriter(ctx, lease, fixtureJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	identity, initErr := InitializeWithWriter(ctx, db, fixtureWorktree, lease, writer, fixtureJob)
	if err := errors.Join(initErr, writer.Close()); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, db, fixtureWorktree, lease)
	if err != nil {
		t.Fatal(err)
	}
	auth := func(ctx context.Context, observed Identity) error {
		if identity != observed {
			return fixtureDenied
		}
		return ctx.Err()
	}
	verify := func(ctx context.Context, _ model.UIMappingPayload) error { return ctx.Err() }
	hash := strings.Repeat("a", 64)
	ref := model.SourceRef{WorktreeID: fixtureWorktree, ProfileID: hash, GenerationID: fixtureJob,
		Path: "app.tsx", ContentHash: hash, Range: model.ByteRange{StartByte: 1, EndByte: 7}}
	mapping := &model.UIMappingPayload{SourceRef: ref, Rationale: "verified fixture site",
		DestinationRef: &model.SymbolRef{Kind: "symbol", GraphIdentity: model.GraphIdentity{
			WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}, SymbolID: "button"}}
	mutate := func(request MutationRequest) (MutationResult, error) {
		writer, err := runtime.AcquireWriter(ctx, lease, request.Mutation.MutationID, time.Second)
		if err != nil {
			return MutationResult{}, err
		}
		request.Writer, request.JobID, request.Authorize = writer, request.Mutation.MutationID, auth
		if request.CheckEvidence == nil {
			request.CheckEvidence = verify
		}
		result, err := store.MutateMapping(ctx, request)
		return result, errors.Join(err, writer.Close())
	}
	propose := MutationRequest{Action: "propose", Reason: "fixture proposal", Mapping: mapping,
		Mutation: model.MutationIdentity{MutationID: fixtureJob, PrincipalID: "maintainer", RequestHash: hash}}
	result, err := mutate(propose)
	if err != nil || result.Record.Version != 1 || result.Record.DeclaredStatus != "proposed" || result.State.RecordEpoch != 1 {
		t.Fatalf("propose: %+v %v", result, err)
	}
	approve := MutationRequest{Action: "approve", Reason: "fixture approval", RecordID: result.Record.RecordID, ExpectedVersion: 1,
		Mutation: model.MutationIdentity{MutationID: approveA, PrincipalID: "maintainer", RequestHash: hash}}
	willFail := approve
	willFail.Mutation.MutationID = failID
	calls := 0
	willFail.CheckEvidence = func(context.Context, model.UIMappingPayload) error {
		calls++
		if calls == 2 {
			return fixtureDenied
		}
		return nil
	}
	if _, err := mutate(willFail); !errors.Is(err, fixtureDenied) {
		t.Fatalf("late denial: %v", err)
	}
	unchanged, err := store.ReadVersion(ctx, result.Record.RecordID, 0, auth)
	if err != nil || unchanged.Record.Version != 1 || unchanged.State.RecordEpoch != 1 {
		t.Fatalf("rollback changed head/epoch: %+v %v", unchanged, err)
	}
	assertCount := func(sqlText string, want int) {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, sqlText).Scan(&count); err != nil || count != want {
			t.Fatalf("count: got %d want %d, error %v", count, want, err)
		}
	}
	assertCount("SELECT count(*) FROM record_versions", 1)
	assertCount("SELECT count(*) FROM mutation_receipts", 1)

	type approval struct {
		request MutationRequest
		output  MutationResult
		err     error
	}
	results := make(chan approval, 2)
	for _, id := range []string{approveA, approveB} {
		go func() {
			request := approve
			request.Mutation.MutationID = id
			output, err := mutate(request)
			results <- approval{request, output, err}
		}()
	}
	var win approval
	var successes int
	for range 2 {
		output := <-results
		if output.err == nil {
			win = output
			successes++
		} else if !errors.Is(output.err, model.ErrRecordConflict) {
			t.Fatalf("approval error: %v", output.err)
		}
	}
	if successes != 1 || win.output.Record.Version != 2 || win.output.Record.DeclaredStatus != "approved" || win.output.State.RecordEpoch != 2 {
		t.Fatalf("CAS did not admit exactly one approval: %d %+v", successes, win)
	}
	assertCount("SELECT count(*) FROM record_versions", 2)
	assertCount("SELECT count(*) FROM mutation_receipts", 2)
	replayed, err := mutate(win.request)
	if err != nil || !replayed.Replayed || replayed.Record.RecordID != win.output.Record.RecordID || replayed.Record.Version != 2 {
		t.Fatalf("receipt did not return committed version: %+v %v", replayed, err)
	}
	for _, field := range []string{"principal", "hash"} {
		changed := win.request
		if field == "principal" {
			changed.Mutation.PrincipalID = "another"
		} else {
			changed.Mutation.RequestHash = strings.Repeat("b", 64)
		}
		if _, err := mutate(changed); !errors.Is(err, model.ErrRecordConflict) {
			t.Fatalf("reuse from different %s: %v", field, err)
		}
	}
	head, err := store.ReadVersion(ctx, result.Record.RecordID, 0, auth)
	if err != nil || head.Record.DeclaredStatus != "approved" || head.State.RecordEpoch != 2 {
		t.Fatalf("head %v %v", head, err)
	}
	history, err := store.ReadVersion(ctx, result.Record.RecordID, 1, auth)
	if err != nil || history.Record.DeclaredStatus != "proposed" || history.Record.UIMapping.SourceRef != ref {
		t.Fatalf("history %v %v", history, err)
	}

	propose.Mutation.MutationID = approveB
	if win.request.Mutation.MutationID == approveB {
		propose.Mutation.MutationID = approveA
	}
	added, err := mutate(propose)
	if err != nil || added.State.RecordEpoch != 3 {
		t.Fatalf("second proposal %v %v", added, err)
	}
	page, err := store.ReadHeadPage(ctx, HeadPageRequest{Limit: 1, Authorize: auth})
	if err != nil || len(page.Records) != 1 || !page.HasMore {
		t.Fatalf("page 1: %v %v", page, err)
	}
	next, err := store.ReadHeadPage(ctx, HeadPageRequest{Limit: 1, AfterID: page.Records[0].RecordID, Expected: &page.State, Authorize: auth})
	if err != nil || len(next.Records) != 1 || next.Records[0].RecordID == page.Records[0].RecordID || next.HasMore {
		t.Fatalf("page 2: %v %v", next, err)
	}
	if _, err := store.ReadHeadPage(ctx, HeadPageRequest{Limit: 1, Expected: &head.State, Authorize: auth}); !errors.Is(err, ErrRecordChanged) {
		t.Fatalf("stale epoch accepted: %v", err)
	}
}
