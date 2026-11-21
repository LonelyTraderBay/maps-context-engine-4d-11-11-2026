package flowview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"sort"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/events"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/gohttp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type Options struct {
	Steps, Edges int
	CallDepth    int
	// Registration chooses one exact route occurrence when a handler has
	// multiple verified registrations.
	Registration *model.SourceRef
	SQL          *SQLInput
	Events       []events.Analysis
}

type Result struct {
	Data      model.FlowData
	Routes    []Route
	SQL       []SQLSite
	Events    []EventSite
	Partial   bool
	Truncated bool
}

type projector struct {
	ctx          context.Context
	program      *Program
	options      Options
	out          Result
	active       map[string]bool
	entries      map[string]string
	transactions map[string]map[string]transaction
	events       map[string][]eventBinding
}

func (p *Program) Project(ctx context.Context, root model.SymbolRef, opt Options) (Result, error) {
	fn := p.functions[root.SymbolID]
	if ctx == nil || root.Kind != "symbol" || root.GraphIdentity != p.identity || fn.id == "" ||
		opt.Steps < 1 || opt.Steps > 100 || opt.Edges < 1 || opt.Edges > 300 || opt.CallDepth < 0 || opt.CallDepth > 4 {
		return Result{}, ErrInput
	}
	if err := p.checkScope(ctx); err != nil {
		return Result{}, err
	}
	out := &projector{ctx: ctx, program: p, options: opt, active: make(map[string]bool), entries: make(map[string]string),
		transactions: make(map[string]map[string]transaction), events: make(map[string][]eventBinding), out: Result{
			Data:   model.FlowData{Operation: "flow_query", RootRef: &root, Mode: "static", Steps: []model.FlowStep{}, Edges: []model.FlowEdge{}},
			Routes: []Route{}, SQL: []SQLSite{}, Events: []EventSite{},
		}}
	if err := out.admitEvents(); err != nil {
		return Result{}, err
	}
	for _, route := range p.routes {
		if route.Handler == root || route.RegistrationOwner == root {
			out.out.Routes = append(out.out.Routes, route)
		}
	}
	routes := out.out.Routes
	if opt.Registration != nil {
		routes = []Route{}
		for _, route := range out.out.Routes {
			if route.Site == *opt.Registration {
				routes = append(routes, route)
			}
		}
		if len(routes) != 1 {
			return Result{}, ErrInput
		}
	}
	var entry string
	if len(routes) == 1 {
		r := routes[0]
		from, handler := r.RegistrationOwner, r.Handler
		entry = out.step("http_route", "call_relationship_only", "statically_possible", r.Site,
			[]model.GraphRef{&from, &handler}, "type_resolved", "")
		fn = p.functions[handler.SymbolID]
	}
	kind, gap := "function", "FLOW_ENTRY_OPERATION_UNRESOLVED"
	if entry != "" {
		kind, gap = "http_handler", ""
	} else if len(routes) > 1 {
		gap = "FLOW_HTTP_ROUTE_CHOICE_REQUIRED"
	}
	ref := p.symbol(fn.id)
	step := out.step(kind, "call_relationship_only", "statically_possible", fn.at, []model.GraphRef{&ref}, "type_resolved", gap)
	if entry != "" {
		out.edge(entry, step, "contract", &routes[0].Site, "type_resolved")
	}
	if err := out.function(fn, step, opt.CallDepth); err != nil {
		return Result{}, err
	}
	if len(out.out.Data.Steps) == 0 {
		return Result{}, ErrLimit
	}
	if out.out.Partial {
		out.note(out.out.Data.Steps[0].StepID, "FLOW_SCOPE_PARTIAL", fn.at)
	}
	data := &out.out.Data
	checked := make(map[string]bool)
	for _, step := range data.Steps {
		for _, ref := range step.SourceRefs {
			if checked[ref.Path] {
				continue
			}
			if err := p.checkPath(ctx, ref.Path); err != nil {
				return Result{}, err
			}
			checked[ref.Path] = true
		}
	}
	for _, route := range out.out.Routes {
		if !checked[route.Site.Path] {
			if err := p.checkPath(ctx, route.Site.Path); err != nil {
				return Result{}, err
			}
			checked[route.Site.Path] = true
		}
	}
	if err := p.checkScope(ctx); err != nil {
		return Result{}, err
	}
	raw, err := json.Marshal(out.out)
	if err != nil || len(raw) > 1<<20 {
		return Result{}, ErrLimit
	}
	data.ProjectionID = digest("static-flow-projection-v1", string(raw))
	raw, err = json.Marshal(data)
	if err != nil {
		return Result{}, err
	}
	if _, err = model.DecodeFlowDataJSON(raw, 1<<20); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return out.out, nil
}

