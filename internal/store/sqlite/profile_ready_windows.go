package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

const windowsReadyName = "ready.json"

type windowsReadyRecord struct {
	FormatVersion int    `json:"format_version"`
	Installation  string `json:"installation_digest"`
	Worktree      string `json:"worktree_id"`
	Profile       string `json:"profile_id"`
}

func windowsInstallationDigest(m windowsProfilePreparation) string {
	m.Stage = "CREATING"
	raw, _ := json.Marshal(m) // fixed primitive fields
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func readWindowsReadyRecord(ctx context.Context, root *paths.Root, dir *os.File, acl windowsControlACL,
	m windowsProfilePreparation) (record windowsReadyRecord, present bool, err error) {
	files := windowsMaintenanceFiles{control: root, acl: acl}
	raw, present, err := files.readEntry(ctx, dir, windowsReadyName)
	if err != nil || !present {
		return record, present, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if model.ValidateWireJSON(raw, maintenanceJournalMaxBytes) != nil || decoder.Decode(&record) != nil ||
		record.FormatVersion != 1 || record.Installation != windowsInstallationDigest(m) ||
		!model.ValidUUID(record.Worktree) || !model.ValidDigest(record.Profile) {
		return windowsReadyRecord{}, true, ErrProfileIncomplete
	}
	return record, true, nil
}

// ActivateWindowsProtectedProfile seals readiness only after the authenticated
// catalog/index/records owners have opened and checked all three live heads.
// The shared lifecycle leases and scoped writer keep admission stable.
func ActivateWindowsProtectedProfile(ctx context.Context, installation, worktree, profile string) (out ProtectedProfileSummary, resultErr error) {
	p, err := OpenWindowsScopedPools(ctx, installation, worktree, profile)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, p.Close()) }()
	job, err := windowsMaintenanceID()
	if err != nil {
		return out, err
	}
	writer, err := runtime.AcquireWriter(ctx, p.worktreeLease, job, 5*time.Second)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	key, err := p.CursorKey(ctx)
	clear(key[:])
	if err != nil {
		return out, err
	}
	b := p.binding
	g, err := p.Index.ReadGeneration(ctx, profile, "", func(ctx context.Context, w, id, digest string, epoch int64) error {
		if w != worktree || id != profile || digest != b.PolicyDigest || epoch != b.PolicyEpoch {
			return ErrProfileDenied
		}
		return p.Check(ctx)
	}, 1<<20)
	if err != nil || g.ProviderDigest != b.ProviderDigest() {
		return out, errors.Join(ErrProfileIncomplete, err)
	}
	dir, err := p.config.directory(ctx)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	if err := p.Check(ctx); err != nil {
		return out, err
	}
	if p.Summary.Stage == "CREATING" {
		record := windowsReadyRecord{FormatVersion: 1, Installation: windowsInstallationDigest(p.preparation), Worktree: worktree, Profile: profile}
		raw, err := json.Marshal(record)
		if err != nil {
			return out, err
		}
		sd, err := p.config.acl.descriptor()
		if err != nil {
			return out, err
		}
		if err := p.config.publish(ctx, dir, sd, windowsReadyName, raw, false, nil); err != nil {
			return out, err
		}
	}
	return ProtectedProfileSummary{ProfileID: p.Summary.InstallationID, Stage: "READY",
		CatalogVerified: true, TrustedHead: true, CatalogIncarnation: p.Summary.CatalogIncarnation}, nil
}

func checkWindowsReadyProfile(ctx context.Context, name string, m windowsProfilePreparation, files windowsMaintenanceFiles) (out ProtectedProfileSummary, resultErr error) {
	root, err := paths.OpenRoot(name)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	dir, err := root.DirectoryHandle()
	if err != nil {
		return out, err
	}
	record, present, err := readWindowsReadyRecord(ctx, root, dir, files.acl, m)
	if err := errors.Join(err, dir.Close()); err != nil || !present {
		return out, errors.Join(ErrProfileIncomplete, err)
	}
	pools, err := CheckWindowsScopedPools(ctx, name, record.Worktree, record.Profile)
	if err != nil || pools.Stage != "READY" || !pools.WALHeadsVerified || pools.InstallationID != m.ProfileID {
		return out, errors.Join(ErrProfileIncomplete, err)
	}
	return ProtectedProfileSummary{ProfileID: m.ProfileID, Stage: "READY", CatalogVerified: true, TrustedHead: true,
		CatalogIncarnation: pools.CatalogIncarnation}, nil
}
