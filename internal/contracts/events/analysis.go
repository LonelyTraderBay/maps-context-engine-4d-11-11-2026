// Package events extracts static event and cache contracts from captured files.
package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrProfile = errors.New("invalid event analysis profile")
var plainName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type Capture struct {
	Ref     model.SourceRef
	Bytes   []byte
	Kind    string // asyncapi or redis_cache
	Version string
}

type CodeCapture struct {
	Ref   model.SourceRef
	Bytes []byte
}

// Broker is the exact static server declared for the channel. Statement
// of a server in a contract grants no access to that server.
type Broker struct {
	Protocol string
	Host     string
	Path     string
	Version  string
}

type Message struct {
	Entity        contracts.Entity
	Codec         string
	SchemaVersion string
}

// Delivery controls are declarations with an original capture and document
// pointer. They do not encode delivery guarantees or observed effects.
type Delivery struct {
	Ack           *bool
	RetryLimit    *int
	Replay        *bool
	DeadLetter    string
	Deduplication string
	Outbox        string
	Inbox         string
	Declaration   model.SourceRef
	Pointer       string
}

type Operation struct {
	Entity   contracts.Entity
	Broker   Broker
	Address  string
	Action   string // send or receive from this application's perspective
	Messages []Message
	Delivery Delivery
	Bindable bool
}

type CacheEntry struct {
	Entity     contracts.Entity
	BackendID  string
	Database   int
	Key        string
	Type       string
	TTLSeconds *int
}

// Binding is an attestation from the source provider. The owner verifies
// static site semantics and symbol membership before calling this adapter.
type Binding struct {
	Operation                             model.EntityRef
	Message                               model.EntityRef
	Symbol                                model.SymbolRef
	Observation                           model.SourceRef
	Broker                                Broker
	Address, Action, Codec, SchemaVersion string
}

type Analysis struct {
	Batch      contracts.Batch
	Coverage   model.Coverage
	Operations []Operation
	Cache      []CacheEntry
}

type analyzer struct {
	profile schema.Profile
	ctx     context.Context
	out     Analysis
}

func AnalyzeCaptured(ctx context.Context, profile schema.Profile, input []Capture, code []CodeCapture, bindings []Binding) (Analysis, error) {
	if ctx == nil || len(input) == 0 {
		return Analysis{}, schema.ErrCapture
	}
	if len(input) > 32 || len(code) > 32 || len(bindings) > 512 {
		return Analysis{}, schema.ErrLimit
	}
	if !plainName.MatchString(profile.ServiceID) || !plainName.MatchString(profile.Environment) || !plainName.MatchString(profile.Namespace) {
		return Analysis{}, ErrProfile
	}
	a := analyzer{profile: profile, ctx: ctx, out: Analysis{
		Batch: contracts.Batch{SchemaVersion: 1, Entities: []contracts.Entity{}, Fields: []contracts.Field{}, Links: []contracts.Link{}},
		Coverage: model.Coverage{Status: "supported_scope_complete", Capabilities: []model.Capability{}, Gaps: []model.Gap{},
			ScopeDescription: "Static event and cache contracts in explicitly supplied captures."},
		Operations: []Operation{}, Cache: []CacheEntry{},
	}}
	files := slices.Clone(input)
	slices.SortFunc(files, func(l, r Capture) int { return strings.Compare(l.Ref.Path, r.Ref.Path) })
	inScope := scope(files[0].Ref)
	total := 0
	seen := make(map[string]bool)
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return Analysis{}, err
		}
		total += len(f.Bytes)
		if total > 4<<20 {
			return Analysis{}, schema.ErrLimit
		}
		if seen[f.Ref.Path] || scope(f.Ref) != inScope || !plainName.MatchString(f.Version) {
			return Analysis{}, schema.ErrCapture
		}
		if err := schema.ValidateCapture(f.Ref, f.Bytes); err != nil {
			return Analysis{}, err
		}
		seen[f.Ref.Path] = true
		if _, err := model.DecodeIdentityJSON(f.Bytes, 1<<20); err != nil {
			if err := a.gap("CONTRACT_EVENT_JSON_UNSUPPORTED", f.Ref); err != nil {
				return Analysis{}, err
			}
			continue
		}
		var err error
		switch f.Kind {
		case "asyncapi":
			err = a.asyncAPI(f)
		case "redis_cache":
			err = a.cache(f)
		default:
			return Analysis{}, schema.ErrCapture
		}
		if err != nil {
			return Analysis{}, err
		}
	}
	if err := a.bind(inScope, code, bindings); err != nil {
		return Analysis{}, err
	}
	if len(a.out.Coverage.Gaps) != 0 {
		a.out.Coverage.Status = "partial"
	}
	a.out.Coverage.Capabilities = append(a.out.Coverage.Capabilities, model.Capability{Name: "captured_event_and_cache_contracts",
		ProviderID: "mce4d.events", ProviderVersion: "1", Status: a.out.Coverage.Status})
	if err := schema.ValidateBatch(a.out.Batch); err != nil {
		return Analysis{}, err
	}
	return a.out, ctx.Err()
}

