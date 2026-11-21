package schema

import (
	"bytes"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type protoToken struct {
	text       string
	start, end int
}

type protoFile struct {
	pkg            string
	imports        []protoToken
	messages       []*protoMessage
	services       []*protoService
	visible        []*document
	importState    string
	ambiguousScope bool
}

type protoMessage struct {
	name   string
	ref    model.SourceRef
	fields []protoField
	entity contracts.Entity
}

type protoField struct {
	name, typ, label string
	number           uint64
	ref              model.SourceRef
}

// ProtoField preserves the declared wire number and cardinality for a
// field whose common DomainAdapter record describes its type.
type ProtoField struct {
	Entity      model.EntityRef
	Name        string
	Number      uint64
	Cardinality string
	Declaration model.SourceRef
}

type protoService struct {
	name    string
	ref     model.SourceRef
	methods []protoMethod
}

type protoMethod struct {
	name, request, response string
	ref                     model.SourceRef
}

type protoParser struct {
	tokens []protoToken
	offset int
	source model.SourceRef
}

func (p *protoParser) take() protoToken {
	if p.offset >= len(p.tokens) {
		return protoToken{}
	}
	t := p.tokens[p.offset]
	p.offset++
	return t
}

func (p *protoParser) is(text string) bool {
	return p.offset < len(p.tokens) && p.tokens[p.offset].text == text
}

func (p *protoParser) accept(text string) bool {
	if !p.is(text) {
		return false
	}
	p.take()
	return true
}

func protoName(text string) bool {
	for i, c := range text {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return len(text) > 0 && len(text) <= 128
}

func (p *protoParser) qualified() string {
	var parts []string
	absolute := p.accept(".")
	for {
		next := p.take().text
		if !protoName(next) || len(parts) >= 16 {
			return ""
		}
		parts = append(parts, next)
		if !p.accept(".") {
			break
		}
	}
	name := strings.Join(parts, ".")
	if len(name) > 96 {
		return ""
	}
	if absolute {
		return "." + name
	}
	return name
}

func protoString(t protoToken) string {
	if len(t.text) < 2 || t.text[0] != '"' || t.text[0] != t.text[len(t.text)-1] {
		return ""
	}
	value := t.text[1 : len(t.text)-1]
	if strings.ContainsAny(value, "\\\\\r\n") {
		return ""
	}
	return value
}

func (p *protoParser) reference(first protoToken) model.SourceRef {
	return span(p.source, first.start, p.tokens[p.offset-1].end)
}

func (a *analyzer) readProto(doc *document) error {
	var s scanner.Scanner
	s.Init(bytes.NewReader(doc.Bytes))
	s.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanStrings | scanner.ScanComments | scanner.SkipComments
	bad := false
	s.Error = func(*scanner.Scanner, string) { bad = true }
	p := protoParser{source: doc.Ref}
	for r := s.Scan(); r != scanner.EOF; r = s.Scan() {
		if err := a.step(); err != nil {
			return err
		}
		text := s.TokenText()
		p.tokens = append(p.tokens, protoToken{text: text, start: s.Position.Offset, end: s.Position.Offset + len(text)})
	}
	if bad || !p.accept("syntax") || !p.accept("=") || protoString(p.take()) != "proto3" || !p.accept(";") {
		return a.gap("CONTRACT_PROTO_SYNTAX_UNSUPPORTED", doc.Ref)
	}
	parsed := &protoFile{}
	for p.offset < len(p.tokens) {
		first := p.take()
		if parsed.read(&p, first) {
			continue
		}
		return a.gap("CONTRACT_PROTO_DECLARATION_UNSUPPORTED", span(doc.Ref, first.start, first.end))
	}
	if parsed.pkg == "" {
		return a.gap("CONTRACT_PROTO_PACKAGE_REQUIRED", doc.Ref)
	}
	doc.proto, doc.dialect = parsed, "proto3"
	names := make(map[string]int)
	for _, msg := range parsed.messages {
		names[msg.name]++
	}
	for _, service := range parsed.services {
		names[service.name]++
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		if names[name] != 1 {
			parsed.ambiguousScope = true
			return a.gap("CONTRACT_PROTO_AMBIGUOUS_DECLARATION", doc.Ref)
		}
	}
	for _, msg := range parsed.messages {
		entity, err := a.entity(doc, "protobuf", "proto_message", parsed.pkg+"."+msg.name, msg.ref)
		if err != nil {
			return err
		}
		msg.entity = entity
	}
	return nil
}

func (f *protoFile) read(p *protoParser, first protoToken) bool {
	switch first.text {
	case "package":
		if f.pkg != "" {
			return false
		}
		f.pkg = p.qualified()
		return f.pkg != "" && !strings.HasPrefix(f.pkg, ".") && p.accept(";")
	case "import":
		token := p.take()
		if protoString(token) == "" || !p.accept(";") {
			return false
		}
		f.imports = append(f.imports, token)
		return true
	case "option":
		return p.accept("go_package") && p.accept("=") && protoString(p.take()) != "" && p.accept(";")
	case "message":
		msg, ok := p.message(first)
		if ok {
			f.messages = append(f.messages, msg)
		}
		return ok
	case "service":
		service, ok := p.service(first)
		if ok {
			f.services = append(f.services, service)
		}
		return ok
	default:
		return false
	}
}

func (p *protoParser) message(first protoToken) (*protoMessage, bool) {
	msg := &protoMessage{name: p.take().text}
	if !protoName(msg.name) || !p.accept("{") {
		return nil, false
	}
	seenNames, seenNumbers := make(map[string]bool), make(map[uint64]bool)
	for !p.accept("}") {
		if p.offset == len(p.tokens) {
			return nil, false
		}
		begin := p.tokens[p.offset]
		label := ""
		if p.is("optional") || p.is("repeated") {
			label = p.take().text
		}
		typ, name := p.qualified(), p.take().text
		if typ == "" || !protoName(name) || !p.accept("=") {
			return nil, false
		}
		number, err := strconv.ParseUint(p.take().text, 10, 29)
		if err != nil || number == 0 || number >= 19000 && number <= 19999 || seenNames[name] ||
			seenNumbers[number] || !p.accept(";") {
			return nil, false
		}
		seenNames[name], seenNumbers[number] = true, true
		msg.fields = append(msg.fields, protoField{name: name, typ: typ, label: label, number: number, ref: p.reference(begin)})
	}
	msg.ref = p.reference(first)
	return msg, true
}

func (p *protoParser) service(first protoToken) (*protoService, bool) {
	service := &protoService{name: p.take().text}
	if !protoName(service.name) || !p.accept("{") {
		return nil, false
	}
	seen := make(map[string]bool)
	for !p.accept("}") {
		begin := p.take()
		name := p.take().text
		if begin.text != "rpc" || !protoName(name) || seen[name] || !p.accept("(") {
			return nil, false
		}
		request := p.qualified()
		if request == "" || !p.accept(")") || !p.accept("returns") || !p.accept("(") {
			return nil, false
		}
		response := p.qualified()
		if response == "" || !p.accept(")") || !p.accept(";") {
			return nil, false
		}
		seen[name] = true
		service.methods = append(service.methods, protoMethod{name: name, request: request, response: response, ref: p.reference(begin)})
	}
	service.ref = p.reference(first)
	return service, true
}

func (a *analyzer) protoImports(doc *document, active map[*document]bool, depth int) (string, error) {
	if err := a.step(); err != nil {
		return "", err
	}
	if doc.proto == nil || doc.proto.ambiguousScope {
		return "CONTRACT_PROTO_IMPORT_UNRESOLVED", nil
	}
	if active[doc] {
		return "CONTRACT_PROTO_IMPORT_CYCLE", nil
	}
	if depth >= maxDepth {
		return "CONTRACT_PROTO_IMPORT_DEPTH", nil
	}
	f := doc.proto
	if f.importState != "" {
		return f.importState, nil
	}
	active[doc] = true
	defer delete(active, doc)
	visible := []*document{doc}
	var issue string
	for _, t := range f.imports {
		name, local := capturePath(".", protoString(t))
		to := a.files[name]
		var gap string
		switch {
		case !local || to == nil:
			gap = "CONTRACT_PROTO_IMPORT_OUTSIDE_CAPTURE"
		case to.Version != doc.Version || to.dialect != doc.dialect:
			gap = "CONTRACT_PROTO_IMPORT_VERSION_MISMATCH"
		case slices.Contains(visible, to) && to != doc:
			gap = "CONTRACT_PROTO_IMPORT_DUPLICATE"
		default:
			var err error
			gap, err = a.protoImports(to, active, depth+1)
			if err != nil {
				return "", err
			}
			switch gap {
			case "ok":
				visible = append(visible, to)
				gap = ""
			case "":
				gap = "CONTRACT_PROTO_IMPORT_UNRESOLVED"
			}
		}
		if gap != "" {
			if err := a.gap(gap, span(doc.Ref, t.start, t.end)); err != nil {
				return "", err
			}
			issue = gap
		}
	}
	f.visible, f.importState = visible, "ok"
	if issue != "" {
		f.importState = issue
	}
	return f.importState, nil
}

func protoTarget(doc *document, name string) *protoMessage {
	if doc.proto.importState != "ok" {
		return nil
	}
	key, absolute := strings.CutPrefix(name, ".")
	if !absolute {
		if strings.Contains(key, ".") {
			return nil // Relative multi-segment lookup requires a richer resolver.
		}
		key = doc.proto.pkg + "." + key
	}
	var result *protoMessage
	for _, candidate := range doc.proto.visible {
		for _, m := range candidate.proto.messages {
			if candidate.proto.pkg+"."+m.name == key {
				if result != nil {
					return nil
				}
				result = m
			}
		}
	}
	return result
}

var protoScalar = []string{"double", "float", "int32", "int64", "uint32", "uint64", "sint32", "sint64",
	"fixed32", "fixed64", "sfixed32", "sfixed64", "bool", "string", "bytes"}

func (a *analyzer) emitProto(doc *document) error {
	f := doc.proto
	if f.ambiguousScope {
		return nil
	}
	if _, err := a.protoImports(doc, make(map[*document]bool), 0); err != nil {
		return err
	}
	for _, msg := range f.messages {
		for _, field := range msg.fields {
			typ := field.typ
			if !slices.Contains(protoScalar, typ) {
				if target := protoTarget(doc, field.typ); target != nil {
					if err := a.link(msg.entity.Ref, target.entity.Ref, "proto_field_type", field.ref); err != nil {
						return err
					}
				} else {
					typ = "unresolved"
					if err := a.gap("CONTRACT_PROTO_TYPE_UNRESOLVED", field.ref); err != nil {
						return err
					}
				}
			}
			if field.label != "" {
				typ = field.label + " " + typ
			}
			if err := a.field(msg.entity.Ref, field.name, "bidirectional", typ, nil, field.ref); err != nil {
				return err
			}
			cardinality := field.label
			if cardinality == "" {
				cardinality = "singular"
			}
			a.out.ProtoFields = append(a.out.ProtoFields, ProtoField{Entity: msg.entity.Ref, Name: field.name,
				Number: field.number, Cardinality: cardinality, Declaration: field.ref})
		}
	}
	for _, service := range f.services {
		identifier := f.pkg + "." + service.name
		entity, err := a.entity(doc, "protobuf", "proto_service", identifier, service.ref)
		if err != nil {
			return err
		}
		for _, method := range service.methods {
			rpc, err := a.entity(doc, "protobuf", "proto_rpc", identifier+"/"+method.name, method.ref)
			if err != nil {
				return err
			}
			if err := a.link(entity.Ref, rpc.Ref, "declares_rpc", method.ref); err != nil {
				return err
			}
			for _, direction := range []struct{ rel, typ string }{{"proto_request", method.request}, {"proto_response", method.response}} {
				target := protoTarget(doc, direction.typ)
				if target == nil {
					if err := a.gap("CONTRACT_PROTO_TYPE_UNRESOLVED", method.ref); err != nil {
						return err
					}
				} else if err := a.link(rpc.Ref, target.entity.Ref, direction.rel, method.ref); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
