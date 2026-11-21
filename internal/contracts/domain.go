// Package contracts defines independently sealed domain adapter records.
package contracts

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrDomainRecord = errors.New("invalid domain adapter record")

const maxBatchBytes = 4 << 20

var (
	namespacePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,192}$`)
	currencyPattern  = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Key struct {
	ServiceID   *string `json:"service_id"`
	Environment string  `json:"environment"`
	Protocol    string  `json:"protocol"`
	Namespace   string  `json:"namespace"`
	Identifier  string  `json:"identifier"`
	Version     string  `json:"version"`
}

type Entity struct {
	Ref         model.EntityRef `json:"ref"`
	Kind        string          `json:"kind"`
	Key         Key             `json:"key"`
	Plane       string          `json:"plane"`
	Declaration model.SourceRef `json:"declaration"`
}

type Field struct {
	Entity      model.EntityRef `json:"entity"`
	FieldPath   string          `json:"field_path"`
	Direction   string          `json:"direction"`
	Type        string          `json:"type"`
	Nullable    *bool           `json:"nullable"`
	Unit        *string         `json:"unit"`
	Currency    *string         `json:"currency"`
	Precision   *int64          `json:"precision"`
	Declaration model.SourceRef `json:"declaration"`
}

type Link struct {
	LinkID           string            `json:"link_id"`
	Source           model.GraphRef    `json:"source"`
	Target           model.GraphRef    `json:"target"`
	Kind             string            `json:"kind"`
	Evidence         model.EvidenceRef `json:"evidence"`
	ResolutionStatus string            `json:"resolution_status"`
}

type Batch struct {
	SchemaVersion int      `json:"schema_version"`
	Entities      []Entity `json:"entities"`
	Fields        []Field  `json:"fields"`
	Links         []Link   `json:"links"`
}

// MatchPinnedEntity resolves an exact selection after the caller observes the
// current authority for that same pinned vector, including unavailable members.
func MatchPinnedEntity(pinned, observed model.ScopeVector, selector model.RefSelector) (model.ScopeMember, error) {
	if err := model.ComparePinnedScope(pinned, observed, maxBatchBytes); err != nil {
		return model.ScopeMember{}, err
	}
	if _, ok := selector.Ref.(*model.EntityRef); !ok || selector.Kind != "ref" {
		return model.ScopeMember{}, ErrDomainRecord
	}
	return model.MatchPinnedMember(pinned, selector.Ref, selector.ServiceID)
}

// QualifiedKey includes the complete declaration identity. It can be used
// for lookup within a generation without treating a display label as a link.
func (e Entity) QualifiedKey() (string, error) {
	raw, err := json.Marshal(struct {
		Worktree   string `json:"worktree_id"`
		Profile    string `json:"profile_id"`
		Key        Key    `json:"key"`
		SourceHash string `json:"source_hash"`
	}{e.Ref.WorktreeID, e.Ref.ProfileID, e.Key, e.Declaration.ContentHash})
	if err != nil {
		return "", err
	}
	value, err := model.DecodeIdentityJSON(raw, 8192)
	if err != nil {
		return "", err
	}
	_, digest, err := model.EncodeCanonical("domain-key-v1", value, 8192)
	return digest, err
}

// DecodeBatch validates the adapter record contract. The owner must check
// source bytes, policy, endpoint existence and generation state on stage.
func DecodeBatch(raw []byte) (Batch, error) {
	if _, err := model.DecodeIdentityJSON(raw, maxBatchBytes); err != nil {
		return Batch{}, ErrDomainRecord
	}
	type wire struct {
		SchemaVersion int               `json:"schema_version"`
		Entities      []json.RawMessage `json:"entities"`
		Fields        []json.RawMessage `json:"fields"`
		Links         []json.RawMessage `json:"links"`
	}
	var input wire
	if _, err := fields(raw, &input, "schema_version", "entities", "fields", "links"); err != nil ||
		input.SchemaVersion != 1 || input.Entities == nil || input.Fields == nil || input.Links == nil ||
		len(input.Entities) > 512 || len(input.Fields) > 2048 || len(input.Links) > 4096 {
		return Batch{}, ErrDomainRecord
	}
	out := Batch{SchemaVersion: 1, Entities: make([]Entity, 0, len(input.Entities)),
		Fields: make([]Field, 0, len(input.Fields)), Links: make([]Link, 0, len(input.Links))}
	for _, raw := range input.Entities {
		item, err := decodeEntity(raw)
		if err != nil {
			return Batch{}, err
		}
		out.Entities = append(out.Entities, item)
	}
	for _, raw := range input.Fields {
		item, err := decodeField(raw)
		if err != nil {
			return Batch{}, err
		}
		out.Fields = append(out.Fields, item)
	}
	for _, raw := range input.Links {
		item, err := decodeLink(raw)
		if err != nil {
			return Batch{}, err
		}
		out.Links = append(out.Links, item)
	}
	return out, nil
}

func fields(raw []byte, output any, names ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != len(names) {
		return nil, ErrDomainRecord
	}
	for _, name := range names {
		if _, ok := object[name]; !ok {
			return nil, ErrDomainRecord
		}
	}
	if json.Unmarshal(raw, output) != nil {
		return nil, ErrDomainRecord
	}
	return object, nil
}

func text(value string, max int) bool {
	return value != "" && utf8.RuneCountInString(value) <= max && strings.IndexFunc(value, unicode.IsControl) == -1
}

func identity(ref model.SourceRef) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}
}

func validDeclaration(fields map[string]json.RawMessage, name string, owner model.GraphIdentity) bool {
	source, err := model.DecodeSourceRefJSON(fields[name], maxBatchBytes)
	return err == nil && identity(source) == owner
}

func decodeEntity(raw []byte) (Entity, error) {
	var item Entity
	obj, err := fields(raw, &item, "ref", "kind", "key", "plane", "declaration")
	if err != nil {
		return Entity{}, err
	}
	if _, err = model.DecodeGraphRefJSON(obj["ref"], maxBatchBytes); err != nil || item.Ref.Kind != "entity" ||
		!validDeclaration(obj, "declaration", item.Ref.GraphIdentity) || !text(item.Kind, 192) ||
		(item.Plane != "intended" && item.Plane != "observed_static") {
		return Entity{}, ErrDomainRecord
	}
	key := item.Key
	if _, err = fields(obj["key"], &item.Key, "service_id", "environment", "protocol", "namespace", "identifier", "version"); err != nil ||
		(key.ServiceID != nil && !text(*key.ServiceID, 192)) || !text(key.Environment, 128) ||
		!namespacePattern.MatchString(key.Namespace) || !text(key.Identifier, 1024) || !text(key.Version, 128) {
		return Entity{}, ErrDomainRecord
	}
	switch key.Protocol {
	case "frontend", "http", "json_schema", "protobuf", "mysql", "asyncapi", "event", "cache", "state":
		return item, nil
	default:
		return Entity{}, ErrDomainRecord
	}
}

func decodeField(raw []byte) (Field, error) {
	var item Field
	obj, err := fields(raw, &item, "entity", "field_path", "direction", "type", "nullable", "unit", "currency", "precision", "declaration")
	if err != nil {
		return Field{}, err
	}
	if _, err = model.DecodeGraphRefJSON(obj["entity"], maxBatchBytes); err != nil || item.Entity.Kind != "entity" ||
		!validDeclaration(obj, "declaration", item.Entity.GraphIdentity) || !text(item.FieldPath, 1024) ||
		!text(item.Type, 128) || item.Unit != nil && !text(*item.Unit, 64) ||
		item.Currency != nil && !currencyPattern.MatchString(*item.Currency) ||
		item.Precision != nil && *item.Precision < 0 {
		return Field{}, ErrDomainRecord
	}
	switch item.Direction {
	case "input", "output", "bidirectional", "stored":
		return item, nil
	default:
		return Field{}, ErrDomainRecord
	}
}

func endpoint(raw []byte) (model.GraphRef, model.GraphIdentity, error) {
	ref, err := model.DecodeGraphRefJSON(raw, maxBatchBytes)
	if err != nil {
		return nil, model.GraphIdentity{}, ErrDomainRecord
	}
	switch value := ref.(type) {
	case *model.EntityRef:
		return value, value.GraphIdentity, nil
	case *model.SymbolRef:
		return value, value.GraphIdentity, nil
	default:
		return nil, model.GraphIdentity{}, ErrDomainRecord
	}
}

func decodeLink(raw []byte) (Link, error) {
	var wire struct {
		LinkID           string          `json:"link_id"`
		Source           json.RawMessage `json:"source"`
		Target           json.RawMessage `json:"target"`
		Kind             string          `json:"kind"`
		Evidence         json.RawMessage `json:"evidence"`
		ResolutionStatus string          `json:"resolution_status"`
	}
	if _, err := fields(raw, &wire, "link_id", "source", "target", "kind", "evidence", "resolution_status"); err != nil ||
		!text(wire.LinkID, 192) || !text(wire.Kind, 192) {
		return Link{}, ErrDomainRecord
	}
	source, from, err := endpoint(wire.Source)
	if err != nil {
		return Link{}, err
	}
	target, to, err := endpoint(wire.Target)
	if err != nil || from != to {
		return Link{}, ErrDomainRecord
	}
	evidence, err := model.DecodeEvidenceRefJSON(wire.Evidence, maxBatchBytes)
	if err != nil || evidence.Origin != "static_analyzer" || evidence.RecordRef != nil || evidence.ArtifactID != nil ||
		len(evidence.SourceRefs) == 0 {
		return Link{}, ErrDomainRecord
	}
	for _, ref := range evidence.SourceRefs {
		if identity(ref) != from {
			return Link{}, ErrDomainRecord
		}
	}
	switch wire.ResolutionStatus {
	case "exact_in_supported_scope":
		if evidence.Kind != "syntax_exact" && evidence.Kind != "type_resolved" && evidence.Kind != "explicit_document" {
			return Link{}, ErrDomainRecord
		}
	case "candidate":
		if evidence.Kind != "type_possible" && evidence.Kind != "text_match" {
			return Link{}, ErrDomainRecord
		}
	case "unresolved":
		if evidence.Kind != "unresolved" {
			return Link{}, ErrDomainRecord
		}
	default:
		return Link{}, ErrDomainRecord
	}
	return Link{LinkID: wire.LinkID, Source: source, Target: target, Kind: wire.Kind, Evidence: evidence, ResolutionStatus: wire.ResolutionStatus}, nil
}
