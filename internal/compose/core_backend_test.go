package compose

import (
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

func TestLocateResponseReportsUncapturedFileMatches(t *testing.T) {
	scope := model.ScopeVector{Members: []model.ScopeMember{{}}}
	response, err := locateResponse(app.CoreCallOptions{RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", MaxOutputBytes: 16384, DeliveryMode: "mcp_dual"}, scope, search.LocatePage{Result: search.Result{Coverage: "partial", Text: []search.TextHit{}}})
	if err != nil || response.Status != "partial" || response.Coverage.Status != "partial" || len(response.Coverage.Gaps) != 1 || response.Coverage.Gaps[0].Code != "UNCAPTURED_FILE_MATCHES" {
		t.Fatalf("uncaptured matches were reported as complete: %+v %v", response, err)
	}
}
