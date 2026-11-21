package sql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

const (
	maxSources        = 32
	maxSourceBytes    = 1 << 20
	maxAggregateBytes = 4 << 20
	maxEntities       = 512
	maxFields         = 2048
	maxLinks          = 4096
	maxGaps           = 512
)

type capturedDeclaration struct {
	owner  model.EntityRef
	source model.SourceRef
	engine string
	uses   []objectUse
}

type analyzer struct {
	profile      Profile
	result       Analysis
	declarations map[objectName][]contracts.Entity
	pending      []capturedDeclaration
}

// AnalyzeCaptured verifies the source identity and parses only the selected
// byte ranges in one captured generation. Its caller supplies authorized
// captures and verifies current scope before publication.
func AnalyzeCaptured(ctx context.Context, profile Profile, sources []Source) (Analysis, error) {
	if ctx == nil || len(sources) == 0 {
		return Analysis{}, ErrSource
	}
	if len(sources) > maxSources {
		return Analysis{}, ErrLimit
	}
	normalized, digest, err := profile.normalized()
	if err != nil {
		return Analysis{}, err
	}
	a := analyzer{profile: normalized, declarations: make(map[objectName][]contracts.Entity), result: Analysis{
		ProfileDigest: digest,
		Batch:         contracts.Batch{SchemaVersion: 1, Entities: []contracts.Entity{}, Fields: []contracts.Field{}, Links: []contracts.Link{}},
		Coverage: model.Coverage{Status: "supported_scope_complete", Capabilities: []model.Capability{},
			Gaps: []model.Gap{}, ScopeDescription: "Static MySQL declarations and object uses in one captured generation."}}}
	input := slices.Clone(sources)
	slices.SortStableFunc(input, func(left, right Source) int {
		cmp := strings.Compare(left.Ref.Path, right.Ref.Path)
		if cmp != 0 {
			return cmp
		}
		if left.Ref.Range.StartByte < right.Ref.Range.StartByte {
			return -1
		}
		if left.Ref.Range.StartByte > right.Ref.Range.StartByte {
			return 1
		}
		return 0
	})
	identity := sourceIdentity(input[0].Ref)
	scanner, err := policy.NewScanner(maxSourceBytes)
	if err != nil {
		return Analysis{}, err
	}
	total := 0
	for i, item := range input {
		if err := ctx.Err(); err != nil {
			return Analysis{}, err
		}
		if len(item.Data) > maxSourceBytes || len(item.Data) > maxAggregateBytes-total {
			return Analysis{}, ErrLimit
		}
		total += len(item.Data)
		if !validSourceKind(item.Kind) || sourceIdentity(item.Ref) != identity || item.Ref.ValidateCapturedBytes(item.Data) != nil {
			return Analysis{}, ErrSource
		}
		if i > 0 && input[i-1].Ref.Path == item.Ref.Path {
			previous := input[i-1].Ref
			if previous.ContentHash != item.Ref.ContentHash || previous.Range.EndByte > item.Ref.Range.StartByte ||
				previous.Range.StartByte == item.Ref.Range.StartByte {
				return Analysis{}, ErrSource
			}
		}
		if err := scanner.CheckContent([]byte(item.Ref.Path)); err != nil {
			return Analysis{}, err
		}
		if err := scanner.CheckContent(item.Data); err != nil {
			return Analysis{}, err
		}
		if err := a.readSource(ctx, item); err != nil {
			return Analysis{}, err
		}
	}
	if err := a.finishLinks(ctx); err != nil {
		return Analysis{}, err
	}
	if len(a.result.Coverage.Gaps) != 0 {
		a.result.Coverage.Status = "partial"
	}
	a.result.Coverage.Capabilities = append(a.result.Coverage.Capabilities, model.Capability{
		Name: "mysql_static_lineage", ProviderID: "mce4d.mysql", ProviderVersion: "1", Status: a.result.Coverage.Status})
	raw, err := json.Marshal(a.result.Batch)
	if err != nil {
		return Analysis{}, err
	}
	if _, err := contracts.DecodeBatch(raw); err != nil {
		return Analysis{}, err
	}
	if err := ctx.Err(); err != nil {
		return Analysis{}, err
	}
	return a.result, nil
}

