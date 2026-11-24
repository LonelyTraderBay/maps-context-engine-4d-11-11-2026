package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"golang.org/x/sys/windows"
)

const windowsIndexSidecarMaxBytes = 16 << 20

// Called only by confirmed recovery under the exclusive worktree and writer
// leases. The interrupted live SQLite files are evidence to retain, never the
// source of the replacement database.
func (o windowsStoreMaintenance) prepareIndexRollback(ctx context.Context) (_ *MaintenanceJournal, resultErr error) {
	if err := errors.Join(o.check(), o.lease.CheckRoot(ctx, o.files.control, o.files.scope, ports.ExclusiveLease)); err != nil {
		return nil, err
	}
	stores, release, err := o.openStores()
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	ids, err := o.incarnations(ctx)
	if err != nil {
		return nil, err
	}
	store := stores[0]
	headDir, err := store.head.directory(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, headDir.Close()) }()
	dataDir, err := store.data.directory(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, dataDir.Close()) }()
	identity, err := store.identity(ids[0])
	if err != nil {
		return nil, err
	}
	images := windowsIndexImages{store.head, store.data, headDir, dataDir, identity}
	keyBytes, err := store.head.requiredBytes(ctx, windowsCatalogHeadKey, 32)
	defer clear(keyBytes)
	if err != nil || len(keyBytes) != 32 {
		return nil, errors.Join(ErrStoreRecoveryRequired, err)
	}
	receiptBytes, err := store.data.requiredBytes(ctx, walAuthorityName, walAuthorityMaxBytes)
	if err != nil {
		return nil, err
	}
	headBytes, err := store.head.requiredBytes(ctx, walHeadName, walHeadMaxBytes)
	if err != nil {
		return nil, err
	}
	authority, err := replayWindowsIndexRollbackAuthority(ctx, identity, [32]byte(keyBytes), receiptBytes, headBytes)
	defer clear(authority.key[:])
	if err != nil || authority.pending == nil {
		return nil, err
	}
	preimage, err := images.preimage(ctx, *authority.pending)
	if err != nil {
		return nil, err
	}
	if err := withWindowsSQLiteImage(ctx, preimage, func(db *sql.DB) error {
		return store.verifyImage(ctx, db, o.lease, ids[0])
	}); err != nil {
		return nil, err
	}
	_, records, key, err := stores[1].capture(ctx, ids[1])
	clear(key[:])
	if err != nil {
		return nil, err
	}
	operation, err := windowsMaintenanceID()
	if err != nil {
		return nil, err
	}
	incarnation, err := windowsMaintenanceID()
	if err != nil {
		return nil, err
	}
	rollback := &MaintenanceIndexRollback{
		Records:  MaintenanceStore{"records", ids[1], hex.EncodeToString(records.SHA256[:])},
		Sidecars: []MaintenanceSidecar{},
	}
	for _, suffix := range indexRollbackSidecarSuffixes {
		digest, found, err := store.data.indexSidecar(ctx, dataDir, store.main+suffix, "", "")
		if err != nil {
			return nil, err
		}
		if found {
			rollback.Sidecars = append(rollback.Sidecars, MaintenanceSidecar{Suffix: suffix, SHA256: digest})
		}
	}
	stage, err := o.files.newStagedWorktreeDirectory(ctx, "restore-"+operation)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, stage.control.Close()) }()
	if err := stage.publishStagedWorktreeFile(ctx, "old-index-committed", preimage); err != nil {
		return nil, err
	}
	file := StoreBackupFile{Kind: "index", FileName: store.main, SourceIncarnation: ids[0]}
	next, proof, err := o.stageRestoredImage(ctx, stage, store, preimage, file, incarnation)
	if err != nil {
		return nil, err
	}
	newIdentity, err := store.identity(incarnation)
	if err != nil {
		return nil, err
	}
	log, head := windowsWALGenesis(newIdentity, authority.key, proof)
	journal := &MaintenanceJournal{FormatVersion: 1, OperationID: operation, ProfileInstallationID: o.profile.ProfileID,
		Target: "worktree", WorktreeID: o.files.scope, BackupDirectory: "index-preimage", BackupManifestSHA256: windowsBytesDigest(preimage),
		Phase: "PREPARED", PermissionReviewConfirmed: true, IndexRollback: rollback,
		OldStores: []MaintenanceStore{{"index", ids[0], windowsBytesDigest(preimage)}},
		NewStores: []MaintenanceStore{{"index", incarnation, hex.EncodeToString(proof.SHA256[:])}},
		Files:     []MaintenanceFileProof{}, CompletedSteps: []string{}}
	marker, err := o.restoredMarker(ctx, journal.NewStores)
	if err != nil {
		return nil, err
	}
	successors := [][]byte{next, log[:], head, marker}
	files := o.restoreFiles(stores[:1])
	for i, step := range maintenanceJournalSteps(*journal) {
		source := files[i]
		raw, err := source.files.requiredBytes(ctx, source.name, source.limit)
		if err != nil {
			return nil, err
		}
		if step == "index_journal" && windowsBytesDigest(raw) != windowsBytesDigest(receiptBytes) ||
			step == "index_head" && windowsBytesDigest(raw) != windowsBytesDigest(headBytes) {
			return nil, ErrStateChanged
		}
		if err := stage.publishStagedWorktreeFile(ctx, "old-"+step, raw); err != nil {
			return nil, err
		}
		if err := stage.publishStagedWorktreeFile(ctx, "new-"+step, successors[i]); err != nil {
			return nil, err
		}
		journal.Files = append(journal.Files, MaintenanceFileProof{step, windowsBytesDigest(raw), windowsBytesDigest(successors[i])})
	}
	return journal, o.check()
}

