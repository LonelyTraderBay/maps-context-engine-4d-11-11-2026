// Package flowview projects bounded static evidence from a guarded Go load.
package flowview

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/gohttp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrInput      = errors.New("invalid static flow scope or capture")
	ErrUnverified = errors.New("typed static flow unavailable for this capture")
	ErrLimit      = errors.New("static flow limit exceeded")
)

type Route struct {
	Pattern           gohttp.Pattern
	Site              model.SourceRef
	RegistrationOwner model.SymbolRef
	Handler           model.SymbolRef
}

type Input struct {
	Graph  golang.GraphInput
	Member model.ScopeMember
	// ServeMuxModern explicitly selects Go 1.22+ pattern semantics.
	ServeMuxModern bool
}

type Program struct {
	identity       model.GraphIdentity
	member         model.ScopeMember
	capture        *snapshot.Snapshot
	files          map[string]*goFile
	functions      map[string]function
	routes         []Route
	gaps           []model.Gap
	authorizeScope func(context.Context, string, string, string, int64) error
	authorizePath  func(context.Context, string, string, string, int64) error
}

type position struct{ start, end int64 }

type goFile struct {
	ref         model.SourceRef
	raw         []byte
	tokens      *token.File
	parsed      *ast.File
	callees     map[position]golang.TypedCallSite
	occurrences map[position]golang.TypedOccurrence
	definitions map[position]golang.TypedOccurrence
	calls       map[position]model.RelationFact
}

type function struct {
	id   string
	file *goFile
	decl *ast.FuncDecl
	at   model.SourceRef
}

func Load(ctx context.Context, input Input) (*Program, error) {
	if ctx == nil || input.Graph.Typed.Loader.Preflight.Snapshot == nil {
		return nil, ErrInput
	}
	capture := input.Graph.Typed.Loader.Preflight.Snapshot
	manifest := capture.Manifest()
	member := input.Member
	if member.GenerationID == "" || member.WorktreeID != manifest.WorktreeID || member.ProfileID != manifest.ProfileID ||
		member.SnapshotDigest != manifest.SnapshotDigest || member.PolicyDigest != manifest.PolicyDigest ||
		member.PolicyEpoch != input.Graph.Typed.Loader.Preflight.PolicyEpoch {
		return nil, ErrInput
	}
	input.Graph.Typed.IncludeStdlibCallTargets = true
	result, err := golang.AnalyzeCapturedGraph(ctx, input.Graph)
	if err != nil {
		return nil, err
	}
	if !result.Typed.Loader.BuildSelectionVerified || result.Typed.Loader.SnapshotDigest != member.SnapshotDigest {
		return nil, fmt.Errorf("%w: %s", ErrUnverified, strings.Join(result.Typed.Loader.Reasons, ", "))
	}
	return buildProgram(ctx, input, capture, result)
}

