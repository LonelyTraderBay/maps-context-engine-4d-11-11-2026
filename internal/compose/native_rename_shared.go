//go:build linux || windows

package compose

import (
	"path/filepath"
	"reflect"
	"slices"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func nativeRenameExecutable(policy model.PolicyConfig, name string) (model.AllowedExecutable, bool) {
	var selected model.AllowedExecutable
	for _, executable := range policy.AllowedExecutables {
		if filepath.Base(executable.Path) != name {
			continue
		}
		if selected.Path != "" || !filepath.IsAbs(executable.Path) || filepath.Clean(executable.Path) != executable.Path || !hash(executable.SHA256) {
			return model.AllowedExecutable{}, false
		}
		selected = executable
	}
	return selected, selected.Path != ""
}

// The loader defensively copies stage slices; nil and empty both mean no
// stages. Compare their contents while retaining every membership/config flag.
func sameNativeLoaderInventory(a, b inventory.FileMap) bool {
	if a.InputDigest != b.InputDigest || a.DeniedCount != b.DeniedCount || a.InvalidPathCount != b.InvalidPathCount || a.IgnoredCount != b.IgnoredCount || len(a.Entries) != len(b.Entries) {
		return false
	}
	for i, entry := range a.Entries {
		other := b.Entries[i]
		if !slices.Equal(entry.Stages, other.Stages) {
			return false
		}
		entry.Stages, other.Stages = nil, nil
		if !reflect.DeepEqual(entry, other) {
			return false
		}
	}
	return true
}
