// Package policy supplies conservative source-deny checks. It cannot detect
// every secret; callers must enforce it before persistence or output.
package policy

import (
	"bytes"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrDeniedPath    = errors.New("source path denied")
	ErrSecretBlocked = errors.New("source content blocked")
	ErrContentLimit  = errors.New("source content limit exceeded")
	ErrInvalidText   = errors.New("invalid UTF-8 source")
)

// CheckPath applies the built-in deny baseline before opening a source file.
// Additional user policy may narrow, never widen, this baseline.
func CheckPath(path string) error {
	if model.ValidateRelativePath(path, false) != nil {
		return ErrDeniedPath
	}
	parts := strings.Split(strings.ToLower(path), "/")
	for _, p := range parts {
		if p == ".git" || p == ".ssh" || p == ".aws" || p == ".gnupg" || p == ".kube" {
			return ErrDeniedPath
		}
	}
	base := parts[len(parts)-1]
	if base == ".env" || strings.HasPrefix(base, ".env.") && base != ".env.example" && base != ".env.sample" {
		return ErrDeniedPath
	}
	if base == "credentials" || base == "credentials.json" || base == "wallet.dat" || strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
		return ErrDeniedPath
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".kdbx", ".dmp", ".dump", ".dump.gz"} {
		if strings.HasSuffix(base, suffix) {
			return ErrDeniedPath
		}
	}
	return nil
}

// Scanner applies a bounded, deliberately finite set of high-confidence
// indicators. A clean result does not guarantee that content contains no secret.
type Scanner struct {
	maxBytes   int
	indicators []*regexp.Regexp
}

func NewScanner(maxBytes int) (*Scanner, error) {
	if maxBytes <= 0 || uint64(maxBytes) >= uint64(math.MaxInt64) {
		return nil, ErrContentLimit
	}
	expressions := []string{
		`-----BEGIN (?:RSA |EC |OPENSSH |DSA |ENCRYPTED )?PRIVATE KEY-----`,
		`(?i)\b(?:api[_-]?key|access[_-]?token|client[_-]?secret|private[_-]?key|password)\b\s*[:=]\s*["']?[A-Za-z0-9_+/=-]{16,}`,
	}
	scanner := &Scanner{maxBytes: maxBytes}
	for _, expr := range expressions {
		r, err := regexp.Compile(expr)
		if err != nil {
			return nil, err
		}
		scanner.indicators = append(scanner.indicators, r)
	}
	return scanner, nil
}
func (s *Scanner) CheckContent(content []byte) error {
	if s == nil || s.maxBytes <= 0 || len(content) > s.maxBytes {
		return ErrContentLimit
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return ErrInvalidText
	}
	for _, r := range s.indicators {
		if r.Match(content) {
			return ErrSecretBlocked
		}
	}
	return nil
}
