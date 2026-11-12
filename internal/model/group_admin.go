package model

import (
	"encoding/json"
	"fmt"
)

type ServiceNamespace struct {
	Environment     string  `json:"environment"`
	Protocol        string  `json:"protocol"`
	BaseOrigin      *string `json:"base_origin"`
	RoutePrefix     *string `json:"route_prefix"`
	ContractVersion *string `json:"contract_version"`
}
type GroupMemberConfig struct {
	ServiceID  string           `json:"service_id"`
	WorktreeID string           `json:"worktree_id"`
	ProfileID  string           `json:"profile_id"`
	Namespace  ServiceNamespace `json:"namespace"`
}
type GroupConfig struct {
	GroupID string              `json:"group_id"`
	Name    string              `json:"name"`
	Members []GroupMemberConfig `json:"members"`
}
type GroupAdminRequest struct {
	Action           string       `json:"action"`
	GroupID          *string      `json:"group_id,omitempty"`
	ExpectedRevision *int64       `json:"expected_revision,omitempty"`
	Config           *GroupConfig `json:"config,omitempty"`
	Limit            *int64       `json:"limit,omitempty"`
	Cursor           *string      `json:"cursor,omitempty"`
}

func DecodeGroupConfigJSON(data []byte, maxBytes int) (GroupConfig, error) {
	var g GroupConfig
	if _, err := DecodeIdentityJSON(data, maxBytes); err != nil {
		return g, err
	}
	fields, err := scopeObject(data, []string{"group_id", "name", "members"}, nil)
	if err != nil {
		return g, err
	}
	var members []json.RawMessage
	if err := json.Unmarshal(fields["members"], &members); err != nil {
		return g, err
	}
	if len(members) < 1 || len(members) > 32 {
		return g, fmt.Errorf("invalid group member count")
	}
	for _, m := range members {
		f, err := scopeObject(m, []string{"service_id", "worktree_id", "profile_id", "namespace"}, nil)
		if err != nil {
			return g, err
		}
		if _, err := scopeObject(f["namespace"], []string{"environment", "protocol", "base_origin", "route_prefix", "contract_version"}, map[string]bool{"base_origin": true, "route_prefix": true, "contract_version": true}); err != nil {
			return g, err
		}
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return GroupConfig{}, err
	}
	if !uuid(g.GroupID) || !boundedString(g.Name, 256) || g.Members == nil {
		return GroupConfig{}, fmt.Errorf("invalid group")
	}
	for _, m := range g.Members {
		n := m.Namespace
		if !validID(m.ServiceID) || !uuid(m.WorktreeID) || !lowerHex(m.ProfileID, 64) || !validID(n.Environment) || (n.BaseOrigin != nil && !boundedString(*n.BaseOrigin, 2048)) || (n.RoutePrefix != nil && !boundedString(*n.RoutePrefix, 1024)) || !optionalID(n.ContractVersion) {
			return GroupConfig{}, fmt.Errorf("invalid group member")
		}
		switch n.Protocol {
		case "http", "websocket", "event", "sql":
		default:
			return GroupConfig{}, fmt.Errorf("invalid namespace protocol")
		}
	}
	return g, nil
}

// DecodeGroupAdminRequestJSON checks intent and CAS fields. It does not grant
// group access, validate remote origins, dispatch operations or mutate a catalog.
func DecodeGroupAdminRequestJSON(data []byte, maxBytes int) (GroupAdminRequest, error) {
	var r GroupAdminRequest
	f, err := optionalObject(data, maxBytes, "action", "group_id", "expected_revision", "config", "limit", "cursor")
	if err != nil {
		return r, err
	}
	if _, ok := f["action"]; !ok {
		return r, fmt.Errorf("missing group action")
	}
	if raw, ok := f["config"]; ok {
		config, err := DecodeGroupConfigJSON(raw, maxBytes)
		if err != nil {
			return r, err
		}
		r.Config = &config
		delete(f, "config")
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return r, err
	}
	var body GroupAdminRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return GroupAdminRequest{}, err
	}
	body.Config = r.Config
	if body.GroupID != nil && !uuid(*body.GroupID) || !optionalRange(body.ExpectedRevision, 0, maxExactInteger) || !optionalRange(body.Limit, 1, 100) || body.Cursor != nil && !validToken(*body.Cursor) {
		return GroupAdminRequest{}, fmt.Errorf("invalid group request")
	}
	switch body.Action {
	case "get":
		if body.GroupID == nil || body.Config != nil || body.ExpectedRevision != nil {
			return GroupAdminRequest{}, fmt.Errorf("invalid get")
		}
	case "list":
		if body.GroupID != nil || body.Config != nil || body.ExpectedRevision != nil {
			return GroupAdminRequest{}, fmt.Errorf("invalid list")
		}
		if body.Limit == nil {
			v := int64(20)
			body.Limit = &v
		}
	case "put":
		if body.Config == nil || body.ExpectedRevision == nil || body.GroupID != nil && *body.GroupID != body.Config.GroupID {
			return GroupAdminRequest{}, fmt.Errorf("invalid put")
		}
	case "remove":
		if body.GroupID == nil || body.ExpectedRevision == nil || body.Config != nil {
			return GroupAdminRequest{}, fmt.Errorf("invalid remove")
		}
	default:
		return GroupAdminRequest{}, fmt.Errorf("invalid group action")
	}
	return body, nil
}
