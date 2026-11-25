package model

import (
	"encoding/json"
	"fmt"
)

type RegisteredScopeStatus struct {
	WorktreeID string  `json:"worktree_id"`
	ProfileID  *string `json:"profile_id"`
	RootStatus string  `json:"root_status"`
	IndexState string  `json:"index_state"`
}
type StatusData struct {
	Operation        string                  `json:"operation"`
	Scopes           []ScopeMember           `json:"scopes"`
	Capabilities     []Capability            `json:"capabilities"`
	ProjectStage     string                  `json:"project_stage"`
	IndexState       string                  `json:"index_state"`
	RegisteredScopes []RegisteredScopeStatus `json:"registered_scopes"`
}
type Candidate struct {
	Ref           GraphRef      `json:"ref"`
	Name          string        `json:"name"`
	QualifiedName string        `json:"qualified_name"`
	SourceRefs    []SourceRef   `json:"source_refs"`
	EvidenceRefs  []EvidenceRef `json:"evidence_refs"`
}
type LocateData struct {
	Operation  string           `json:"operation"`
	Candidates []Candidate      `json:"candidates"`
	Excerpts   []LocatedExcerpt `json:"excerpts,omitempty"`
}
type LocatedExcerpt struct {
	SourceRef SourceRef `json:"source_ref"`
	StartLine int64     `json:"start_line"`
	Content   string    `json:"content"`
}
type ValidateData struct {
	Operation                 string      `json:"operation"`
	EditPreconditionValidated bool        `json:"edit_precondition_validated"`
	ChangedSources            []SourceRef `json:"changed_sources"`
	MembershipChanged         bool        `json:"membership_changed"`
	ConfigurationChanged      bool        `json:"configuration_changed"`
	RecordSetChanged          bool        `json:"record_set_changed"`
}

