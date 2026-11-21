package schema

import (
	"encoding/json"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
)

type declaration struct {
	node      *node
	entity    contracts.Entity
	direction string
}

func pointerPart(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

func (a *analyzer) registerSchema(doc *document, pointer, direction string, data *node) error {
	if data == nil || data.object == nil {
		return a.gap("CONTRACT_SCHEMA_FORM_UNSUPPORTED", data.ref(doc))
	}
	item, err := a.entity(doc, "json_schema", "schema", doc.Ref.Path+"#"+pointer, data.ref(doc))
	if err != nil {
		return err
	}
	doc.schemas[pointer] = &declaration{node: data, entity: item, direction: direction}
	if defs := data.get("$defs"); defs != nil {
		if scopedURI(data) || doc.dialect == "openapi-3.0" || defs.object == nil {
			return a.gap("CONTRACT_SCHEMA_DEFS_UNSUPPORTED", defs.ref(doc))
		}
		for _, name := range defs.keys() {
			if err := a.registerSchema(doc, pointer+"/$defs/"+pointerPart(name), "bidirectional", defs.get(name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func scopedURI(data *node) bool {
	return data.get("$id") != nil || data.get("$anchor") != nil || data.get("$dynamicAnchor") != nil ||
		data.get("$dynamicRef") != nil
}

// capturePath canonicalizes a relative member name without filesystem access.
// In protobuf, imports are rooted at the one declared capture root.
func capturePath(base, relative string) (string, bool) {
	if relative == "" || strings.HasPrefix(relative, "/") || strings.ContainsAny(relative, `\:%`) ||
		strings.IndexFunc(relative, unicode.IsControl) >= 0 {
		return "", false
	}
	name := path.Join(base, relative)
	return name, name != "." && name != ".." && !strings.HasPrefix(name, "../")
}

func (a *analyzer) target(doc *document, ref *node) (*document, *declaration, string) {
	text := ref.text()
	if len(text) == 0 || len(text) > 4096 {
		return nil, nil, "CONTRACT_REF_UNSUPPORTED"
	}
	value, err := url.Parse(text)
	if err != nil {
		return nil, nil, "CONTRACT_REF_UNSUPPORTED"
	}
	if value.IsAbs() || value.Host != "" || strings.HasPrefix(text, "//") {
		return nil, nil, "CONTRACT_REF_REMOTE"
	}
	if value.RawQuery != "" || value.ForceQuery || value.Fragment != "" && !strings.HasPrefix(value.Fragment, "/") {
		return nil, nil, "CONTRACT_REF_UNSUPPORTED"
	}
	to := doc
	if value.Path != "" {
		name, ok := capturePath(path.Dir(doc.Ref.Path), value.Path)
		if !ok {
			return nil, nil, "CONTRACT_REF_OUTSIDE_CAPTURE"
		}
		to = a.files[name]
		if to == nil {
			return nil, nil, "CONTRACT_REF_OUTSIDE_CAPTURE"
		}
	}
	if to.dialect != doc.dialect || to.Version != doc.Version {
		return nil, nil, "CONTRACT_REF_VERSION_MISMATCH"
	}
	decl := to.schemas[value.Fragment]
	if decl == nil {
		return nil, nil, "CONTRACT_REF_UNRESOLVED"
	}
	return to, decl, ""
}

// refClosure follows supported schema locations only. No declaration can be
// treated as a resolved reference through an ambiguous URI scope or a cycle.
func (a *analyzer) refClosure(doc *document, data *node, active map[*node]bool, depth int) (string, error) {
	if err := a.step(); err != nil {
		return "", err
	}
	if active[data] {
		return "CONTRACT_REF_CYCLE", nil
	}
	if depth >= maxDepth {
		return "CONTRACT_REF_DEPTH", nil
	}
	if data.object == nil || scopedURI(data) || data != doc.root && data.get("$schema") != nil {
		return "CONTRACT_REF_UNSUPPORTED", nil
	}
	active[data] = true
	defer delete(active, data)
	if ref := data.get("$ref"); ref != nil {
		for _, key := range data.keys() {
			switch key {
			case "$ref", "$schema", "$defs", "title", "description":
			default:
				return "CONTRACT_REF_SIBLINGS_UNSUPPORTED", nil
			}
		}
		to, decl, gap := a.target(doc, ref)
		if gap != "" {
			return gap, nil
		}
		return a.refClosure(to, decl.node, active, depth+1)
	}
	properties := data.get("properties")
	for _, key := range properties.keys() {
		if gap, err := a.refClosure(doc, properties.get(key), active, depth+1); gap != "" || err != nil {
			return gap, err
		}
	}
	if item := data.get("items"); item != nil {
		return a.refClosure(doc, item, active, depth+1)
	}
	return "", nil
}

func (a *analyzer) emitSchema(doc *document, decl *declaration, data *node, prefix string, depth int) error {
	if err := a.step(); err != nil {
		return err
	}
	if depth >= maxDepth {
		return a.gap("CONTRACT_SCHEMA_DEPTH", data.ref(doc))
	}
	if data.object == nil || scopedURI(data) || data != doc.root && data.get("$schema") != nil {
		return a.gap("CONTRACT_SCHEMA_SCOPE_UNSUPPORTED", data.ref(doc))
	}
	unresolved := false
	if ref := data.get("$ref"); ref != nil {
		gap, err := a.refClosure(doc, data, make(map[*node]bool), 0)
		if err != nil {
			return err
		}
		if gap != "" {
			if err := a.gap(gap, ref.ref(doc)); err != nil {
				return err
			}
			unresolved = true
		} else {
			_, target, _ := a.target(doc, ref)
			if err := a.link(decl.entity.Ref, target.entity.Ref, "schema_ref", ref.ref(doc)); err != nil {
				return err
			}
		}
	}
	for _, key := range data.keys() {
		switch key {
		case "$schema", "$defs", "$ref", "type", "properties", "items", "required", "description",
			"title", "examples", "default", "nullable", "x-unit", "x-currency", "x-precision":
		default:
			if err := a.gap("CONTRACT_SCHEMA_KEYWORD_UNSUPPORTED", data.get(key).ref(doc)); err != nil {
				return err
			}
		}
	}
	if prefix != "" {
		typ, nullable := fieldType(data, doc.dialect)
		if unresolved {
			return a.field(decl.entity.Ref, prefix, decl.direction, "unresolved", nil, data.ref(doc))
		}
		if typ == "unknown" && data.get("type") != nil || doc.dialect != "openapi-3.0" && data.get("nullable") != nil {
			if err := a.gap("CONTRACT_SCHEMA_TYPE_UNSUPPORTED", data.ref(doc)); err != nil {
				return err
			}
		}
		if err := a.field(decl.entity.Ref, prefix, decl.direction, typ, nullable, data.ref(doc)); err != nil {
			return err
		}
		field := &a.out.Batch.Fields[len(a.out.Batch.Fields)-1]
		if unit := data.get("x-unit").text(); boundedText(unit, 64) {
			field.Unit = &unit
		}
		if currency := data.get("x-currency").text(); len(currency) == 3 &&
			strings.IndexFunc(currency, func(r rune) bool { return r < 'A' || r > 'Z' }) < 0 {
			field.Currency = &currency
		}
		if p := data.get("x-precision"); p != nil {
			if n, ok := p.value.(json.Number); ok {
				if precision, err := n.Int64(); err == nil && precision >= 0 && precision <= 9007199254740991 {
					field.Precision = &precision
				}
			}
		}
	}
	if unresolved {
		return nil
	}
	if properties := data.get("properties"); properties != nil {
		if properties.object == nil {
			return a.gap("CONTRACT_SCHEMA_PROPERTIES_UNSUPPORTED", properties.ref(doc))
		}
		for _, key := range properties.keys() {
			if err := a.emitSchema(doc, decl, properties.get(key), prefix+"/properties/"+pointerPart(key), depth+1); err != nil {
				return err
			}
		}
	}
	if items := data.get("items"); items != nil {
		return a.emitSchema(doc, decl, items, prefix+"/items", depth+1)
	}
	return nil
}

func fieldType(n *node, dialect string) (string, *bool) {
	types := n.get("type")
	if types == nil {
		if n.get("$ref") != nil {
			return "schema_ref", nil
		}
		return "unknown", nil
	}
	selected := types.text()
	nullable := selected == "null"
	if types.items != nil && dialect != "openapi-3.0" {
		if len(types.items) != 2 {
			return "unknown", nil
		}
		left, right := types.items[0].text(), types.items[1].text()
		if left == "null" && right != "null" {
			left, right = right, left
		}
		if right != "null" || left == "null" {
			return "unknown", nil
		}
		selected, nullable = left, true
	}
	if !slices.Contains([]string{"object", "array", "string", "number", "integer", "boolean", "null"}, selected) {
		return "unknown", nil
	}
	if dialect == "openapi-3.0" {
		if selected == "null" {
			return "unknown", nil
		}
		if n := n.get("nullable"); n != nil {
			v, ok := n.value.(bool)
			if !ok {
				return selected, nil
			}
			nullable = v
		}
	}
	return selected, &nullable
}
