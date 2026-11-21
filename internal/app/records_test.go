package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type recordOwnerFixture struct {
	stamp        RecordSnapshot
	head         []ports.StoredRecord
	provenance   string
	changing     bool
	observations int
}

func (r *recordOwnerFixture) ObserveRecords(ctx context.Context, _ *model.ScopeRequest) (RecordSnapshot, error) {
	r.observations++
	stamp := r.stamp
	if r.changing && r.observations > 1 {
		stamp.Digest = strings.Repeat("e", 64)
	}
	return stamp, ctx.Err()
}
func (r *recordOwnerFixture) ReadRecord(ctx context.Context, _ RecordSnapshot, _ string, _ int64) (ports.StoredRecord, error) {
	return r.head[0], ctx.Err()
}
func (r *recordOwnerFixture) ReadRecordHeads(ctx context.Context, _ RecordSnapshot, after, key string, limit int) ([]ports.StoredRecord, bool, error) {
	index := 0
	for index < len(r.head) && r.head[index].RecordID <= after {
		index++
	}
	end := min(index+limit, len(r.head))
	return r.head[index:end], end < len(r.head), ctx.Err()
}
func (r *recordOwnerFixture) RecordProvenance(ctx context.Context, _ RecordSnapshot, _ ports.StoredRecord) (string, error) {
	return r.provenance, ctx.Err()
}

func TestRecordPagesWithoutAnIndexAndScopeChanges(t *testing.T) {
	const first = "11111111-1111-4111-8111-111111111111"
	const second = "22222222-2222-4222-8222-222222222222"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	owner := &recordOwnerFixture{stamp: RecordSnapshot{Digest: strings.Repeat("a", 64), Incarnation: first, Epoch: 3},
		head: []ports.StoredRecord{
			{RecordID: first, Version: 2, Kind: "ui_mapping", DeclaredStatus: "approved"},
			{RecordID: second, Version: 1, Kind: "decision", DeclaredStatus: "proposed"},
		}, provenance: "snapshot_expired"}
	key, err := contextpack.GenerateCursorKey()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	service := &RecordService{Owner: owner, Cursors: codec}
	limit := int64(1)
	request := model.RecordListRequest{Limit: &limit}
	page, err := service.List(ctx, request)
	if err != nil || page.Scope != nil || page.Cursor == nil || len(page.Data.Records) != 1 ||
		page.Data.Records[0].EffectiveStatus != "needs_review" || len(page.Data.Records[0].SourceRefs) != 0 {
		t.Fatalf("unexpected first page: %+v %v", page, err)
	}
	request.Cursor = page.Cursor
	continuation, err := service.List(ctx, request)
	if err != nil || len(continuation.Data.Records) != 1 || continuation.Data.Records[0].RecordID != second || continuation.Cursor != nil {
		t.Fatalf("unexpected second page: %+v %v", continuation, err)
	}
	filter := "approved"
	request.Status = &filter
	if _, err := service.List(ctx, request); !errors.Is(err, contextpack.ErrCursorExpired) {
		t.Fatalf("cursor crossed filters: %v", err)
	}
	request.Status = nil
	owner.stamp.Epoch++
	owner.stamp.Digest = strings.Repeat("b", 64)
	if _, err := service.List(ctx, request); !errors.Is(err, contextpack.ErrCursorExpired) {
		t.Fatalf("record epoch changed: %v", err)
	}
	owner.observations, owner.changing = 0, true
	if result, err := service.Read(ctx, model.RecordReadRequest{RecordID: first}); !errors.Is(err, ErrRecordsChanged) || result.Data.UIMapping != nil {
		t.Fatalf("exposed output after scope change: %+v %v", result, err)
	}
}
