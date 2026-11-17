package contextpack

import "errors"

var (
	ErrCursorKeyUnsafe      = errors.New("cursor key location or ownership is unsafe")
	ErrCursorKeyPending     = errors.New("cursor key creation requires recovery")
	ErrCursorKeyUncertain   = errors.New("cursor key installation outcome is uncertain")
	ErrCursorKeyUnsupported = errors.New("secure cursor key storage unsupported")
)
