package sqlite

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// DecodeStoreBackupManifest accepts only the closed supported local format.
// This is the pre-SQL structural check; the administrator must independently
// approve its namespace/ownership and hash every file before a worker opens it.
func DecodeStoreBackupManifest(raw []byte) (StoreBackupManifest, error) {
	var out StoreBackupManifest
	if model.ValidateWireJSON(raw, backupManifestMaxBytes) != nil {
		return out, ErrProfileInput
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || out.FormatVersion != 1 || !model.ValidUUID(out.OperationID) || !model.ValidUUID(out.ProfileInstallationID) {
		return StoreBackupManifest{}, ErrProfileInput
	}
	created, err := time.Parse(time.RFC3339Nano, out.CreatedAt)
	if err != nil || created.UTC().Format(time.RFC3339Nano) != out.CreatedAt {
		return StoreBackupManifest{}, ErrProfileInput
	}
	expected := []string{"catalog"}
	scope := ""
	switch out.Target {
	case "catalog":
		if out.WorktreeID != "" || out.Consistency != "single_store" {
			return StoreBackupManifest{}, ErrProfileInput
		}
	case "worktree":
		if !model.ValidUUID(out.WorktreeID) || out.Consistency != "quiesced_worktree_set" {
			return StoreBackupManifest{}, ErrProfileInput
		}
		expected = []string{"index", "records"}
		scope = out.WorktreeID
	default:
		return StoreBackupManifest{}, ErrProfileInput
	}
	if !out.Revisions.ValidFor(scope) || len(out.Stores) != len(expected) {
		return StoreBackupManifest{}, ErrProfileInput
	}
	for i, store := range out.Stores {
		version := 1
		if store.Kind == "index" {
			version = 2
		}
		if store.Kind != expected[i] || store.FileName != store.Kind+".sqlite" || store.WorktreeID != scope || !model.ValidUUID(store.SourceIncarnation) || store.SchemaVersion != version || store.MinReaderVersion != version || !model.ValidDigest(store.RawSHA256) || !model.ValidDigest(store.LogicalSHA256) || store.ByteLength < 512 || store.ByteLength > 2<<30 || store.ByteLength%512 != 0 || !backupSQLiteVersion(store.SQLiteVersion) {
			return StoreBackupManifest{}, ErrProfileInput
		}
		if store.Kind == "records" {
			if store.RecordEpoch == nil || *store.RecordEpoch < 0 || *store.RecordEpoch > 9007199254740991 {
				return StoreBackupManifest{}, ErrProfileInput
			}
		} else if store.RecordEpoch != nil {
			return StoreBackupManifest{}, ErrProfileInput
		}
		if store.Kind == "catalog" && store.SourceIncarnation != out.Revisions.CatalogIncarnation {
			return StoreBackupManifest{}, ErrProfileInput
		}
	}
	return out, nil
}

func backupSQLiteVersion(value string) bool {
	if len(value) > 32 {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if len(part) < 1 || len(part) > 6 || len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
