package flowview

import (
	"go/ast"
	"go/token"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type transaction struct {
	group, step string
	begin       model.SourceRef
}

// transactionSite groups calls only for a single, never reassigned local Tx
// introduced by Begin/BeginTx. The link describes the receiver identity.
// Execution and persistence remain unknown even on a commit call site.
func (p *projector) transactionSite(fn function, call *ast.CallExpr, target *golang.StdlibCallTarget, site *SQLSite) {
	if site.StepID == "" {
		return
	}
	if site.Operation == "BEGIN" {
		id := stableTransactionVariable(fn, call)
		if id == "" {
			p.note(site.StepID, "FLOW_TRANSACTION_RECEIVER_UNRESOLVED", site.Source)
			return
		}
		site.TransactionGroupID = digest("static-transaction-v1", fn.id, id, site.StepID)
		p.transactions[fn.id][id] = transaction{group: site.TransactionGroupID, step: site.StepID, begin: site.Source}
		return
	}
	if target.Receiver != "Tx" {
		return
	}
	selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		p.note(site.StepID, "FLOW_TRANSACTION_RECEIVER_UNRESOLVED", site.Source)
		return
	}
	receiver, ok := ast.Unparen(selector.X).(*ast.Ident)
	if !ok {
		p.note(site.StepID, "FLOW_TRANSACTION_RECEIVER_UNRESOLVED", site.Source)
		return
	}
	use := fn.file.occurrences[fn.file.position(receiver)]
	if use.SymbolID == nil {
		p.note(site.StepID, "FLOW_TRANSACTION_RECEIVER_UNRESOLVED", site.Source)
		return
	}
	group := p.transactions[fn.id][*use.SymbolID]
	if group.group == "" {
		p.note(site.StepID, "FLOW_TRANSACTION_RECEIVER_UNRESOLVED", site.Source)
		return
	}
	site.TransactionGroupID = group.group
	if edge := p.edge(group.step, site.StepID, "transaction_link", &site.Source, "type_resolved"); edge != nil {
		edge.EvidenceRefs[0].SourceRefs = append(edge.EvidenceRefs[0].SourceRefs, group.begin)
	}
}

func stableTransactionVariable(fn function, begin *ast.CallExpr) string {
	id := ""
	for _, stmt := range fn.decl.Body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 ||
			ast.Unparen(assign.Rhs[0]) != begin {
			continue
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			break
		}
		definition := fn.file.definitions[fn.file.position(lhs)]
		if definition.SymbolID != nil && definition.ObjectKind == "variable" {
			id = *definition.SymbolID
		}
		break
	}
	if id == "" {
		return ""
	}

	allowed := make(map[position]bool)
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		proof := fn.file.callees[fn.file.position(call.Fun)].StdlibTarget
		selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if proof != nil && proof.Package == "database/sql" && proof.Receiver == "Tx" && ok {
			if receiver, ok := ast.Unparen(selector.X).(*ast.Ident); ok {
				allowed[fn.file.position(receiver)] = true
			}
		}
		return true
	})
	for at, use := range fn.file.occurrences {
		if use.SymbolID != nil && *use.SymbolID == id && !allowed[at] {
			// An escape, reassignment or function-value use requires alias
			// analysis beyond the receiver-only subset.
			return ""
		}
	}
	return id
}