func buildProgram(ctx context.Context, input Input, capture *snapshot.Snapshot, graph golang.GraphResult) (*Program, error) {
	m := input.Member
	p := &Program{identity: model.GraphIdentity{WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: m.GenerationID},
		member: m, capture: capture, files: make(map[string]*goFile), functions: make(map[string]function),
		routes: []Route{}, gaps: []model.Gap{}, authorizePath: input.Graph.Typed.Loader.Preflight.AuthorizePath,
		authorizeScope: input.Graph.Typed.Loader.Preflight.AuthorizeScope}
	for _, unit := range graph.Typed.Syntax {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, ok := capture.File(unit.Path)
		if !ok || file.ContentHash() != unit.ContentHash || p.files[unit.Path] != nil || !unit.BuildSelectionVerified {
			return nil, ErrInput
		}
		raw := file.Bytes()
		ref := model.SourceRef{Path: unit.Path, WorktreeID: m.WorktreeID, ProfileID: m.ProfileID, GenerationID: m.GenerationID,
			ContentHash: unit.ContentHash, Range: model.ByteRange{EndByte: int64(len(raw))}}
		if ref.ValidateCapturedBytes(raw) != nil {
			return nil, ErrInput
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, unit.Path, raw, parser.SkipObjectResolution)
		if err != nil {
			return nil, ErrInput
		}
		f := &goFile{ref: ref, raw: raw, parsed: parsed, tokens: fset.File(parsed.Pos()),
			callees: make(map[position]golang.TypedCallSite), occurrences: make(map[position]golang.TypedOccurrence),
			definitions: make(map[position]golang.TypedOccurrence),
			calls:       make(map[position]model.RelationFact)}
		p.files[unit.Path] = f
		definitions := make(map[position]string)
		for _, fn := range unit.Symbols {
			if fn.Range != nil && (fn.Kind == "function" || fn.Kind == "method") {
				definitions[position{fn.Range.StartByte, fn.Range.EndByte}] = fn.SymbolID
			}
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if id := definitions[f.position(fn.Name)]; id != "" {
				if p.functions[id].id != "" {
					return nil, ErrInput
				}
				p.functions[id] = function{id: id, file: f, decl: fn, at: f.source(fn.Name)}
			}
		}
	}
	for _, call := range graph.Typed.Calls {
		f := p.files[call.Path]
		if f == nil {
			return nil, ErrInput
		}
		f.callees[position{call.Range.StartByte, call.Range.EndByte}] = call
	}
	for _, use := range graph.Typed.Occurrences {
		if f := p.files[use.Path]; f != nil {
			at := position{use.Range.StartByte, use.Range.EndByte}
			switch use.Kind {
			case "use":
				f.occurrences[at] = use
			case "definition":
				f.definitions[at] = use
			}
		}
	}
	for _, edge := range graph.Relations {
		if f := p.files[edge.SitePath]; f != nil && edge.Kind == "CALLS_DIRECT" && edge.TargetSymbolID != nil {
			f.calls[position{edge.Range.StartByte, edge.Range.EndByte}] = edge
		}
	}
	ids := make([]string, 0, len(p.functions))
	for id := range p.functions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := p.findRoutes(ctx, p.functions[id], input.ServeMuxModern); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(p.routes, func(a, b Route) int {
		if c := strings.Compare(a.Site.Path, b.Site.Path); c != 0 {
			return c
		}
		if a.Site.Range.StartByte == b.Site.Range.StartByte {
			return 0
		}
		if a.Site.Range.StartByte < b.Site.Range.StartByte {
			return -1
		}
		return 1
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Program) findRoutes(ctx context.Context, fn function, modern bool) error {
	maxSites := 100
	var visitErr error
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		if visitErr != nil {
			return false
		}
		if err := ctx.Err(); err != nil {
			visitErr = err
			return false
		}
		if _, literal := n.(*ast.FuncLit); literal {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		proof := fn.file.callees[fn.file.position(call.Fun)]
		pattern, handler, registration := gohttp.Registration(proof.StdlibTarget, call)
		if !registration {
			return true
		}
		if len(p.gaps)+len(p.routes) >= maxSites {
			visitErr = ErrLimit
			return false
		}
		site := fn.file.source(call)
		id := fn.file.directSymbol(handler)
		if !modern || p.functions[id].id == "" {
			p.gaps = append(p.gaps, limitation("FLOW_HTTP_BINDING_UNRESOLVED", site))
			return true
		}
		p.routes = append(p.routes, Route{Pattern: pattern, Site: site,
			RegistrationOwner: p.symbol(fn.id), Handler: p.symbol(id)})
		return true
	})
	return visitErr
}

func (p *Program) Routes() []Route {
	return slices.Clone(p.routes)
}

func (p *Program) Gaps() []model.Gap {
	out := slices.Clone(p.gaps)
	for i := range out {
		out[i].SourceRefs = slices.Clone(out[i].SourceRefs)
	}
	return out
}

func (p *Program) symbol(id string) model.SymbolRef {
	return model.SymbolRef{Kind: "symbol", GraphIdentity: p.identity, SymbolID: id}
}

func (p *Program) checkScope(ctx context.Context) error {
	m := p.member
	return p.authorizeScope(ctx, m.WorktreeID, m.ProfileID, m.PolicyDigest, m.PolicyEpoch)
}

func (p *Program) checkPath(ctx context.Context, path string) error {
	m := p.member
	return p.authorizePath(ctx, m.WorktreeID, path, m.PolicyDigest, m.PolicyEpoch)
}

func (f *goFile) position(n ast.Node) position {
	return position{int64(f.tokens.Offset(n.Pos())), int64(f.tokens.Offset(n.End()))}
}

func (f *goFile) source(n ast.Node) model.SourceRef {
	region := f.position(n)
	ref := f.ref
	ref.Range = model.ByteRange{StartByte: region.start, EndByte: region.end}
	return ref
}

func (f *goFile) directSymbol(expr ast.Expr) string {
	var ident *ast.Ident
	switch n := expr.(type) {
	case *ast.Ident:
		ident = n
	case *ast.SelectorExpr:
		ident = n.Sel
	}
	if ident == nil {
		return ""
	}
	use := f.occurrences[f.position(ident)]
	if use.SymbolID == nil || use.Indirect || use.Dispatch == "interface" || use.ObjectKind != "function" && use.ObjectKind != "method" {
		return ""
	}
	return *use.SymbolID
}

func limitation(code string, refs ...model.SourceRef) model.Gap {
	return model.Gap{Code: code, Reason: "Static flow has a boundary in the selected capture and verified analysis subset.", SourceRefs: refs}
}
