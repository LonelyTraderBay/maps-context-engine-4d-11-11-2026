package flowview

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/sql"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type SQLInput struct {
	Profile sql.Profile
	Config  *model.SourceRef  // optional captured dialect configuration
	Schemas []model.SourceRef // explicit approved whole captured schema files
}

type SQLSite struct {
	StepID             string
	Operation          string
	Source             model.SourceRef
	Targets            []contracts.Entity
	OutcomeStatus      string
	TransactionGroupID string
}

func (p *projector) sqlCall(fn function, call *ast.CallExpr, proof *golang.StdlibCallTarget, at model.SourceRef, refs []model.GraphRef, order string) (string, error) {
	op := ""
	switch {
	case proof.Receiver == "DB" && (proof.Name == "Begin" || proof.Name == "BeginTx"):
		op = "BEGIN"
	case proof.Receiver == "Tx" && proof.Name == "Commit":
		op = "COMMIT"
	case proof.Receiver == "Tx" && proof.Name == "Rollback":
		op = "ROLLBACK"
	}
	if op != "" {
		step := p.step("sql_"+strings.ToLower(op), order, "outcome_unknown", at, refs, "type_resolved", "")
		if step != "" {
			item := SQLSite{StepID: step, Operation: op, Source: at, Targets: []contracts.Entity{}, OutcomeStatus: "outcome_unknown"}
			p.transactionSite(fn, call, proof, &item)
			p.out.SQL = append(p.out.SQL, item)
		}
		return step, nil
	}
	argument := 0
	switch proof.Name {
	case "Exec", "Query", "QueryRow":
	case "ExecContext", "QueryContext", "QueryRowContext":
		argument = 1
	default:
		return p.step("sql_boundary", "call_relationship_only", "outcome_unknown", at, refs, "type_resolved", "FLOW_SQL_METHOD_UNSUPPORTED"), nil
	}
	if proof.Receiver != "DB" && proof.Receiver != "Tx" && proof.Receiver != "Conn" {
		return p.step("sql_boundary", "call_relationship_only", "outcome_unknown", at, refs, "type_resolved", "FLOW_SQL_RECEIVER_UNSUPPORTED"), nil
	}
	if len(call.Args) <= argument {
		return "", ErrInput
	}
	site, ok := literalSQL(fn.file, call.Args[argument])
	if !ok {
		return p.step("sql_boundary", order, "outcome_unknown", at, refs, "type_resolved", "FLOW_SQL_DYNAMIC"), nil
	}
	if p.options.SQL == nil {
		return p.step("sql_boundary", order, "outcome_unknown", site, refs, "type_resolved", "FLOW_SQL_DIALECT_UNVERIFIED"), nil
	}
	sources, err := p.sqlSources(site, fn.file.raw)
	if err != nil {
		return "", err
	}
	a, err := sql.AnalyzeCaptured(p.ctx, p.options.SQL.Profile, sources)
	if err != nil {
		return "", err
	}
	targets := make(map[model.EntityRef]contracts.Entity)
	var query model.EntityRef
	for _, entity := range a.Batch.Entities {
		if entity.Kind == "query" && entity.Declaration.Path == site.Path && entity.Declaration.ContentHash == site.ContentHash &&
			entity.Declaration.Range.StartByte >= site.Range.StartByte && entity.Declaration.Range.EndByte <= site.Range.EndByte {
			query = entity.Ref
		}
		targets[entity.Ref] = entity
	}
	var proven []contracts.Entity
	if query.EntityID != "" {
		contents := fn.file.raw[site.Range.StartByte:site.Range.EndByte]
		head := strings.Fields(string(contents))
		if len(head) != 0 {
			switch candidate := strings.ToUpper(head[0]); candidate {
			case "SELECT":
				op = "READ"
			case "INSERT", "UPDATE", "DELETE", "CALL":
				op = candidate
			}
		}
		seen := make(map[model.EntityRef]bool)
		for _, link := range a.Batch.Links {
			source, left := link.Source.(*model.EntityRef)
			target, right := link.Target.(*model.EntityRef)
			if left && right && *source == query && !seen[*target] &&
				(link.Kind == "reads" || link.Kind == "writes" || link.Kind == "calls") {
				proven = append(proven, targets[*target])
				seen[*target] = true
			}
		}
	}
	kind, gap := "sql_boundary", "FLOW_SQL_UNRESOLVED"
	if op != "" {
		kind, gap = "sql_"+strings.ToLower(op), ""
	}
	if len(a.Coverage.Gaps) != 0 && gap == "" {
		gap = "FLOW_SQL_LINEAGE_PARTIAL"
	}
	for i := range proven {
		if len(refs) == 16 {
			break
		}
		refs = append(refs, &proven[i].Ref)
	}
	step := p.step(kind, order, "outcome_unknown", site, refs, "type_resolved", gap)
	if step != "" && op != "" {
		flowStep := &p.out.Data.Steps[len(p.out.Data.Steps)-1]
		for _, target := range proven {
			if len(flowStep.SourceRefs) == 16 {
				break
			}
			flowStep.SourceRefs = append(flowStep.SourceRefs, target.Declaration)
		}
		if config := p.options.SQL.Config; config != nil && len(flowStep.EvidenceRefs[0].SourceRefs) < 16 {
			flowStep.EvidenceRefs[0].SourceRefs = append(flowStep.EvidenceRefs[0].SourceRefs, *config)
		}
		if proven == nil {
			proven = []contracts.Entity{}
		}
		item := SQLSite{StepID: step, Operation: op, Source: site, Targets: proven, OutcomeStatus: "outcome_unknown"}
		p.transactionSite(fn, call, proof, &item)
		p.out.SQL = append(p.out.SQL, item)
	}
	return step, nil
}

