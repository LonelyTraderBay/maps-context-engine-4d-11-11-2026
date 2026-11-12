package model

import "testing"

func TestAnalysisSnapshotCannotClaimEditReadiness(t *testing.T) {
	req := PackRequest{FreshnessMode: "analysis_snapshot"}
	r := Response{Readiness: "needs_context", Freshness: Freshness{Status: "not_checked"}, Data: PackData{Operation: "context_pack"}}
	if err := ValidatePackResponseSemantics(req, r); err != nil {
		t.Fatal(err)
	}
	bad := r
	bad.Readiness = "reviewable"
	if err := ValidatePackResponseSemantics(req, bad); err == nil {
		t.Fatal("accepted reviewable snapshot")
	}
	bad = r
	bad.Freshness.Status = "observed_current"
	if err := ValidatePackResponseSemantics(req, bad); err == nil {
		t.Fatal("accepted live freshness claim")
	}
	bad = r
	bad.Data = PackData{Operation: "context_pack", EditPreconditionValidated: true}
	if err := ValidatePackResponseSemantics(req, bad); err == nil {
		t.Fatal("accepted edit precondition claim")
	}
}
