//go:build !linux

package sqlite

import (
	"context"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
)

func CreateProtectedBackup(context.Context, string, ProtectedBackupOptions, func(context.Context) error, func(context.Context) (catalog.BackupRevisions, error)) (ProtectedBackupSummary, error) {
	return ProtectedBackupSummary{}, ErrStateUnsupported
}
