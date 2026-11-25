package compose

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func TestRouteEvidenceKeepsDirectSitesAndFlowIncludesEventContext(t *testing.T) {
	const route, wrapper, page = "routes.ts", "wrapper.tsx", "page.tsx"
	sources := map[string]index.PublishedFile{
		route:   {Bytes: []byte("const path = { path: '/home', Component: Wrapper };\n")},
		wrapper: {Bytes: []byte("export function Wrapper() { return <Page web />; }\n")},
		page:    {Bytes: []byte("const labels = ['Tiếp tục'];\n\n<Button onClick={start}>\n  {labels[step]}\n</Button>\n")},
	}
	span := func(file, text string) model.ByteRange {
		start := bytes.Index(sources[file].Bytes, []byte(text))
		if start < 0 {
			t.Fatalf("fixture has no %q in %s", text, file)
		}
		return model.ByteRange{StartByte: int64(start), EndByte: int64(start + len(text))}
	}
	render := span(wrapper, "Page")
	root := span(wrapper, "export function Wrapper() { return <Page web />; }")
	component, jsx, wrapperID, wrapperPath := "page", "jsx", "wrapper", wrapper
	pagePath := page
	decl := span(page, "const labels = ['Tiếp tục'];")
	labelRead := index.JSXVariableRead{Use: span(page, "labels[step]"), Declaration: model.SymbolFact{
		Path: &pagePath, Range: &decl,
	}}
	view := index.PublishedNeighborhood{Files: sources, Symbols: []model.SymbolFact{
		{SymbolID: wrapperID, Path: &wrapperPath, Range: &root},
	}, Relations: []model.RelationFact{
		{Kind: "ROUTE_COMPONENT", SourceSymbolID: "route", TargetSymbolID: &wrapperID, SitePath: route, Range: span(route, "Wrapper")},
		{Kind: "RENDERS_COMPONENT", SourceSymbolID: wrapperID, TargetSymbolID: &component, SitePath: wrapper, Range: render},
		{RelationID: "jsx-render", Kind: "RENDERS_JSX", SourceSymbolID: component, TargetSymbolID: &jsx, SitePath: page, Range: span(page, "Button")},
	}, JSXReads: map[string][]index.JSXVariableRead{"jsx-render": {labelRead}}}
	direct := appendLocateExcerpts(model.ScopeMember{}, view, wrapperID, 8, true)
	if len(direct) != 2 || direct[0].SourceRef.Path != route || direct[1].SourceRef.Path != wrapper {
		t.Fatalf("unexpected direct route evidence: %+v", direct)
	}
	full := appendLocateExcerpts(model.ScopeMember{}, view, wrapperID, 8, false)
	if len(full) < 4 || full[2].SourceRef.Path != page || !strings.Contains(full[2].Content, "{labels[step]}") ||
		full[3].StartLine != 1 || !strings.Contains(full[3].Content, "Tiếp tục") {
		t.Fatalf("flow lost the event or its label definition: %+v", full)
	}
}
