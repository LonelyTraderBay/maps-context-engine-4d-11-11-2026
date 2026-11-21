// Package schema extracts bounded, source-bound contract declarations.
package schema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

var (
	ErrCapture = errors.New("invalid contract capture")
	ErrProfile = errors.New("invalid contract profile")
	ErrLimit   = errors.New("contract analysis limit exceeded")
)

const (
	maxFile   = 1 << 20
	maxFiles  = 32
	maxDepth  = 32
	maxSteps  = 65536
	maxIssues = 512
)

// Profile identifies one explicitly configured contract namespace.
type Profile struct {
	ServiceID, Environment, Namespace string
}

type Capture struct {
	Ref     model.SourceRef
	Bytes   []byte
	Kind    string // openapi, json_schema, or protobuf
	Version string // contract revision, independent of format revision
}

type Operation struct {
	Entity      contracts.Entity
	OperationID string
	Method      string
	Path        string
	Server      string
	// Bindable requires an unambiguous static server and route and no
	// effective security policy needing separate verification.
	Bindable bool
}

type Analysis struct {
	Batch       contracts.Batch
	Coverage    model.Coverage
	Operations  []Operation
	ProtoFields []ProtoField
}

type document struct {
	Capture
	root    *node
	dialect string
	schemas map[string]*declaration
	proto   *protoFile
}