func (a *analyzer) readSource(ctx context.Context, item Source) error {
	start, end := item.Ref.Range.StartByte, item.Ref.Range.EndByte
	statements, err := splitStatements(ctx, string(item.Data[start:end]), a.profile.SQLMode)
	if err != nil {
		return err
	}
	if item.Kind == "literal" && len(statements) > 1 {
		return a.gap("SQL_LITERAL_MULTI_STATEMENT_UNSUPPORTED", item.Ref)
	}
	var migration *model.EntityRef
	if item.Kind == "migration" && len(statements) > 0 {
		entity, err := a.addEntity("migration", item.Ref.Path, a.profile.Engine, item.Ref)
		if err != nil {
			return err
		}
		migration = &entity.Ref
	}
	schema := a.profile.Schema
	for statementIndex, line := range statements {
		if err := ctx.Err(); err != nil {
			return err
		}
		ref := subrange(item.Ref, line.start, line.end)
		if line.gap != "" {
			if err := a.gap(line.gap, ref); err != nil {
				return err
			}
			if line.gap == "SQL_EXECUTABLE_COMMENT" && statementIndex+1 < len(statements) {
				return a.gap("SQL_SESSION_STATE_UNRESOLVED", subrange(item.Ref, line.end, int(end-start)))
			}
			continue
		}
		if keywordAt(line.tokens, 0, "SET") && changesSQLMode(line.tokens) {
			return a.gap("SQL_SESSION_STATE_UNRESOLVED", subrange(item.Ref, line.start, int(end-start)))
		}
		if keywordAt(line.tokens, 0, "USE") {
			if item.Kind == "literal" {
				return a.gap("SQL_LITERAL_DECLARATION_UNSUPPORTED", ref)
			}
			next, ok := identifierAt(line.tokens, 1)
			if len(line.tokens) == 2 && ok {
				schema = next
			} else {
				schema = ""
				if err := a.gap("SQL_SCHEMA_UNRESOLVED", ref); err != nil {
					return err
				}
			}
			continue
		}
		value := parseDeclaration(line.tokens, schema, a.profile)
		if item.Kind == "literal" && value.kind != "" && value.kind != "query" {
			return a.gap("SQL_LITERAL_DECLARATION_UNSUPPORTED", ref)
		}
		if err := a.addDeclaration(item.Ref, ref, value, migration); err != nil {
			return err
		}
	}
	return nil
}

func changesSQLMode(tokens []token) bool {
	for _, item := range tokens {
		if (item.kind == bare || item.kind == identifier) && strings.EqualFold(item.text, "sql_mode") {
			return true
		}
	}
	return false
}

func (a *analyzer) addDeclaration(source, ref model.SourceRef, spec declaration, migration *model.EntityRef) error {
	for _, code := range spec.gaps {
		if err := a.gap(code, ref); err != nil {
			return err
		}
	}
	if spec.kind == "" {
		return nil
	}
	name := spec.name.key()
	if spec.kind == "query" {
		name = ref.Path + "@" + strconv.FormatInt(ref.Range.StartByte, 10)
	}
	entity, err := a.addEntity(spec.kind, name, spec.engine, ref)
	if err != nil {
		return err
	}
	if spec.kind != "query" && spec.kind != "migration_change" {
		a.declarations[spec.name] = append(a.declarations[spec.name], entity)
	}
	if migration != nil {
		if err := a.addLink(*migration, entity.Ref, "declares", ref); err != nil {
			return err
		}
	}
	for _, field := range spec.columns {
		if len(a.result.Batch.Fields) >= maxFields {
			return ErrLimit
		}
		a.result.Batch.Fields = append(a.result.Batch.Fields, contracts.Field{
			Entity: entity.Ref, FieldPath: field.name, Direction: "stored", Type: field.typ,
			Nullable: field.nullable, Precision: field.precision, Declaration: subrange(source, field.start, field.end)})
	}
	if len(spec.uses) != 0 {
		a.pending = append(a.pending, capturedDeclaration{owner: entity.Ref, source: source, engine: spec.engine, uses: spec.uses})
	}
	return nil
}

