// Package paths provides handle-based confined source reads. Authorization and
// registered root identity are checked by the calling service.
package paths

import "errors"

var (
	ErrUnsupported = errors.New("secure path access is unsupported")
	ErrRootChanged = errors.New("registered root identity changed")
	ErrInvalidPath = errors.New("invalid path")
	ErrNotRegular  = errors.New("not a regular file")
	ErrHardLinked  = errors.New("file has multiple hard links")
)
