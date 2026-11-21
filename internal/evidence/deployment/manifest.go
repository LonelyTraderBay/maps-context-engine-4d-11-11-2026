package deployment

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const (
	MaxEvidenceBytes = 1 << 20
	MaxServices      = 64
	MaxItems         = 64
)

type Manifest struct {
	Kind          string    `json:"kind"`
	SchemaVersion int       `json:"schema_version"`
	Services      []Service `json:"services"`
}

type Service struct {
	ID          string       `json:"id"`
	Version     string       `json:"version"`
	Owner       string       `json:"owner"`
	Ports       []Port       `json:"ports"`
	Config      []ConfigKey  `json:"config"`
	Permissions []Permission `json:"permissions"`
}

type Port struct {
	Name     string `json:"name"`
	Number   int    `json:"number"`
	Protocol string `json:"protocol"`
}

// ConfigKey carries only a name, type and owner. The closed input contract
// has no field for a variable value or expression.
type ConfigKey struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Owner string `json:"owner"`
}

type Permission struct {
	Principal string `json:"principal"`
	Action    string `json:"action"`
	Resource  string `json:"resource"`
}

var label = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)

func decode(raw []byte, output any) error {
	if len(raw) == 0 || len(raw) > MaxEvidenceBytes || scanner.CheckContent(raw) != nil {
		return ErrEvidence
	}
	input, err := model.DecodeIdentityJSON(raw, MaxEvidenceBytes)
	if err != nil || json.Unmarshal(raw, output) != nil {
		return ErrEvidence
	}
	exact, err := json.Marshal(output)
	if err != nil {
		return ErrEvidence
	}
	// A typed round-trip rejects missing, extra and case-folded keys. The
	// identity decoder already checked duplicate keys and preserves arrays.
	canonical, err := model.DecodeIdentityJSON(exact, MaxEvidenceBytes)
	if err != nil || !reflect.DeepEqual(input, canonical) {
		return ErrEvidence
	}
	return nil
}

func manifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := decode(raw, &m); err != nil || m.Kind != "deployment-map" || m.SchemaVersion != 1 ||
		len(m.Services) < 1 || len(m.Services) > MaxServices {
		return Manifest{}, ErrEvidence
	}
	seen := make(map[string]bool, len(m.Services))
	for i := range m.Services {
		s := &m.Services[i]
		if !label.MatchString(s.ID) || seen[s.ID] || !label.MatchString(s.Owner) || !label.MatchString(s.Version) ||
			!validatePorts(s.Ports) || !validateConfig(s.Config) || !validatePermissions(s.Permissions) {
			return Manifest{}, ErrEvidence
		}
		seen[s.ID] = true
	}
	slices.SortFunc(m.Services, func(a, b Service) int { return strings.Compare(a.ID, b.ID) })
	return m, nil
}

func validatePorts(rows []Port) bool {
	if rows == nil || len(rows) > MaxItems {
		return false
	}
	slices.SortFunc(rows, func(a, b Port) int { return strings.Compare(a.Name, b.Name) })
	for i, row := range rows {
		if !label.MatchString(row.Name) || row.Number < 1 || row.Number > 65535 ||
			(row.Protocol != "tcp" && row.Protocol != "udp") || i > 0 && rows[i-1].Name == row.Name {
			return false
		}
	}
	return true
}

func validateConfig(rows []ConfigKey) bool {
	if rows == nil || len(rows) > MaxItems {
		return false
	}
	slices.SortFunc(rows, func(a, b ConfigKey) int { return strings.Compare(a.Name, b.Name) })
	for i, row := range rows {
		if !label.MatchString(row.Name) || !label.MatchString(row.Owner) || i > 0 && rows[i-1].Name == row.Name {
			return false
		}
		switch row.Kind {
		case "config", "feature_flag", "secret_reference":
		default:
			return false
		}
	}
	return true
}

func validatePermissions(rows []Permission) bool {
	if rows == nil || len(rows) > MaxItems {
		return false
	}
	slices.SortFunc(rows, func(a, b Permission) int {
		if a.Principal != b.Principal {
			return strings.Compare(a.Principal, b.Principal)
		}
		if a.Action != b.Action {
			return strings.Compare(a.Action, b.Action)
		}
		return strings.Compare(a.Resource, b.Resource)
	})
	for i, row := range rows {
		if !label.MatchString(row.Principal) || !label.MatchString(row.Action) || !label.MatchString(row.Resource) || i > 0 && rows[i-1] == row {
			return false
		}
	}
	return true
}