func (a *analyzer) addEntity(kind, name, engine string, ref model.SourceRef) (contracts.Entity, error) {
	if len(a.result.Batch.Entities) >= maxEntities || !validProfileText(kind+":"+name, 1024) {
		return contracts.Entity{}, ErrLimit
	}
	item := contracts.Entity{
		Ref:  model.EntityRef{Kind: "entity", GraphIdentity: sourceIdentity(ref)},
		Kind: kind, Plane: "intended", Declaration: ref,
		Key: contracts.Key{ServiceID: a.profile.ServiceID, Environment: a.profile.Environment, Protocol: "mysql",
			Namespace: a.profile.Namespace + "." + engine, Identifier: kind + ":" + name, Version: a.profile.Version},
	}
	if kind == "query" {
		item.Plane = "observed_static"
	}
	key, err := item.QualifiedKey()
	if err != nil {
		return contracts.Entity{}, err
	}
	item.Ref.EntityID = digestParts("mysql-entity-v1", a.result.ProfileDigest, key, ref.Path, strconv.FormatInt(ref.Range.StartByte, 10),
		strconv.FormatInt(ref.Range.EndByte, 10))
	a.result.Batch.Entities = append(a.result.Batch.Entities, item)
	return item, nil
}

func (a *analyzer) finishLinks(ctx context.Context) error {
	for _, pending := range a.pending {
		for _, use := range pending.uses {
			if err := ctx.Err(); err != nil {
				return err
			}
			ref := subrange(pending.source, use.start, use.end)
			var target contracts.Entity
			count := 0
			for _, option := range a.declarations[use.name] {
				if use.kind == option.Kind || use.kind == "table_or_view" && (option.Kind == "table" || option.Kind == "view") {
					target, count = option, count+1
				}
			}
			switch count {
			case 1:
				if use.relation == "references" && (pending.engine == "unknown" || target.Key.Namespace != a.profile.Namespace+"."+pending.engine) {
					if err := a.gap("SQL_REFERENCE_ENGINE_UNRESOLVED", ref); err != nil {
						return err
					}
					continue
				}
				if err := a.addLink(pending.owner, target.Ref, use.relation, ref); err != nil {
					return err
				}
			case 0:
				if err := a.gap("SQL_RELATED_OBJECT_UNDECLARED", ref); err != nil {
					return err
				}
			default:
				if err := a.gap("SQL_RELATED_OBJECT_AMBIGUOUS", ref); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (a *analyzer) addLink(from, to model.EntityRef, relation string, ref model.SourceRef) error {
	if len(a.result.Batch.Links) >= maxLinks {
		return ErrLimit
	}
	id := digestParts("mysql-link-v1", from.EntityID, to.EntityID, relation, ref.Path,
		strconv.FormatInt(ref.Range.StartByte, 10), strconv.FormatInt(ref.Range.EndByte, 10))
	a.result.Batch.Links = append(a.result.Batch.Links, contracts.Link{
		LinkID: id, Source: &from, Target: &to, Kind: relation, ResolutionStatus: "exact_in_supported_scope",
		Evidence: model.EvidenceRef{EvidenceID: id, Origin: "static_analyzer", Kind: "syntax_exact", SourceRefs: []model.SourceRef{ref}},
	})
	return nil
}

func (a *analyzer) gap(code string, ref model.SourceRef) error {
	if len(a.result.Coverage.Gaps) >= maxGaps {
		return ErrLimit
	}
	reason := "The selected MySQL syntax has details outside the supported analysis subset."
	switch code {
	case "SQL_RELATED_OBJECT_UNDECLARED":
		reason = "The referenced object has no matching declaration in the captured input."
	case "SQL_RELATED_OBJECT_AMBIGUOUS":
		reason = "More than one captured declaration matches the referenced object."
	case "SQL_DYNAMIC_STATEMENT":
		reason = "The prepared statement target cannot be resolved from a static declaration."
	case "SQL_EXECUTABLE_COMMENT":
		reason = "A version-dependent SQL comment affects the statement."
	case "SQL_REFERENCE_ENGINE_UNRESOLVED":
		reason = "The foreign-key engines do not have matching declared profiles."
	case "SQL_SESSION_STATE_UNRESOLVED":
		reason = "Session syntax settings after this point are unknown."
	}
	a.result.Coverage.Gaps = append(a.result.Coverage.Gaps, model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{ref}})
	return nil
}

func digestParts(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func sourceIdentity(ref model.SourceRef) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}
}

func subrange(ref model.SourceRef, start, end int) model.SourceRef {
	base := ref.Range.StartByte
	ref.Range = model.ByteRange{StartByte: base + int64(start), EndByte: base + int64(end)}
	return ref
}

func validSourceKind(kind string) bool {
	return kind == "schema" || kind == "migration" || kind == "literal"
}