// Only an authenticated matching prefix can establish the last independently
// observed PREPARE. A short receipt tail may follow it; nothing in that tail
// is treated as a commit. The recovery plan keeps all original bytes.
func replayWindowsIndexRollbackAuthority(ctx context.Context, identity walAuthorityIdentity, key [32]byte,
	receipt, head []byte) (windowsWALAuthority, error) {
	if len(head) == 0 || len(head)%walHeadRecordSize != 0 {
		return windowsWALAuthority{}, ErrStoreRecoveryRequired
	}
	extent := len(head) / walHeadRecordSize * walAuthorityRecordSize
	if len(receipt) < extent || len(receipt)-extent >= walAuthorityRecordSize {
		return windowsWALAuthority{}, ErrStoreRecoveryRequired
	}
	authority, err := replayWindowsWALAuthority(ctx, identity, key, receipt[:extent], head)
	if err != nil {
		return windowsWALAuthority{}, err
	}
	if len(receipt) > extent && authority.pending == nil {
		clear(authority.key[:])
		return windowsWALAuthority{}, ErrStoreRecoveryRequired
	}
	return authority, nil
}

func (o windowsStoreMaintenance) verifyPreservedRecords(ctx context.Context, store windowsMaintenanceStore, proof MaintenanceStore) error {
	marker, err := o.files.readWindowsWorktreeStage(ctx)
	if err != nil || marker.RecordsIncarnation != proof.Incarnation {
		return errors.Join(ErrStateChanged, err)
	}
	_, image, key, err := store.capture(ctx, proof.Incarnation)
	clear(key[:])
	if err != nil || hex.EncodeToString(image.SHA256[:]) != proof.LogicalSHA256 {
		return errors.Join(ErrStateChanged, err)
	}
	return nil
}

func (o windowsStoreMaintenance) archiveIndexSidecars(ctx context.Context, store windowsMaintenanceStore, journal *MaintenanceJournal,
	boundary func(string)) (resultErr error) {
	dir, err := store.data.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	for _, suffix := range indexRollbackSidecarSuffixes {
		if err := o.check(); err != nil {
			return err
		}
		want := ""
		for _, sidecar := range journal.IndexRollback.Sidecars {
			if sidecar.Suffix == suffix {
				want = sidecar.SHA256
			}
		}
		name := store.main + suffix
		archive := "retired-" + journal.OperationID + suffix
		oldDigest, old, err := store.data.indexSidecar(ctx, dir, name, "", "")
		if err != nil {
			return err
		}
		newDigest, saved, err := store.data.indexSidecar(ctx, dir, archive, "", "")
		if err != nil {
			return err
		}
		switch {
		case want == "" && !old && !saved:
		case want != "" && !old && saved && newDigest == want:
		case want != "" && old && oldDigest == want && !saved:
			if _, present, err := store.data.indexSidecar(ctx, dir, name, archive, want); err != nil || !present {
				return errors.Join(ErrStoreRecoveryRequired, err)
			}
			if boundary != nil {
				boundary("retired" + suffix)
			}
			archived, present, err := store.data.indexSidecar(ctx, dir, archive, "", "")
			if err != nil || !present || archived != want {
				return errors.Join(ErrStoreRecoveryRequired, err)
			}
		default:
			return ErrStateChanged
		}
	}
	return store.data.requireAbsent(store.main+"-wal", store.main+"-shm", store.main+"-journal")
}

// SQLite-created sidecars can have the controller token's default DACL.
// Their bytes are never replayed. A confined single-link file is opened with
// a sharing mode that excludes writers, bound to its owner and hashed before
// the same handle is allowed to rename it to its unique archive name.
func (j windowsMaintenanceFiles) indexSidecar(ctx context.Context, dir *os.File, name, archive, want string) (_ string, _ bool, resultErr error) {
	file, err := openWindowsControlEntryShared(dir, name, nil, archive != "", windows.FILE_OPEN, windows.FILE_SHARE_READ)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(err, windows.STATUS_NO_SUCH_FILE) {
		return "", false, errors.Join(ctx.Err(), j.control.Verify(), j.acl.check(dir))
	}
	if err != nil {
		return "", false, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return "", false, err
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(j.acl.owner) {
		return "", false, errors.Join(ErrProfileDenied, err)
	}
	if err := paths.VerifyRegularFile(file); err != nil {
		return "", false, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() < 0 || info.Size() > windowsIndexSidecarMaxBytes {
		return "", false, errors.Join(ErrStateLimit, err)
	}
	hash, err := hashLogicalReader(ctx, file, info.Size())
	if err := errors.Join(err, ctx.Err(), j.control.Verify(), j.acl.check(dir), paths.VerifyRegularFile(file)); err != nil {
		return "", false, err
	}
	digest := hex.EncodeToString(hash[:])
	if archive != "" {
		if digest != want {
			return "", false, ErrStateChanged
		}
		if err := renameWindowsControlEntry(file, archive, false); err != nil {
			return "", false, err
		}
		if err := errors.Join(file.Sync(), j.control.Verify(), j.acl.check(dir)); err != nil {
			return "", false, err
		}
	}
	return digest, true, nil
}
