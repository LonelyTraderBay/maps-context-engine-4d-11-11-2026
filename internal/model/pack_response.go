package model

import "fmt"

// ValidatePackResponseSemantics checks the request-dependent snapshot label;
// source freshness and actual edit authorization require live validation.
func ValidatePackResponseSemantics(request PackRequest, response Response) error {
	if response.Data == nil {
		return nil
	}
	pack, ok := response.Data.(PackData)
	if !ok || pack.Operation != "context_pack" {
		return fmt.Errorf("unexpected pack response data")
	}
	if request.FreshnessMode != "analysis_snapshot" && request.FreshnessMode != "checked_workspace" {
		return fmt.Errorf("invalid freshness mode")
	}
	if request.FreshnessMode == "analysis_snapshot" && (response.Readiness == "reviewable" || response.Freshness.Status != "not_checked" || pack.EditPreconditionValidated) {
		return fmt.Errorf("analysis snapshot cannot validate edit preconditions")
	}
	return nil
}
