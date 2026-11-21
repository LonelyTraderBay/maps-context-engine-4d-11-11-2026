//go:build linux

package history

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

const (
	fixtureWorktree   = "11111111-1111-4111-8111-111111111111"
	fixtureGeneration = "22222222-2222-4222-8222-222222222222"
	fixtureCatalog    = "33333333-3333-4333-8333-333333333333"
	fixtureIndex      = "44444444-4444-4444-8444-444444444444"
	fixturePropose    = "55555555-5555-4555-8555-555555555555"
	fixtureApprove    = "66666666-6666-4666-8666-666666666666"
	fixtureDenied     = "77777777-7777-4777-8777-777777777777"
	fixtureRevoke     = "88888888-8888-4888-8888-888888888888"
)

type stateRecordOwnerFixture struct {
	store      *records.Store
	auth       records.StateAuthorizer
	ref        model.SourceRef
	provenance string
}

func (f *stateRecordOwnerFixture) ObserveRecords(ctx context.Context, _ *model.ScopeRequest) (app.RecordSnapshot, error) {
	durable, err := f.store.ReadState(ctx, 10, f.auth)
	if err != nil {
		return app.RecordSnapshot{}, err
	}
	scope := model.ScopeVector{Kind: "single", CatalogIncarnation: fixtureCatalog,
		Members: []model.ScopeMember{{WorktreeID: fixtureWorktree, ProfileID: f.ref.ProfileID,
			GenerationID: fixtureGeneration, SnapshotDigest: digest([]byte("capture manifest")),
			PolicyDigest: digest([]byte("policy")), PolicyEpoch: 1, BindingRevision: 1,
			RecordEpoch: durable.RecordEpoch, RecordsIncarnation: durable.Identity.Incarnation, IndexIncarnation: fixtureIndex}},
		UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	return app.RecordSnapshot{Digest: scope.VectorDigest, Scope: &scope, Incarnation: durable.Identity.Incarnation, Epoch: durable.RecordEpoch}, err
}

func (f *stateRecordOwnerFixture) ReadRecord(ctx context.Context, stamp app.RecordSnapshot, id string, version int64) (ports.StoredRecord, error) {
	read, err := f.store.ReadVersion(ctx, id, version, f.auth)
	if err != nil {
		return ports.StoredRecord{}, err
	}
	if read.State.Identity.Incarnation != stamp.Incarnation || read.State.RecordEpoch != stamp.Epoch {
		return ports.StoredRecord{}, app.ErrRecordsChanged
	}
	return read.Record, nil
}

func (f *stateRecordOwnerFixture) ReadRecordHeads(context.Context, app.RecordSnapshot, string, string, int) ([]ports.StoredRecord, bool, error) {
	return nil, false, errors.New("only exact reads are supported by the fixture owner")
}

func (f *stateRecordOwnerFixture) RecordProvenance(ctx context.Context, _ app.RecordSnapshot, record ports.StoredRecord) (string, error) {
	if len(record.SourceRefs) != 1 || record.SourceRefs[0] != f.ref {
		return "unverified", ctx.Err()
	}
	return f.provenance, ctx.Err()
}

func TestStateModelApprovedHeadUsesCapturedBytesAndCAS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../domain/state/testdata/order.machine.json")
	check(err)
	ref := model.SourceRef{WorktreeID: fixtureWorktree, ProfileID: digest([]byte("state profile")),
		GenerationID: fixtureGeneration, Path: "order.machine.json", ContentHash: digest(raw),
		Range: model.ByteRange{EndByte: int64(len(raw))}}
	dir := t.TempDir()
	root, err := paths.OpenRoot(dir)
	check(err)
	defer root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, root, fixtureWorktree, ports.ExclusiveLease, time.Second)
	check(err)
	defer lease.Close()
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(dir, "records.sqlite")}).String()
	db, err := sql.Open("sqlite", dsn)
	check(err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	writer, err := runtime.AcquireWriter(ctx, lease, fixturePropose, time.Second)
	check(err)
	id, initErr := records.InitializeWithWriter(ctx, db, fixtureWorktree, lease, writer, fixturePropose)
	check(errors.Join(initErr, writer.Close()))
	store, err := records.Open(ctx, db, fixtureWorktree, lease)
	check(err)
	auth := func(ctx context.Context, observed records.Identity) error {
		if id != observed {
			return records.ErrStoreChanged
		}
		return ctx.Err()
	}
	owner := &stateRecordOwnerFixture{store: store, auth: auth, ref: ref, provenance: "retained_snapshot"}
	service := &app.RecordService{Owner: owner}
	read := func(ctx context.Context, observed model.SourceRef, limit int) ([]byte, error) {
		if observed != ref || len(raw) > limit {
			return nil, ErrStateModelApproval
		}
		return raw, ctx.Err()
	}
	mutate := func(action, mutationID, recordID string, version int64, source *model.SourceRef, reader StateSourceReader) (records.MutationResult, error) {
		guard, err := runtime.AcquireWriter(ctx, lease, mutationID, time.Second)
		if err != nil {
			return records.MutationResult{}, err
		}
		result, resultErr := store.MutateStateModel(ctx, records.StateModelMutationRequest{Source: source, ReadSource: reader,
			MutationRequest: records.MutationRequest{RecordID: recordID, ExpectedVersion: version, Action: action,
				Reason: "local fixture owner decision", Authorize: auth, Writer: guard, JobID: mutationID,
				Mutation: model.MutationIdentity{MutationID: mutationID, PrincipalID: "fixture-maintainer", RequestHash: digest([]byte(action + recordID))}}})
		return result, errors.Join(resultErr, guard.Close())
	}
	proposed, err := mutate("propose", fixturePropose, "", 0, &ref, read)
	check(err)
	if proposed.Record.Version != 1 || proposed.State.RecordEpoch != 1 || proposed.Record.DeclaredStatus != "proposed" {
		t.Fatalf("proposal: %+v", proposed)
	}
	request := model.RecordReadRequest{RecordID: proposed.Record.RecordID}
	pinned, err := owner.ObserveRecords(ctx, nil)
	check(err)
	deniedRead := func(context.Context, model.SourceRef, int) ([]byte, error) {
		t.Error("source was materialized without a current approval")
		return nil, ErrStateModelApproval
	}
	if _, err := ResolveStateModel(ctx, service, deniedRead, request, *pinned.Scope); !errors.Is(err, ErrStateModelApproval) {
		t.Fatalf("proposed record was accepted: %v", err)
	}
	evidenceReads := 0
	denied := errors.New("fixture source changed at commit")
	lateDeny := func(ctx context.Context, observed model.SourceRef, limit int) ([]byte, error) {
		evidenceReads++
		if evidenceReads == 2 {
			return nil, denied
		}
		return read(ctx, observed, limit)
	}
	if _, err := mutate("approve", fixtureDenied, request.RecordID, 1, nil, lateDeny); !errors.Is(err, denied) {
		t.Fatalf("late source change did not roll back: %v", err)
	}
	stillProposed, err := owner.ObserveRecords(ctx, nil)
	check(err)
	if stillProposed.Epoch != 1 {
		t.Fatalf("failed transaction incremented record epoch: %d", stillProposed.Epoch)
	}
	approved, err := mutate("approve", fixtureApprove, request.RecordID, 1, nil, read)
	check(err)
	replay, err := mutate("approve", fixtureApprove, request.RecordID, 1, nil, read)
	check(err)
	if approved.Record.Version != 2 || !replay.Replayed || replay.State.RecordEpoch != 2 || !replay.Record.StateModelSource {
		t.Fatalf("approval replay: %+v %+v", approved, replay)
	}
	current, err := owner.ObserveRecords(ctx, nil)
	check(err)
	if _, err := ResolveStateModel(ctx, service, deniedRead, request, *pinned.Scope); !errors.Is(err, ErrStateModelApproval) {
		t.Fatalf("old scope was accepted: %v", err)
	}
	view, err := ResolveStateModel(ctx, service, read, request, *current.Scope)
	check(err)
	if view.RecordVersion != 2 || view.Source != ref || view.Definition.MachineID != "order-flow" || view.Reachability != "unknown" {
		t.Fatalf("approved declaration: %+v", view)
	}
	owner.provenance = "source_changed"
	if _, err := ResolveStateModel(ctx, service, deniedRead, request, *current.Scope); !errors.Is(err, ErrStateModelApproval) {
		t.Fatalf("stale declaration was accepted: %v", err)
	}
	owner.provenance = "retained_snapshot"
	if _, err := ResolveStateModel(ctx, service, func(ctx context.Context, observed model.SourceRef, limit int) ([]byte, error) {
		_, err := mutate("revoke", fixtureRevoke, request.RecordID, 2, nil, read)
		if err != nil {
			return nil, err
		}
		return read(ctx, observed, limit)
	}, request, *current.Scope); !errors.Is(err, ErrStateModelApproval) {
		t.Fatalf("approval changed during source read: %v", err)
	}
	t.Log("C062: typed declaration and SQLite proposal/approval/replay, stale source and scope revocation withheld")
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