func (p *projector) function(fn function, entry string, depth int) error {
	if entry == "" {
		return nil
	}
	p.active[fn.id] = true
	p.entries[fn.id] = entry
	p.transactions[fn.id] = make(map[string]transaction)
	defer delete(p.active, fn.id)
	defer delete(p.entries, fn.id)
	defer delete(p.transactions, fn.id)
	p.attachEvents(fn, entry)
	_, err := p.block(fn, fn.decl.Body.List, entry, depth)
	return err
}

func (p *projector) block(fn function, list []ast.Stmt, head string, depth int) (string, error) {
	previous := head
	for _, stmt := range list {
		if err := p.ctx.Err(); err != nil {
			return "", err
		}
		if len(p.out.Data.Steps) >= p.options.Steps {
			p.out.Partial = true
			p.out.Truncated = true
			return "", nil
		}
		owner := p.program.symbol(fn.id)
		refs := []model.GraphRef{&owner}
		at := fn.file.source(stmt)
		switch n := stmt.(type) {
		case *ast.BlockStmt:
			var err error
			previous, err = p.block(fn, n.List, previous, depth)
			if err != nil {
				return "", err
			}
		case *ast.IfStmt:
			if err := p.branch(fn, n, previous, depth); err != nil {
				return "", err
			}
			previous = "" // The subset does not prove post-dominance at joins.
		case *ast.ReturnStmt:
			if _, err := p.expressionCalls(fn, stmt, previous, depth, "call_relationship_only"); err != nil {
				return "", err
			}
			step := p.step("return", "control_flow_supported", "statically_possible", at, refs, "syntax_exact", "")
			p.edge(previous, step, "return", &at, "syntax_exact")
			return "", nil
		case *ast.GoStmt:
			relation := fn.file.calls[fn.file.position(n.Call.Fun)]
			if relation.SourceSymbolID == fn.id && relation.TargetSymbolID != nil {
				target := p.program.symbol(*relation.TargetSymbolID)
				refs = append(refs, &target)
			}
			step := p.step("async_boundary", "control_flow_supported", "outcome_unknown", at, refs, "syntax_exact", "FLOW_ASYNC_ORDER_UNKNOWN")
			p.edge(previous, step, "async", &at, "syntax_exact")
		case *ast.DeferStmt:
			target := fn.file.callees[fn.file.position(n.Call.Fun)].StdlibTarget
			if target != nil && target.Package == "database/sql" && target.Receiver == "Tx" &&
				(target.Name == "Commit" || target.Name == "Rollback") {
				step, err := p.sqlCall(fn, n.Call, target, at, refs, "unknown")
				if err != nil {
					return "", err
				}
				p.note(step, "FLOW_DEFERRED_EFFECT_UNKNOWN", at)
				continue
			}
			step := p.step("defer_boundary", "unknown", "outcome_unknown", at, refs, "syntax_exact", "FLOW_DEFERRED_EFFECT_UNKNOWN")
			p.edge(previous, step, "control", &at, "syntax_exact")
		case *ast.ForStmt, *ast.RangeStmt:
			step := p.step("loop_boundary", "unknown", "outcome_unknown", at, refs, "syntax_exact", "FLOW_LOOP_COUNT_UNVERIFIED")
			p.edge(previous, step, "control", &at, "syntax_exact")
			previous = ""
		case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt,
			*ast.BranchStmt, *ast.LabeledStmt:
			step := p.step("control_boundary", "unknown", "outcome_unknown", at, refs, "syntax_exact", "FLOW_CONTROL_UNSUPPORTED")
			p.edge(previous, step, "control", &at, "syntax_exact")
			previous = ""
		default:
			var err error
			previous, err = p.expressionCalls(fn, n, previous, depth, "source_sequence")
			if err != nil {
				return "", err
			}
		}
	}
	return previous, nil
}

