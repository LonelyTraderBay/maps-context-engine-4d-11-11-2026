package runtime

import (
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const runtimeConfigName = "runtime.json"
const runtimeConfigLock = ".runtime.json.lock"
const runtimeConfigPendingName = ".runtime.json.pending"

// One lock and one pending slot are shared by all approved config files in a
// directory. That also serializes aliases on a case-insensitive filesystem.
// The caller selects and authorizes the parent root, never an arbitrary path.
func validRuntimeConfigName(name string) bool {
	return len(name) <= 255 && !strings.Contains(name, "/") &&
		model.ValidateRelativePath(name, false) == nil &&
		!strings.EqualFold(name, runtimeConfigLock) &&
		!strings.EqualFold(name, runtimeConfigPendingName)
}

// ConfigWriteResult separates the known commit state from subsequent errors.
// State is "not_applied", "applied", or "unknown"; callers must not blindly
// retry an applied or unknown result.
type ConfigWriteResult = ports.RuntimeConfigWriteResult
