// Package gohttp recognizes source-bound registrations in Go's standard mux.
package gohttp

import (
	"go/ast"
	"regexp"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
)

type Pattern struct {
	Method string
	Path   string
}

var literalSegment = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
var variableSegment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParsePattern handles explicit method and exact or single-segment wildcard
// paths in the Go 1.22+ ServeMux syntax. The adapter leaves host, methodless,
// subtree, catch-all and escaped patterns at the boundary.
func ParsePattern(raw string) (Pattern, bool) {
	method, path, ok := strings.Cut(raw, " ")
	if !ok || len(path) > 512 || !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return Pattern{}, false
	}
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
	default:
		return Pattern{}, false
	}
	seen := make(map[string]bool)
	for _, segment := range strings.Split(path[1:], "/") {
		if literalSegment.MatchString(segment) && segment != "." && segment != ".." {
			continue
		}
		if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' {
			return Pattern{}, false
		}
		name := segment[1 : len(segment)-1]
		if !variableSegment.MatchString(name) || seen[name] {
			return Pattern{}, false
		}
		seen[name] = true
	}
	return Pattern{Method: method, Path: path}, true
}

// Registration consumes an exact typed compiler callsite and its original
// syntax. A callable with the same spelling in another package never matches.
func Registration(target *golang.StdlibCallTarget, call *ast.CallExpr) (Pattern, ast.Expr, bool) {
	if target == nil || target.Package != "net/http" || (target.Receiver != "" && target.Receiver != "ServeMux") ||
		(target.Name != "HandleFunc" && target.Name != "Handle") {
		return Pattern{}, nil, false
	}
	if target.Name != "HandleFunc" || len(call.Args) != 2 {
		return Pattern{}, nil, true
	}
	literal, ok := ast.Unparen(call.Args[0]).(*ast.BasicLit)
	if !ok {
		return Pattern{}, nil, true
	}
	text, err := strconv.Unquote(literal.Value)
	if err != nil {
		return Pattern{}, nil, true
	}
	pattern, supported := ParsePattern(text)
	if !supported {
		return Pattern{}, nil, true
	}
	return pattern, ast.Unparen(call.Args[1]), true
}