func (p *projector) branch(fn function, n *ast.IfStmt, previous string, depth int) error {
	ref := p.program.symbol(fn.id)
	refs := []model.GraphRef{&ref}
	if n.Init != nil {
		if _, err := p.expressionCalls(fn, n.Init, previous, depth, "call_relationship_only"); err != nil {
			return err
		}
	}
	cond := fn.file.source(n.Cond)
	if _, err := p.expressionCalls(fn, n.Cond, "", depth, "call_relationship_only"); err != nil {
		return err
	}
	step := p.step("branch", "control_flow_supported", "statically_possible", cond, refs, "syntax_exact", "")
	p.edge(previous, step, "control", &cond, "syntax_exact")
	then := p.step("branch_then", "control_flow_supported", "statically_possible", fn.file.source(n.Body), refs, "syntax_exact", "")
	p.condition(step, then, cond)
	if _, err := p.block(fn, n.Body.List, then, depth); err != nil {
		return err
	}
	if n.Else != nil {
		other := p.step("branch_else", "control_flow_supported", "statically_possible", fn.file.source(n.Else), refs, "syntax_exact", "")
		p.condition(step, other, cond)
		if _, err := p.block(fn, []ast.Stmt{n.Else}, other, depth); err != nil {
			return err
		}
	}
	return nil
}

func (p *projector) expressionCalls(fn function, expr ast.Node, previous string, depth int, order string) (string, error) {
	var calls []*ast.CallExpr
	ast.Inspect(expr, func(n ast.Node) bool {
		if _, nested := n.(*ast.FuncLit); nested {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			typed := fn.file.callees[fn.file.position(call.Fun)]
			if typed.Kind != "conversion" {
				calls = append(calls, call)
			}
		}
		return true
	})
	if len(calls) > 1 {
		order = "call_relationship_only"
		sort.SliceStable(calls, func(i, j int) bool { return calls[i].Pos() < calls[j].Pos() })
	}
	result := previous
	for _, call := range calls {
		if err := p.ctx.Err(); err != nil {
			return "", err
		}
		step, err := p.call(fn, call, previous, depth, order)
		if err != nil {
			return "", err
		}
		if order == "source_sequence" {
			result = step
		}
	}
	if len(calls) > 1 {
		result = ""
	}
	return result, nil
}

func (p *projector) call(fn function, call *ast.CallExpr, previous string, depth int, order string) (string, error) {
	at := fn.file.source(call)
	owner := p.program.symbol(fn.id)
	refs := []model.GraphRef{&owner}
	proof := fn.file.callees[fn.file.position(call.Fun)]
	if proof.StdlibTarget != nil && proof.StdlibTarget.Package == "database/sql" {
		step, err := p.sqlCall(fn, call, proof.StdlibTarget, at, refs, order)
		if err != nil {
			return "", err
		}
		p.linkCall(fn.id, previous, step, order, at)
		return step, nil
	}
	if _, _, known := gohttp.Registration(proof.StdlibTarget, call); known {
		return previous, nil
	}
	relation := fn.file.calls[fn.file.position(call.Fun)]
	if relation.TargetSymbolID == nil || relation.SourceSymbolID != fn.id || p.program.functions[*relation.TargetSymbolID].id == "" {
		step := p.step("unresolved_call", "call_relationship_only", "outcome_unknown", at, refs, "syntax_exact", "FLOW_CALL_TARGET_UNRESOLVED")
		p.edge(p.entries[fn.id], step, "candidate", &at, "syntax_exact")
		return "", nil
	}
	target := p.program.functions[*relation.TargetSymbolID]
	decl := p.program.symbol(target.id)
	refs = append(refs, &decl)
	gap := ""
	if p.active[target.id] {
		gap = "FLOW_RECURSIVE_BOUNDARY"
	} else if depth == 0 {
		gap = "FLOW_CALL_DEPTH_BOUNDARY"
	}
	step := p.step("direct_call", order, "statically_possible", at, refs, "type_resolved", gap)
	p.linkCall(fn.id, previous, step, order, at)
	if gap == "" {
		entry := p.step("callee", "call_relationship_only", "statically_possible", target.at, []model.GraphRef{&decl}, "type_resolved", "")
		p.edge(step, entry, "call", &at, "type_resolved")
		if err := p.function(target, entry, depth-1); err != nil {
			return "", err
		}
	}
	return step, nil
}

