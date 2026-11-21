package compose

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestT097FlowResponseKeepsVisibleEndpointsUnderBudget(t *testing.T) {
	member := model.ScopeMember{WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64),
		GenerationID: "22222222-2222-4222-8222-222222222222", SnapshotDigest: strings.Repeat("b", 64), PolicyDigest: strings.Repeat("c", 64),
		PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: "33333333-3333-4333-8333-333333333333",
		RecordsIncarnation: "44444444-4444-4444-8444-444444444444"}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member},
		CatalogIncarnation: "55555555-5555-4555-8555-555555555555", UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 32768)
	if err != nil {
		t.Fatal(err)
	}
	root := &model.SymbolRef{Kind: "symbol", SymbolID: "fixture-route",
		GraphIdentity: model.GraphIdentity{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID}}
	data := model.FlowData{Operation: "flow_query", Mode: "static", ProjectionID: strings.Repeat("d", 64),
		RootRef: root, Steps: []model.FlowStep{}, Edges: []model.FlowEdge{}}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("step-%d", i)
		ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
			Path: "fixture.go", ContentHash: strings.Repeat("e", 64), Range: model.ByteRange{StartByte: int64(i), EndByte: int64(i + 1)}}
		data.Steps = append(data.Steps, model.FlowStep{StepID: id, CanonicalRefs: []model.GraphRef{root}, SourceRefs: []model.SourceRef{ref},
			StepKind: "direct_call", OrderSemantics: "source_sequence", PossibleOrObserved: "statically_possible",
			OutcomeStatus: "statically_possible", EvidenceRefs: []model.EvidenceRef{}, Limitations: []model.Gap{}})
		if i != 0 {
			data.Edges = append(data.Edges, model.FlowEdge{EdgeID: fmt.Sprintf("edge-%d", i),
				FromStepID: fmt.Sprintf("step-%d", i-1), ToStepID: id, Kind: "call", EvidenceRefs: []model.EvidenceRef{}})
		}
	}
	options := app.CoreCallOptions{RequestID: "66666666-6666-4666-8666-666666666666", DeliveryMode: "http", MaxOutputBytes: 5000}
	blocks, err := encodeFlowResponse(baseResponse(options, scope), flowview.Result{Data: data})
	if err != nil || len(blocks) != 1 || len(blocks[0]) > int(options.MaxOutputBytes) {
		t.Fatalf("bounded response: %v", err)
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	response, err := registry.DecodeResponse(contracts.HTTP, "POST /flow/query", blocks[0], int(options.MaxOutputBytes), 1)
	if err != nil {
		t.Fatal(err)
	}
	page := response.Data.(model.FlowData)
	if response.Status != "partial" || !response.Pagination.Truncated || response.Pagination.EnumerationComplete ||
		len(page.Steps) == 0 || len(page.Steps) >= len(data.Steps) || len(page.Edges) != len(page.Steps)-1 ||
		page.ProjectionID != data.ProjectionID || response.Pagination.RequiredPending == nil ||
		*response.Pagination.RequiredPending != int64(len(data.Steps)-len(page.Steps)) {
		t.Fatalf("invalid bounded flow: %+v, %+v", page, response.Pagination)
	}
}
