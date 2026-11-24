package app

import "errors"

// Administrative adapters may return these errors only when the mutation is
// known not to have committed. Other failures require outcome reconciliation.
var (
	ErrAdminDenied      = errors.New("administrative access denied")
	ErrAdminUnavailable = errors.New("administrative scope unavailable")
	ErrAdminConflict    = errors.New("administrative revision conflict")
	ErrAdminUnsupported = errors.New("administrative capability unavailable")
	ErrAdminBusy        = errors.New("administrative catalog is busy")
)