func (p *projector) linkCall(fn, from, to, order string, at model.SourceRef) {
	if order == "source_sequence" && from != "" {
		p.edge(from, to, "control", &at, "syntax_exact")
	} else {
		p.edge(p.entries[fn], to, "call", &at, "type_resolved")
	}
}

func (p *projector) step(kind, order, outcome string, at model.SourceRef, refs []model.GraphRef, evidenceKind, gap string) string {
	if len(p.out.Data.Steps) >= p.options.Steps {
		p.out.Partial = true
		p.out.Truncated = true
		return ""
	}
	id := digest("static-flow-step-v1", kind, at.Path, at.ContentHash, strconv.FormatInt(at.Range.StartByte, 10),
		strconv.FormatInt(at.Range.EndByte, 10), strconv.Itoa(len(p.out.Data.Steps)))
	item := model.FlowStep{StepID: id, CanonicalRefs: refs, SourceRefs: []model.SourceRef{at}, StepKind: kind, OrderSemantics: order,
		PossibleOrObserved: "statically_possible", OutcomeStatus: outcome, Limitations: []model.Gap{},
		EvidenceRefs: []model.EvidenceRef{evidence(id, at, evidenceKind)}}
	if gap != "" {
		item.Limitations = append(item.Limitations, limitation(gap, at))
		p.out.Partial = true
	}
	p.out.Data.Steps = append(p.out.Data.Steps, item)
	return id
}

func (p *projector) edge(from, to, kind string, at *model.SourceRef, evidenceKind string) *model.FlowEdge {
	if from == "" || to == "" || from == to {
		return nil
	}
	if len(p.out.Data.Edges) >= p.options.Edges {
		p.out.Partial = true
		p.out.Truncated = true
		return nil
	}
	sites := []model.EvidenceRef{}
	id := digest("static-flow-edge-v1", from, to, kind)
	if at != nil && evidenceKind != "" {
		sites = append(sites, evidence(id, *at, evidenceKind))
	}
	e := model.FlowEdge{EdgeID: id, FromStepID: from, ToStepID: to, Kind: kind, EvidenceRefs: sites}
	p.out.Data.Edges = append(p.out.Data.Edges, e)
	return &p.out.Data.Edges[len(p.out.Data.Edges)-1]
}

func (p *projector) condition(from, to string, source model.SourceRef) {
	if edge := p.edge(from, to, "control", &source, "syntax_exact"); edge != nil {
		edge.ConditionSourceRef = &source
	}
}

func (p *projector) note(id, code string, src model.SourceRef) {
	for i := range p.out.Data.Steps {
		if step := &p.out.Data.Steps[i]; step.StepID == id && len(step.Limitations) < 16 {
			step.Limitations = append(step.Limitations, limitation(code, src))
			p.out.Partial = true
			break
		}
	}
}

func evidence(id string, at model.SourceRef, kind string) model.EvidenceRef {
	return model.EvidenceRef{EvidenceID: id, Origin: "static_analyzer", Kind: kind, SourceRefs: []model.SourceRef{at}}
}

func digest(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}
