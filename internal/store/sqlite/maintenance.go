package sqlite

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrStoreRecoveryRequired = errors.New("store maintenance recovery required")

const maintenanceJournalMaxBytes = 32768

// Root-owned, scoped offline recovery metadata; never source bytes, keys or a
// caller-supplied SQL/path script. Executors derive paths from the closed step
// names, scope and operation UUID. Presence is not permission to apply recovery.
type MaintenanceJournal struct {
	FormatVersion             int                       `json:"format_version"`
	OperationID               string                    `json:"operation_id"`
	ProfileInstallationID     string                    `json:"profile_installation_id"`
	Target                    string                    `json:"target"`
	WorktreeID                string                    `json:"worktree_id"`
	BackupDirectory           string                    `json:"backup_directory"`
	BackupManifestSHA256      string                    `json:"backup_manifest_sha256"`
	Phase                     string                    `json:"phase"`
	Outcome                   string                    `json:"outcome"`
	PermissionReviewConfirmed bool                      `json:"permission_review_confirmed"`
	OldStores                 []MaintenanceStore        `json:"old_stores"`
	NewStores                 []MaintenanceStore        `json:"new_stores"`
	Files                     []MaintenanceFileProof    `json:"files"`
	CompletedSteps            []string                  `json:"completed_steps"`
	IndexRollback             *MaintenanceIndexRollback `json:"index_rollback,omitempty"`
}

type MaintenanceIndexRollback struct {
	Records  MaintenanceStore     `json:"records"`
	Sidecars []MaintenanceSidecar `json:"sidecars"`
}

type MaintenanceSidecar struct {
	Suffix string `json:"suffix"`
	SHA256 string `json:"sha256"`
}

var indexRollbackSidecarSuffixes = [...]string{"-wal", "-shm", "-journal"}

type MaintenanceStore struct {
	Kind          string `json:"kind"`
	Incarnation   string `json:"incarnation"`
	LogicalSHA256 string `json:"logical_sha256"`
}
type MaintenanceFileProof struct {
	Step      string `json:"step"`
	OldSHA256 string `json:"old_sha256"`
	NewSHA256 string `json:"new_sha256"`
}

type StoreRecoveryReport struct {
	Target              string   `json:"target"`
	WorktreeID          string   `json:"worktree_id"`
	ReportOnly          bool     `json:"report_only"`
	JournalPresent      bool     `json:"journal_present"`
	OperationID         string   `json:"operation_id"`
	Phase               string   `json:"phase"`
	Outcome             string   `json:"outcome"`
	CompletedSteps      []string `json:"completed_steps"`
	ActualStateVerified bool     `json:"actual_state_verified"`
}

func maintenanceSteps(target string) []string {
	if target == "catalog" {
		return []string{"catalog_main", "catalog_journal", "catalog_head", "profile_manifest"}
	}
	if target == "worktree" {
		return []string{"index_main", "records_main", "index_journal", "records_journal", "index_head", "records_head", "worktree_manifest"}
	}
	return nil
}

func maintenanceJournalSteps(j MaintenanceJournal) []string {
	if j.IndexRollback != nil {
		return []string{"index_main", "index_journal", "index_head", "worktree_manifest"}
	}
	return maintenanceSteps(j.Target)
}