func (a *analyzer) gap(code string, ref model.SourceRef) error {
	if len(a.out.Coverage.Gaps) >= 512 {
		return schema.ErrLimit
	}
	a.out.Coverage.Gaps = append(a.out.Coverage.Gaps, model.Gap{Code: code,
		Reason:     "The captured event or cache contract requires unavailable or unsupported evidence.",
		SourceRefs: []model.SourceRef{ref}})
	return nil
}

func (a *analyzer) entity(f Capture, protocol, kind, identifier, pointer string) (contracts.Entity, error) {
	if err := a.ctx.Err(); err != nil {
		return contracts.Entity{}, err
	}
	if len(a.out.Batch.Entities) >= 512 || len(identifier) > 1024 {
		return contracts.Entity{}, schema.ErrLimit
	}
	service := a.profile.ServiceID
	item := contracts.Entity{Ref: model.EntityRef{Kind: "entity", GraphIdentity: scope(f.Ref)},
		Declaration: f.Ref, Plane: "intended", Kind: kind,
		Key: contracts.Key{ServiceID: &service, Environment: a.profile.Environment,
			Protocol: protocol, Namespace: a.profile.Namespace, Identifier: identifier, Version: f.Version}}
	key, err := item.QualifiedKey()
	if err != nil {
		return contracts.Entity{}, err
	}
	item.Ref.EntityID = digest("event-contract-entity-v1", key, f.Ref.Path, pointer)
	a.out.Batch.Entities = append(a.out.Batch.Entities, item)
	return item, nil
}

func (a *analyzer) link(from model.EntityRef, to model.GraphRef, key, kind, evidenceKind string, sites ...model.SourceRef) error {
	if len(a.out.Batch.Links) >= 4096 {
		return schema.ErrLimit
	}
	id := digest("event-contract-link-v1", from.EntityID, key, kind)
	a.out.Batch.Links = append(a.out.Batch.Links, contracts.Link{LinkID: id, Source: &from, Target: to, Kind: kind,
		ResolutionStatus: "exact_in_supported_scope", Evidence: model.EvidenceRef{EvidenceID: id,
			Origin: "static_analyzer", Kind: evidenceKind, SourceRefs: sites}})
	return nil
}

func (a *analyzer) field(owner model.EntityRef, key, typ, direction string, file Capture) error {
	if len(a.out.Batch.Fields) >= 2048 {
		return schema.ErrLimit
	}
	a.out.Batch.Fields = append(a.out.Batch.Fields, contracts.Field{Entity: owner, FieldPath: key,
		Type: typ, Direction: direction, Declaration: file.Ref})
	return nil
}

func keys[T any](values map[string]T) []string {
	return slices.Sorted(maps.Keys(values))
}

func scope(ref model.SourceRef) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}
}

func digest(values ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(hash[:])
}
