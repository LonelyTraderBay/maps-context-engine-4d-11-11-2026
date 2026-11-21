//go:build !linux && !windows

package sqlite

import "context"

func ReadProtectedRecoveryReport(context.Context, string, string, string) (StoreRecoveryReport, error) {
	return StoreRecoveryReport{}, ErrStateUnsupported
}