func decodeMaintenanceJournal(raw []byte, profile, scope string) (out MaintenanceJournal, resultErr error) {
	if model.ValidateWireJSON(raw, maintenanceJournalMaxBytes) != nil {
		return out, ErrStoreRecoveryRequired
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || out.FormatVersion != 1 || !model.ValidUUID(out.OperationID) || !model.ValidUUID(out.ProfileInstallationID) || out.ProfileInstallationID != profile || !model.ValidDigest(out.BackupManifestSHA256) || out.BackupDirectory == "" || len(out.BackupDirectory) > 128 || strings.Contains(out.BackupDirectory, "/") || model.ValidateRelativePath(out.BackupDirectory, false) != nil {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	kinds := []string{"catalog"}
	if scope == "catalog" {
		if out.Target != "catalog" || out.WorktreeID != "" {
			return MaintenanceJournal{}, ErrStoreRecoveryRequired
		}
	} else {
		if !model.ValidUUID(scope) || out.Target != "worktree" || out.WorktreeID != scope {
			return MaintenanceJournal{}, ErrStoreRecoveryRequired
		}
		kinds = []string{"index", "records"}
	}
	if rollback := out.IndexRollback; rollback != nil {
		if out.Target != "worktree" || out.BackupDirectory != "index-preimage" || out.Phase == "NEEDS_RECOVERY" ||
			rollback.Records.Kind != "records" || !model.ValidUUID(rollback.Records.Incarnation) ||
			!model.ValidDigest(rollback.Records.LogicalSHA256) || rollback.Sidecars == nil || len(rollback.Sidecars) > len(indexRollbackSidecarSuffixes) {
			return MaintenanceJournal{}, ErrStoreRecoveryRequired
		}
		seen := map[string]bool{}
		for _, sidecar := range rollback.Sidecars {
			valid := false
			for _, suffix := range indexRollbackSidecarSuffixes {
				valid = valid || suffix == sidecar.Suffix
			}
			if !valid || seen[sidecar.Suffix] || !model.ValidDigest(sidecar.SHA256) {
				return MaintenanceJournal{}, ErrStoreRecoveryRequired
			}
			seen[sidecar.Suffix] = true
		}
		kinds = []string{"index"}
	}
	if len(out.OldStores) != len(kinds) || len(out.NewStores) != len(kinds) || out.Files == nil || out.CompletedSteps == nil {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	for i, kind := range kinds {
		old, next := out.OldStores[i], out.NewStores[i]
		if old.Kind != kind || next.Kind != kind || !model.ValidUUID(old.Incarnation) || !model.ValidUUID(next.Incarnation) || old.Incarnation == next.Incarnation || !model.ValidDigest(old.LogicalSHA256) || next.LogicalSHA256 != "" && !model.ValidDigest(next.LogicalSHA256) {
			return MaintenanceJournal{}, ErrStoreRecoveryRequired
		}
		for _, previous := range out.OldStores {
			if next.Incarnation == previous.Incarnation {
				return MaintenanceJournal{}, ErrStoreRecoveryRequired
			}
		}
	}
	if len(kinds) == 2 && (out.OldStores[0].Incarnation == out.OldStores[1].Incarnation || out.NewStores[0].Incarnation == out.NewStores[1].Incarnation) {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	if rollback := out.IndexRollback; rollback != nil && (rollback.Records.Incarnation == out.OldStores[0].Incarnation ||
		rollback.Records.Incarnation == out.NewStores[0].Incarnation || out.BackupManifestSHA256 != out.OldStores[0].LogicalSHA256) {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	switch out.Phase {
	case "NEEDS_RECOVERY", "PREPARED", "REPLACING", "VERIFYING", "VERIFIED":
	default:
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	steps := maintenanceJournalSteps(out)
	if len(out.CompletedSteps) > len(steps) {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	if (out.Phase == "NEEDS_RECOVERY" || out.Phase == "PREPARED") && len(out.CompletedSteps) != 0 || out.Phase == "VERIFYING" && len(out.CompletedSteps) != len(steps) {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	for i, step := range out.CompletedSteps {
		if step != steps[i] {
			return MaintenanceJournal{}, ErrStoreRecoveryRequired
		}
	}
	if len(out.Files) != 0 && len(out.Files) != len(steps) {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	for i, proof := range out.Files {
		if proof.Step != steps[i] || !model.ValidDigest(proof.OldSHA256) || !model.ValidDigest(proof.NewSHA256) || proof.OldSHA256 == proof.NewSHA256 {
			return MaintenanceJournal{}, ErrStoreRecoveryRequired
		}
	}
	if out.Phase != "NEEDS_RECOVERY" && out.Outcome != "aborted" && len(out.Files) != len(steps) {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	if out.Phase == "VERIFIED" {
		if out.Outcome != "restored" && out.Outcome != "aborted" || out.Outcome == "restored" && (len(out.CompletedSteps) != len(steps) || !out.PermissionReviewConfirmed) || out.Outcome == "aborted" && len(out.CompletedSteps) != 0 {
			return MaintenanceJournal{}, ErrStoreRecoveryRequired
		}
	} else if out.Outcome != "" || !out.PermissionReviewConfirmed {
		return MaintenanceJournal{}, ErrStoreRecoveryRequired
	}
	if out.Phase != "NEEDS_RECOVERY" && out.Outcome != "aborted" {
		for _, store := range out.NewStores {
			if !model.ValidDigest(store.LogicalSHA256) {
				return MaintenanceJournal{}, ErrStoreRecoveryRequired
			}
		}
	}
	return out, nil
}

// Only the journal executor can publish these transitions; this validator is
// not proof that the corresponding filesystem operation has happened.
func validMaintenanceAdvance(old, next MaintenanceJournal) bool {
	if old.FormatVersion != next.FormatVersion || old.OperationID != next.OperationID || old.ProfileInstallationID != next.ProfileInstallationID || old.Target != next.Target || old.WorktreeID != next.WorktreeID || old.BackupDirectory != next.BackupDirectory || old.BackupManifestSHA256 != next.BackupManifestSHA256 || old.PermissionReviewConfirmed != next.PermissionReviewConfirmed || !reflect.DeepEqual(old.OldStores, next.OldStores) || !reflect.DeepEqual(old.IndexRollback, next.IndexRollback) || len(old.NewStores) != len(next.NewStores) {
		return false
	}
	for i, store := range old.NewStores {
		if store.Kind != next.NewStores[i].Kind || store.Incarnation != next.NewStores[i].Incarnation || old.Phase != "NEEDS_RECOVERY" && store.LogicalSHA256 != next.NewStores[i].LogicalSHA256 {
			return false
		}
	}
	if old.Phase != "NEEDS_RECOVERY" && !reflect.DeepEqual(old.Files, next.Files) {
		return false
	}
	switch old.Phase {
	case "NEEDS_RECOVERY":
		return next.Phase == "PREPARED" || next.Phase == "VERIFIED" && next.Outcome == "aborted"
	case "PREPARED":
		return next.Phase == "REPLACING" && len(next.CompletedSteps) <= 1
	case "REPLACING":
		return next.Phase == "REPLACING" && len(next.CompletedSteps) == len(old.CompletedSteps)+1 || next.Phase == "VERIFYING" && len(next.CompletedSteps) == len(old.CompletedSteps)
	case "VERIFYING":
		return next.Phase == "VERIFIED" && next.Outcome == "restored" && reflect.DeepEqual(old.CompletedSteps, next.CompletedSteps)
	}
	return false
}
