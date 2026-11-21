package lsp

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var (
	ErrLSPAuthority   = errors.New("LSP authority or captured input cannot be verified")
	ErrLSPUnsupported = errors.New("secure LSP session unsupported on this platform")
)

// SessionConfig borrows an already authorized executable directory and an
// immutable set of Go documents. Check must independently verify the binary
// allowlist, principal/full scope, captured package closure, type completeness,
// current file hashes, scratch confinement and toolchain identity. It is called
// before and after the session; an always-true callback is only a fixture.
type SessionConfig struct {
	ServerBinRoot       *paths.Root
	ExpectedSHA256      string
	Environment         runtime.GoEnvironmentInput
	Documents           []CapturedDocument
	Check               func(context.Context) error
	Limiter             *SessionLimiter
	StartupTimeoutMs    int64  // zero selects the 30000ms component default
	RenameTimeoutMs     int64  // applies after startup; zero selects 30000ms
	WindowsContainerSID string // required for Windows sessions
}

func sessionTimeout(ms int64) (time.Duration, error) {
	if ms == 0 {
		ms = 30000
	}
	if ms < 1 || ms > int64((1<<63-1)/int64(time.Millisecond)) {
		return 0, ErrLSPAuthority
	}
	return time.Duration(ms) * time.Millisecond, nil
}
