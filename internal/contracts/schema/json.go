package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var errJSON = errors.New("unsupported or invalid contract JSON")

type node struct {
	start, end int
	value      any
	object     map[string]*node
	items      []*node
}

func (n *node) get(key string) *node {
	if n == nil {
		return nil
	}
	return n.object[key]
}

func (n *node) keys() []string {
	if n == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(n.object))
}

func (n *node) text() string {
	if n == nil {
		return ""
	}
	value, _ := n.value.(string)
	return value
}

func (n *node) ref(doc *document) model.SourceRef {
	if n == nil {
		return doc.Ref
	}
	return span(doc.Ref, n.start, n.end)
}

func (a *analyzer) readJSON(dec *json.Decoder, raw []byte, depth int) (*node, error) {
	if depth > maxDepth {
		return nil, ErrLimit
	}
	if err := a.step(); err != nil {
		return nil, err
	}
	start := int(dec.InputOffset())
	for start < len(raw) && strings.ContainsRune(" \t\r\n,:", rune(raw[start])) {
		start++
	}
	item, err := dec.Token()
	if err != nil {
		return nil, errJSON
	}
	result := &node{start: start, value: item}
	switch item {
	case json.Delim('{'):
		result.object = make(map[string]*node)
		for dec.More() {
			field, err := dec.Token()
			name, ok := field.(string)
			if err != nil || !ok || result.object[name] != nil {
				return nil, errJSON
			}
			child, err := a.readJSON(dec, raw, depth+1)
			if err != nil {
				return nil, err
			}
			result.object[name] = child
		}
		if item, err := dec.Token(); err != nil || item != json.Delim('}') {
			return nil, errJSON
		}
	case json.Delim('['):
		result.items = []*node{}
		for dec.More() {
			child, err := a.readJSON(dec, raw, depth+1)
			if err != nil {
				return nil, err
			}
			result.items = append(result.items, child)
		}
		if item, err := dec.Token(); err != nil || item != json.Delim(']') {
			return nil, errJSON
		}
	}
	result.end = int(dec.InputOffset())
	return result, nil
}

func (a *analyzer) read(doc *document) error {
	if doc.Kind == "protobuf" {
		return a.readProto(doc)
	}
	if doc.Kind != "openapi" && doc.Kind != "json_schema" {
		return ErrCapture
	}
	dec := json.NewDecoder(bytes.NewReader(doc.Bytes))
	dec.UseNumber()
	root, err := a.readJSON(dec, doc.Bytes, 0)
	if err == nil {
		if _, trailing := dec.Token(); trailing != io.EOF || root.object == nil {
			err = errJSON
		}
	}
	if err != nil {
		if errors.Is(err, errJSON) {
			return a.gap("CONTRACT_JSON_UNSUPPORTED", doc.Ref)
		}
		return err
	}
	doc.root = root
	if doc.Kind == "openapi" {
		return a.readOpenAPI(doc)
	}
	const draft = "https://json-schema.org/draft/2020-12/schema"
	if root.get("$schema").text() != draft {
		return a.gap("CONTRACT_DIALECT_UNSUPPORTED", root.get("$schema").ref(doc))
	}
	doc.dialect = "json-schema-2020-12"
	return a.registerSchema(doc, "", "bidirectional", root)
}

func (a *analyzer) emit(doc *document) error {
	if doc.dialect == "" {
		return nil
	}
	if doc.Kind == "protobuf" {
		return a.emitProto(doc)
	}
	for _, p := range slices.Sorted(maps.Keys(doc.schemas)) {
		decl := doc.schemas[p]
		if err := a.emitSchema(doc, decl, decl.node, "", 0); err != nil {
			return err
		}
	}
	return nil
}
