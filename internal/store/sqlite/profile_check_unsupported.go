//go:build !linux && !windows

package sqlite

import "context"

func CheckProtectedProfile(context.Context, string) (ProtectedProfileSummary, error) {
	return ProtectedProfileSummary{}, ErrStateUnsupported
}
