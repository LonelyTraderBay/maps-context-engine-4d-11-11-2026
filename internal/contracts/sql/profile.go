// Package sql extracts source-bound MySQL declarations and lineage.
package sql

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrProfile = errors.New("unsupported SQL analysis profile")
	ErrSource  = errors.New("invalid captured SQL source")
	ErrLimit   = errors.New("SQL analysis limit exceeded")
)

// Profile describes the SQL bytes supplied for analysis. Engine is a
// declared default. Explicit table engine clauses are recorded separately.
type Profile struct {
	Dialect     string  `json:"dialect"`
	Version     string  `json:"version"`
	Engine      string  `json:"engine"`
	SQLMode     string  `json:"sql_mode"`
	Environment string  `json:"environment"`
	Namespace   string  `json:"namespace"`
	Schema      string  `json:"schema"`
	ServiceID   *string `json:"service_id"`
}

type Source struct {
	Kind string // schema, migration, or literal
	Ref  model.SourceRef
	Data []byte // complete captured file, even when Ref selects a SQL fragment
}

type Analysis struct {
	ProfileDigest string
	Batch         contracts.Batch
	Coverage      model.Coverage
}

var (
	mysqlVersion = regexp.MustCompile(`^8\.(?:0|4)(?:\.[0-9]{1,3})?$`)
	sqlNamespace = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
)

func (p Profile) normalized() (Profile, string, error) {
	p.Engine = engineName(p.Engine)
	if p.Dialect != "mysql" || !mysqlVersion.MatchString(p.Version) || p.Engine == "" ||
		!sqlNamespace.MatchString(p.Environment) || !sqlNamespace.MatchString(p.Namespace) ||
		p.Schema != "" && !validIdentifier(p.Schema) ||
		p.ServiceID != nil && !validProfileText(*p.ServiceID, 192) {
		return Profile{}, "", ErrProfile
	}
	switch p.SQLMode {
	case "default", "ansi_quotes", "no_backslash_escapes":
	default:
		return Profile{}, "", ErrProfile
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Profile{}, "", err
	}
	value, err := model.DecodeIdentityJSON(raw, 8192)
	if err != nil {
		return Profile{}, "", ErrProfile
	}
	_, digest, err := model.EncodeCanonical("mysql-static-profile-v1", value, 8192)
	return p, digest, err
}

func engineName(name string) string {
	switch strings.ToLower(name) {
	case "innodb":
		return "innodb"
	case "ndb", "ndbcluster":
		return "ndb"
	case "unknown":
		return "unknown"
	default:
		return ""
	}
}

func validIdentifier(value string) bool {
	return validProfileText(value, 64)
}

func validProfileText(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