type analyzer struct {
	profile Profile
	out     Analysis
	files   map[string]*document
	steps   int
	ctx     context.Context
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// AnalyzeCaptured reads only the supplied, policy-cleared whole-file captures.
// The owner supplies the allowlist and checks current generation membership.
func AnalyzeCaptured(ctx context.Context, profile Profile, captures []Capture) (Analysis, error) {
	if ctx == nil || len(captures) == 0 {
		return Analysis{}, ErrCapture
	}
	if len(captures) > maxFiles {
		return Analysis{}, ErrLimit
	}
	if !profileName.MatchString(profile.ServiceID) || !profileName.MatchString(profile.Environment) || !profileName.MatchString(profile.Namespace) {
		return Analysis{}, ErrProfile
	}
	a := analyzer{profile: profile, files: make(map[string]*document), ctx: ctx, out: Analysis{
		Batch: contracts.Batch{SchemaVersion: 1, Entities: []contracts.Entity{}, Fields: []contracts.Field{}, Links: []contracts.Link{}},
		Coverage: model.Coverage{Status: "supported_scope_complete", Capabilities: []model.Capability{}, Gaps: []model.Gap{},
			ScopeDescription: "OpenAPI JSON, JSON Schema, and Proto3 declarations in explicitly supplied captures."},
		Operations:  []Operation{},
		ProtoFields: []ProtoField{},
	}}
	order := slices.Clone(captures)
	slices.SortFunc(order, func(l, r Capture) int { return strings.Compare(l.Ref.Path, r.Ref.Path) })
	n := 0
	for _, capture := range order {
		n += len(capture.Bytes)
		if len(capture.Bytes) > maxFile || n > 4*maxFile {
			return Analysis{}, ErrLimit
		}
		if identity(capture.Ref) != identity(order[0].Ref) || a.files[capture.Ref.Path] != nil ||
			!boundedText(capture.Version, 128) {
			return Analysis{}, ErrCapture
		}
		if err := ValidateCapture(capture.Ref, capture.Bytes); err != nil {
			return Analysis{}, err
		}
		doc := &document{Capture: capture, schemas: make(map[string]*declaration)}
		a.files[capture.Ref.Path] = doc
		if err := a.read(doc); err != nil {
			return Analysis{}, err
		}
	}
	for _, capture := range order {
		doc := a.files[capture.Ref.Path]
		if err := a.emit(doc); err != nil {
			return Analysis{}, err
		}
	}
	if len(a.out.Coverage.Gaps) != 0 {
		a.out.Coverage.Status = "partial"
	}
	a.out.Coverage.Capabilities = append(a.out.Coverage.Capabilities, model.Capability{Name: "captured_contract_declarations",
		ProviderID: "mce4d.contracts", ProviderVersion: "1", Status: a.out.Coverage.Status})
	if err := ValidateBatch(a.out.Batch); err != nil {
		return Analysis{}, err
	}
	return a.out, ctx.Err()
}

func ValidateCapture(ref model.SourceRef, data []byte) error {
	if len(data) > maxFile {
		return ErrLimit
	}
	if ref.Range.StartByte != 0 || ref.Range.EndByte != int64(len(data)) || ref.ValidateCapturedBytes(data) != nil {
		return ErrCapture
	}
	scanner, err := policy.NewScanner(maxFile)
	if err != nil {
		return err
	}
	if err := scanner.CheckContent([]byte(ref.Path)); err != nil {
		return err
	}
	return scanner.CheckContent(data)
}

func ValidateBatch(batch contracts.Batch) error {
	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	_, err = contracts.DecodeBatch(raw)
	return err
}

func (a *analyzer) step() error {
	if a.steps >= maxSteps {
		return ErrLimit
	}
	a.steps++
	return a.ctx.Err()
}

func (a *analyzer) gap(code string, ref model.SourceRef) error {
	if len(a.out.Coverage.Gaps) >= maxIssues {
		return ErrLimit
	}
	a.out.Coverage.Gaps = append(a.out.Coverage.Gaps, model.Gap{
		Code: code, Reason: "The captured declaration could not be fully resolved in the supported contract subset.",
		SourceRefs: []model.SourceRef{ref},
	})
	return nil
}

func (a *analyzer) entity(doc *document, protocol, kind, identifier string, ref model.SourceRef) (contracts.Entity, error) {
	if len(a.out.Batch.Entities) >= 512 || !boundedText(identifier, 1024) {
		return contracts.Entity{}, ErrLimit
	}
	svc := a.profile.ServiceID
	item := contracts.Entity{Ref: model.EntityRef{Kind: "entity", GraphIdentity: identity(ref)}, Kind: kind,
		Plane: "intended", Declaration: ref, Key: contracts.Key{ServiceID: &svc,
			Environment: a.profile.Environment, Protocol: protocol, Namespace: a.profile.Namespace,
			Identifier: identifier, Version: doc.Version}}
	key, err := item.QualifiedKey()
	if err != nil {
		return contracts.Entity{}, err
	}
	item.Ref.EntityID = digest("captured-contract-entity-v1", key, doc.dialect, ref.Path,
		strconv.FormatInt(ref.Range.StartByte, 10), strconv.FormatInt(ref.Range.EndByte, 10))
	a.out.Batch.Entities = append(a.out.Batch.Entities, item)
	return item, nil
}

func (a *analyzer) field(owner model.EntityRef, name, direction, typ string, nullable *bool, ref model.SourceRef) error {
	if len(a.out.Batch.Fields) >= 2048 || !boundedText(name, 1024) || !boundedText(typ, 128) {
		return ErrLimit
	}
	a.out.Batch.Fields = append(a.out.Batch.Fields, contracts.Field{
		Entity: owner, FieldPath: name, Direction: direction, Type: typ, Nullable: nullable, Declaration: ref,
	})
	return nil
}

func (a *analyzer) link(from, to model.EntityRef, relation string, ref model.SourceRef) error {
	if len(a.out.Batch.Links) >= 4096 {
		return ErrLimit
	}
	id := digest("captured-contract-link-v1", from.EntityID, to.EntityID, relation, ref.Path,
		strconv.FormatInt(ref.Range.StartByte, 10), strconv.FormatInt(ref.Range.EndByte, 10))
	a.out.Batch.Links = append(a.out.Batch.Links, contracts.Link{
		LinkID: id, Source: &from, Target: &to, Kind: relation, ResolutionStatus: "exact_in_supported_scope",
		Evidence: model.EvidenceRef{EvidenceID: id, Origin: "static_analyzer", Kind: "syntax_exact", SourceRefs: []model.SourceRef{ref}},
	})
	return nil
}

func identity(ref model.SourceRef) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}
}

func digest(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}

func boundedText(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func span(ref model.SourceRef, start, end int) model.SourceRef {
	ref.Range = model.ByteRange{StartByte: int64(start), EndByte: int64(end)}
	return ref
}
