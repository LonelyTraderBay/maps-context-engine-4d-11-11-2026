package sqlite

import (
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
)

const backupManifestMaxBytes = 8 << 20

// Offline local-file format, distinct from the six-tool wire contract. Backup
// excludes profile/cursor keys, sessions, bearer tokens and runtime.json.
type StoreBackupManifest struct {
	FormatVersion         int                     `json:"format_version"`
	OperationID           string                  `json:"operation_id"`
	ProfileInstallationID string                  `json:"profile_installation_id"`
	Target                string                  `json:"target"`
	WorktreeID            string                  `json:"worktree_id"`
	Consistency           string                  `json:"consistency"`
	CreatedAt             string                  `json:"created_at"`
	Revisions             catalog.BackupRevisions `json:"revisions"`
	Stores                []StoreBackupFile       `json:"stores"`
}
type StoreBackupFile struct {
	Kind              string `json:"kind"`
	WorktreeID        string `json:"worktree_id"`
	SourceIncarnation string `json:"source_incarnation"`
	SchemaVersion     int    `json:"schema_version"`
	MinReaderVersion  int    `json:"min_reader_version"`
	RecordEpoch       *int64 `json:"record_epoch"`
	FileName          string `json:"file_name"`
	RawSHA256         string `json:"raw_sha256"`
	LogicalSHA256     string `json:"source_logical_sha256"`
	ByteLength        int64  `json:"byte_length"`
	SQLiteVersion     string `json:"sqlite_version"`
}
type ProtectedBackupOptions struct {
	Target, WorktreeID, Output string
	Wait                       time.Duration
	MaxDatabaseBytes           int64
}
type ProtectedBackupSummary struct {
	OperationID    string `json:"operation_id"`
	Target         string `json:"target"`
	WorktreeID     string `json:"worktree_id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	FileCount      int    `json:"file_count"`
	TotalBytes     int64  `json:"total_bytes"`
}
