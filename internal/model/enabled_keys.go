package model

import (
	"errors"
	"fmt"
)

var ErrUnsupportedCapability = errors.New("unsupported capability")

// RequireRegisteredKey checks a capability/node/edge/rule key against an
// explicit enabled-provider allowlist. The caller must derive the list from
// a trusted, versioned provider; an arbitrary client list grants no authority.
func RequireRegisteredKey(key string, enabled []string) error {
	if !validID(key) || enabled == nil || len(enabled) > 4096 {
		return fmt.Errorf("invalid registry input")
	}
	seen := make(map[string]struct{}, len(enabled))
	found := false
	for _, value := range enabled {
		if !validID(value) {
			return fmt.Errorf("invalid registry key")
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate registry key")
		}
		seen[value] = struct{}{}
		if value == key {
			found = true
		}
	}
	if !found {
		return ErrUnsupportedCapability
	}
	return nil
}
