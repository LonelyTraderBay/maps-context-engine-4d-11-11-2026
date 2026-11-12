package model

import (
	"encoding/json"
	"errors"
	"fmt"
)

// GraphRef is the closed GraphRef union from S43. It does not grant access.
type GraphRef interface{ graphIdentity() (GraphIdentity, error) }
type GraphIdentity struct {
	WorktreeID   string `json:"worktree_id"`
	ProfileID    string `json:"profile_id"`
	GenerationID string `json:"generation_id"`
}
type SymbolRef struct {
	Kind string `json:"kind"`
	GraphIdentity
	SymbolID string `json:"symbol_id"`
}
type EntityRef struct {
	Kind string `json:"kind"`
	GraphIdentity
	EntityID string `json:"entity_id"`
}
type ModuleRef struct {
	Kind string `json:"kind"`
	GraphIdentity
	ModuleID string `json:"module_id"`
}
type AnchorRef struct {
	Kind string `json:"kind"`
	GraphIdentity
	BuildMapID string `json:"build_map_id"`
	AnchorID   string `json:"anchor_id"`
	BindingID  string `json:"binding_id"`
}
type FileRef struct {
	Kind string `json:"kind"`
	GraphIdentity
	Path        string  `json:"path"`
	ContentHash *string `json:"content_hash"`
}

func validGraphIdentity(id GraphIdentity) bool {
	return uuid(id.WorktreeID) && lowerHex(id.ProfileID, 64) && uuid(id.GenerationID)
}
func (r *SymbolRef) graphIdentity() (GraphIdentity, error) {
	if r == nil || r.Kind != "symbol" || !validGraphIdentity(r.GraphIdentity) || !validID(r.SymbolID) {
		return GraphIdentity{}, fmt.Errorf("invalid symbol ref")
	}
	return r.GraphIdentity, nil
}
func (r *EntityRef) graphIdentity() (GraphIdentity, error) {
	if r == nil || r.Kind != "entity" || !validGraphIdentity(r.GraphIdentity) || !validID(r.EntityID) {
		return GraphIdentity{}, fmt.Errorf("invalid entity ref")
	}
	return r.GraphIdentity, nil
}
func (r *ModuleRef) graphIdentity() (GraphIdentity, error) {
	if r == nil || r.Kind != "module" || !validGraphIdentity(r.GraphIdentity) || !validID(r.ModuleID) {
		return GraphIdentity{}, fmt.Errorf("invalid module ref")
	}
	return r.GraphIdentity, nil
}
func (r *AnchorRef) graphIdentity() (GraphIdentity, error) {
	if r == nil || r.Kind != "source_anchor" || !validGraphIdentity(r.GraphIdentity) || !lowerHex(r.BuildMapID, 64) || !lowerHex(r.AnchorID, 64) || !lowerHex(r.BindingID, 64) {
		return GraphIdentity{}, fmt.Errorf("invalid anchor ref")
	}
	return r.GraphIdentity, nil
}
func (r *FileRef) graphIdentity() (GraphIdentity, error) {
	if r == nil || r.Kind != "file" || !validGraphIdentity(r.GraphIdentity) || !validSourcePath(r.Path) || (r.ContentHash != nil && !lowerHex(*r.ContentHash, 64)) {
		return GraphIdentity{}, fmt.Errorf("invalid file ref")
	}
	return r.GraphIdentity, nil
}

func DecodeGraphRefJSON(data []byte, maxBytes int) (GraphRef, error) {
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return nil, err
	}
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	fields := []string{"kind", "worktree_id", "profile_id", "generation_id"}
	nullable := map[string]bool{}
	var result GraphRef
	switch head.Kind {
	case "symbol":
		fields = append(fields, "symbol_id")
		result = &SymbolRef{}
	case "entity":
		fields = append(fields, "entity_id")
		result = &EntityRef{}
	case "module":
		fields = append(fields, "module_id")
		result = &ModuleRef{}
	case "source_anchor":
		fields = append(fields, "build_map_id", "anchor_id", "binding_id")
		result = &AnchorRef{}
	case "file":
		fields = append(fields, "path", "content_hash")
		nullable["content_hash"] = true
		result = &FileRef{}
	default:
		return nil, fmt.Errorf("unknown graph ref kind")
	}
	if _, err := scopeObject(data, fields, nullable); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, err
	}
	if _, err := result.graphIdentity(); err != nil {
		return nil, err
	}
	return result, nil
}

var ErrAmbiguousMember = errors.New("ambiguous scope member")
var ErrMemberNotInScope = errors.New("reference not in scope")

// MatchPinnedMember only matches a reference to one member of the supplied
// vector. The caller must independently verify digest, authorization, stores,
// policy and current epochs before using the result.
func MatchPinnedMember(scope ScopeVector, ref GraphRef, serviceID *string) (ScopeMember, error) {
	if err := scope.validateShape(); err != nil {
		return ScopeMember{}, err
	}
	if ref == nil || !optionalID(serviceID) {
		return ScopeMember{}, fmt.Errorf("invalid selector")
	}
	id, err := ref.graphIdentity()
	if err != nil {
		return ScopeMember{}, err
	}
	count := 0
	var found ScopeMember
	for _, member := range scope.Members {
		if member.WorktreeID != id.WorktreeID || member.ProfileID != id.ProfileID || member.GenerationID != id.GenerationID {
			continue
		}
		if serviceID != nil && (member.ServiceID == nil || *member.ServiceID != *serviceID) {
			continue
		}
		count++
		found = member
	}
	if count == 0 {
		return ScopeMember{}, ErrMemberNotInScope
	}
	if count > 1 {
		return ScopeMember{}, ErrAmbiguousMember
	}
	return found, nil
}