func literalSQL(file *goFile, expr ast.Expr) (model.SourceRef, bool) {
	literal, ok := ast.Unparen(expr).(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return model.SourceRef{}, false
	}
	ref := file.source(literal)
	if ref.Range.EndByte-ref.Range.StartByte < 2 {
		return model.SourceRef{}, false
	}
	ref.Range.StartByte++
	ref.Range.EndByte--
	raw := file.raw[ref.Range.StartByte:ref.Range.EndByte]
	value, err := strconv.Unquote(literal.Value)
	return ref, err == nil && len(value) != 0 && value == string(raw)
}

func (p *projector) sqlSources(site model.SourceRef, data []byte) ([]sql.Source, error) {
	if len(p.options.SQL.Schemas) > 31 {
		return nil, ErrLimit
	}
	sources := []sql.Source{{Kind: "literal", Ref: site, Data: data}}
	m := p.program.member
	if ref := p.options.SQL.Config; ref != nil {
		if ref.WorktreeID != m.WorktreeID || ref.ProfileID != m.ProfileID || ref.GenerationID != m.GenerationID {
			return nil, ErrInput
		}
		file, ok := p.program.capture.File(ref.Path)
		if !ok || ref.ValidateCapturedBytes(file.Bytes()) != nil {
			return nil, ErrInput
		}
		if err := p.program.checkPath(p.ctx, ref.Path); err != nil {
			return nil, err
		}
	}
	for _, ref := range p.options.SQL.Schemas {
		if ref.WorktreeID != m.WorktreeID || ref.ProfileID != m.ProfileID || ref.GenerationID != m.GenerationID {
			return nil, ErrInput
		}
		if err := p.program.checkPath(p.ctx, ref.Path); err != nil {
			return nil, err
		}
		file, ok := p.program.capture.File(ref.Path)
		if !ok || file.ContentHash() != ref.ContentHash || ref.Range.StartByte != 0 || ref.Range.EndByte != int64(file.Len()) {
			return nil, ErrInput
		}
		sources = append(sources, sql.Source{Kind: "schema", Ref: ref, Data: file.Bytes()})
	}
	return sources, nil
}