func validIndexState(s string) bool {
	return s == "missing" || s == "published" || s == "building_with_previous" || s == "unavailable"
}
func DecodeRegisteredScopeStatusJSON(data []byte, maxBytes int) (RegisteredScopeStatus, error) {
	var r RegisteredScopeStatus
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	if _, err := scopeObject(data, []string{"worktree_id", "profile_id", "root_status", "index_state"}, map[string]bool{"profile_id": true}); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return RegisteredScopeStatus{}, err
	}
	if !uuid(r.WorktreeID) || r.ProfileID != nil && !lowerHex(*r.ProfileID, 64) || !validIndexState(r.IndexState) || r.RootStatus != "registered" && r.RootStatus != "unavailable" && r.RootStatus != "removed" {
		return RegisteredScopeStatus{}, fmt.Errorf("invalid registered scope")
	}
	return r, nil
}
func DecodeStatusDataJSON(data []byte, maxBytes int) (StatusData, error) {
	var r StatusData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "scopes", "capabilities", "project_stage", "index_state", "registered_scopes"}, nil)
	if err != nil {
		return r, err
	}
	for _, spec := range []struct {
		name  string
		limit int
	}{{"scopes", 32}, {"capabilities", 128}, {"registered_scopes", 32}} {
		var items []json.RawMessage
		if err := json.Unmarshal(f[spec.name], &items); err != nil {
			return r, err
		}
		if items == nil || len(items) > spec.limit {
			return r, fmt.Errorf("invalid status list")
		}
		for _, item := range items {
			switch spec.name {
			case "scopes":
				_, err = DecodeScopeMemberJSON(item, maxBytes)
			case "capabilities":
				_, err = DecodeCapabilityJSON(item, maxBytes)
			case "registered_scopes":
				_, err = DecodeRegisteredScopeStatusJSON(item, maxBytes)
			}
			if err != nil {
				return r, err
			}
		}
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return StatusData{}, err
	}
	if r.Operation != "status" || !validIndexState(r.IndexState) || r.Scopes == nil || r.Capabilities == nil || r.RegisteredScopes == nil {
		return StatusData{}, fmt.Errorf("invalid status data")
	}
	switch r.ProjectStage {
	case "spec_only", "mixed", "implemented_scope", "unknown":
	default:
		return StatusData{}, fmt.Errorf("invalid project stage")
	}
	return r, nil
}
func DecodeCandidateJSON(data []byte, maxBytes int) (Candidate, error) {
	var c Candidate
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return c, err
	}
	f, err := scopeObject(data, []string{"ref", "name", "qualified_name", "source_refs", "evidence_refs"}, nil)
	if err != nil {
		return c, err
	}
	c.Ref, err = DecodeGraphRefJSON(f["ref"], maxBytes)
	if err != nil {
		return c, err
	}
	delete(f, "ref")
	if _, err := decodeSourceRefs(f["source_refs"], 16, maxBytes); err != nil {
		return c, err
	}
	if _, err := decodeEvidenceRefs(f["evidence_refs"], 16, maxBytes); err != nil {
		return c, err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return c, err
	}
	var body Candidate
	if err := json.Unmarshal(raw, &body); err != nil {
		return c, err
	}
	body.Ref = c.Ref
	if !stringAtMost(body.Name, 512) || !stringAtMost(body.QualifiedName, 2048) || body.SourceRefs == nil || body.EvidenceRefs == nil {
		return Candidate{}, fmt.Errorf("invalid candidate")
	}
	return body, nil
}
func DecodeLocateDataJSON(data []byte, maxBytes int) (LocateData, error) {
	var r LocateData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := optionalObject(data, maxBytes, "operation", "candidates", "excerpts")
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(f["operation"], &r.Operation); err != nil {
		return r, err
	}
	if r.Operation != "locate" {
		return r, fmt.Errorf("invalid locate operation")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(f["candidates"], &items); err != nil {
		return r, err
	}
	if items == nil || len(items) > 100 {
		return r, fmt.Errorf("invalid candidates")
	}
	r.Candidates = make([]Candidate, 0, len(items))
	for _, item := range items {
		c, err := DecodeCandidateJSON(item, maxBytes)
		if err != nil {
			return LocateData{}, err
		}
		r.Candidates = append(r.Candidates, c)
	}
	if raw, exists := f["excerpts"]; exists {
		var excerpts []json.RawMessage
		if err := json.Unmarshal(raw, &excerpts); err != nil || len(excerpts) == 0 || len(excerpts) > 32 {
			return LocateData{}, fmt.Errorf("invalid excerpts")
		}
		for _, item := range excerpts {
			fields, err := scopeObject(item, []string{"source_ref", "start_line", "content"}, nil)
			if err != nil {
				return LocateData{}, err
			}
			ref, err := DecodeSourceRefJSON(fields["source_ref"], maxBytes)
			if err != nil {
				return LocateData{}, err
			}
			var x LocatedExcerpt
			if err := json.Unmarshal(item, &x); err != nil || x.StartLine < 1 || x.StartLine > maxExactInteger ||
				len(x.Content) == 0 || len(x.Content) > 4096 || int64(len(x.Content)) != ref.Range.EndByte-ref.Range.StartByte {
				return LocateData{}, fmt.Errorf("invalid located excerpt")
			}
			x.SourceRef = ref
			r.Excerpts = append(r.Excerpts, x)
		}
	}
	return r, nil
}

// DecodeValidateDataJSON rejects a successful precondition claim alongside
// explicitly changed inputs. Actual validation against disk remains external.
func DecodeValidateDataJSON(data []byte, maxBytes int) (ValidateData, error) {
	var r ValidateData
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return r, err
	}
	f, err := scopeObject(data, []string{"operation", "edit_precondition_validated", "changed_sources", "membership_changed", "configuration_changed", "record_set_changed"}, nil)
	if err != nil {
		return r, err
	}
	if _, err := decodeSourceRefs(f["changed_sources"], 256, maxBytes); err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return ValidateData{}, err
	}
	if r.Operation != "validate_pack" || r.ChangedSources == nil || r.EditPreconditionValidated && (len(r.ChangedSources) > 0 || r.MembershipChanged || r.ConfigurationChanged || r.RecordSetChanged) {
		return ValidateData{}, fmt.Errorf("invalid validation result")
	}
	return r, nil
}
