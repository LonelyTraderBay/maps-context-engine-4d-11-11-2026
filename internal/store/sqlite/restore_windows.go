package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type windowsRestoreInput struct {
	manifest StoreBackupManifest
	raw      []byte
	images   [][]byte
}

// RestoreWindowsProtectedStore requires explicit controller confirmation and
// takes exclusive lifecycle and writer leases for the chosen restore scope.
func RestoreWindowsProtectedStore(ctx context.Context, root, manifest string, confirmed bool, wait time.Duration) (ProtectedRestoreSummary, error) {
	return restoreWindowsProtectedStore(ctx, root, manifest, confirmed, wait, nil)
}

func restoreWindowsProtectedStore(ctx context.Context, root, manifest string, confirmed bool, wait time.Duration,
	boundary func(string)) (ProtectedRestoreSummary, error) {
	target, worktree, leaf, err := windowsRestoreSelection(root, manifest)
	if !confirmed || err != nil {
		return ProtectedRestoreSummary{}, ErrProfileInput
	}
	return withWindowsStoreMaintenance(ctx, root, target, worktree, "restore", wait, func(o windowsStoreMaintenance) (ProtectedRestoreSummary, error) {
		return o.restore(ctx, target, worktree, leaf, boundary)
	})
}

// RecoverWindowsProtectedStore resumes an authenticated plan, or restores a
// worktree's interrupted index to the checkpoint authorized by its PREPARE.
func RecoverWindowsProtectedStore(ctx context.Context, root, target, worktree string, confirmed bool, wait time.Duration) (ProtectedRestoreSummary, error) {
	return recoverWindowsProtectedStore(ctx, root, target, worktree, confirmed, wait, nil)
}

func recoverWindowsProtectedStore(ctx context.Context, root, target, worktree string, confirmed bool, wait time.Duration,
	boundary func(string)) (ProtectedRestoreSummary, error) {
	if !confirmed {
		return ProtectedRestoreSummary{}, ErrProfileInput
	}
	return withWindowsStoreMaintenance(ctx, root, target, worktree, "recover", wait, func(o windowsStoreMaintenance) (out ProtectedRestoreSummary, resultErr error) {
		journal, present, err := o.files.read(ctx)
		if err != nil || journal.Phase == "NEEDS_RECOVERY" {
			return out, errors.Join(ErrStoreRecoveryRequired, err)
		}
		if target == "worktree" && (!present || journal.Phase == "VERIFIED") {
			rollback, err := o.prepareIndexRollback(ctx)
			if err != nil {
				return out, err
			}
			if rollback != nil {
				if err := o.files.write(ctx, *rollback, false, nil); err != nil {
					return out, errors.Join(ErrStoreRecoveryRequired, err)
				}
				if boundary != nil {
					boundary("PREPARED")
				}
				return o.runRestoreJournal(ctx, rollback, boundary)
			}
		}
		if !present {
			return out, ErrStoreRecoveryRequired
		}
		if journal.Phase == "VERIFIED" {
			return windowsRestoreSummary(journal), o.check()
		}
		return o.runRestoreJournal(ctx, &journal, boundary)
	})
}

func windowsRestoreSelection(root, input string) (target, worktree, leaf string, err error) {
	if !validWindowsPreparationRoot(root) || !filepath.IsAbs(input) || filepath.Clean(input) != input || filepath.Base(input) != "backup.json" {
		return "", "", "", ErrProfileInput
	}
	dir := filepath.Dir(input)
	leaf = filepath.Base(dir)
	if !windowsOperationLeaf(leaf) {
		return "", "", "", ErrProfileInput
	}
	base := filepath.Dir(dir)
	switch {
	case base == filepath.Join(windowsMaintenancePath(root, "catalog", ""), "backups"):
		return "catalog", "", leaf, nil
	case filepath.Base(base) == "backups":
		worktree = filepath.Base(filepath.Dir(base))
		if model.ValidUUID(worktree) && base == filepath.Join(windowsMaintenancePath(root, "worktree", worktree), "backups") {
			return "worktree", worktree, leaf, nil
		}
	}
	return "", "", "", ErrProfileInput
}

func (o windowsStoreMaintenance) readRestoreInput(ctx context.Context, target, worktree, leaf string) (out windowsRestoreInput, resultErr error) {
	backup, err := o.files.openWindowsPoolDirectory("backups/" + leaf)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, backup.control.Close()) }()
	out.raw, err = backup.requiredBytes(ctx, "backup.json", backupManifestMaxBytes)
	if err != nil {
		return out, err
	}
	out.manifest, err = DecodeStoreBackupManifest(out.raw)
	if err != nil || out.manifest.ProfileInstallationID != o.profile.ProfileID ||
		out.manifest.Target != target || out.manifest.WorktreeID != worktree {
		return windowsRestoreInput{}, errors.Join(ErrProfileDenied, err)
	}
	dir, err := backup.directory(ctx)
	if err != nil {
		return windowsRestoreInput{}, err
	}
	entries, err := dir.ReadDir(len(out.manifest.Stores) + 2)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err := errors.Join(err, dir.Close()); err != nil || len(entries) != len(out.manifest.Stores)+1 {
		return windowsRestoreInput{}, errors.Join(ErrProfileInput, err)
	}
	allowed := map[string]bool{"backup.json": true}
	for _, store := range out.manifest.Stores {
		allowed[store.FileName] = true
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !allowed[entry.Name()] {
			return windowsRestoreInput{}, ErrProfileInput
		}
	}

	// Capture all declared bytes before any artifact is passed to SQLite.
	for _, file := range out.manifest.Stores {
		if file.ByteLength > windowsCatalogSeedMaxSize {
			return windowsRestoreInput{}, ErrStateLimit
		}
		if err := backup.requireAbsent(file.FileName+"-wal", file.FileName+"-shm", file.FileName+"-journal"); err != nil {
			return windowsRestoreInput{}, err
		}
		raw, err := backup.requiredBytes(ctx, file.FileName, file.ByteLength)
		if err != nil || int64(len(raw)) != file.ByteLength || windowsBytesDigest(raw) != file.RawSHA256 || !windowsStandaloneImage(raw) {
			return windowsRestoreInput{}, errors.Join(ErrStateChanged, err)
		}
		out.images = append(out.images, raw)
	}
	latest, err := backup.requiredBytes(ctx, "backup.json", backupManifestMaxBytes)
	if err != nil || windowsBytesDigest(latest) != windowsBytesDigest(out.raw) {
		return windowsRestoreInput{}, errors.Join(ErrStateChanged, err)
	}
	return out, backup.control.Verify()
}

func (o windowsStoreMaintenance) restore(ctx context.Context, target, worktree, leaf string, boundary func(string)) (out ProtectedRestoreSummary, resultErr error) {
	if err := o.files.checkServe(ctx, ""); err != nil {
		return out, err
	}
	input, err := o.readRestoreInput(ctx, target, worktree, leaf)
	if err != nil {
		return out, err
	}
	operation, err := windowsMaintenanceID()
	if err != nil {
		return out, err
	}
	journal := MaintenanceJournal{FormatVersion: 1, OperationID: operation, ProfileInstallationID: o.profile.ProfileID,
		Target: target, WorktreeID: worktree, BackupDirectory: leaf, BackupManifestSHA256: windowsBytesDigest(input.raw),
		Phase: "PREPARED", PermissionReviewConfirmed: true, Files: []MaintenanceFileProof{}, CompletedSteps: []string{}}
	if err := o.prepareRestore(ctx, input, &journal); err != nil {
		return out, err
	}
	if err := o.check(); err != nil {
		return out, err
	}
	if err := o.files.write(ctx, journal, false, nil); err != nil {
		return out, errors.Join(ErrStoreRecoveryRequired, err)
	}
	if boundary != nil {
		boundary("PREPARED")
	}
	return o.runRestoreJournal(ctx, &journal, boundary)
}

type windowsRestoreFile struct {
	files windowsMaintenanceFiles
	name  string
	limit int64
}

func (o windowsStoreMaintenance) restoreFiles(stores []windowsMaintenanceStore) []windowsRestoreFile {
	var files []windowsRestoreFile
	for _, s := range stores {
		files = append(files, windowsRestoreFile{s.data, s.main, windowsCatalogSeedMaxSize})
	}
	for _, s := range stores {
		files = append(files, windowsRestoreFile{s.data, walAuthorityName, walAuthorityMaxBytes})
	}
	for _, s := range stores {
		files = append(files, windowsRestoreFile{s.head, walHeadName, walHeadMaxBytes})
	}
	marker := "worktree.json"
	if o.files.scope == "catalog" {
		marker = windowsCatalogSeedRecord
	}
	return append(files, windowsRestoreFile{o.files, marker, maintenanceJournalMaxBytes})
}

func (o windowsStoreMaintenance) prepareRestore(ctx context.Context, input windowsRestoreInput, journal *MaintenanceJournal) (resultErr error) {
	stores, release, err := o.openStores()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	ids, err := o.incarnations(ctx)
	if err != nil {
		return err
	}
	stage, err := o.files.newStagedWorktreeDirectory(ctx, "restore-"+journal.OperationID)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, stage.control.Close()) }()
	files := o.restoreFiles(stores)
	images, receipts, heads := make([][]byte, len(stores)), make([][]byte, len(stores)), make([][]byte, len(stores))
	for i, store := range stores {
		_, logical, key, err := store.capture(ctx, ids[i])
		if err != nil {
			clear(key[:])
			return err
		}
		var next []byte
		var nextProof walLogicalImage
		id, err := windowsMaintenanceID()
		if err == nil {
			next, nextProof, err = o.stageRestoreImage(ctx, stage, store, input, i, id)
		}
		if err != nil {
			clear(key[:])
			return err
		}
		identity, err := store.identity(id)
		if err != nil {
			clear(key[:])
			return err
		}
		receipt, head := windowsWALGenesis(identity, key, nextProof)
		clear(key[:])
		images[i], receipts[i], heads[i] = next, receipt[:], head
		journal.OldStores = append(journal.OldStores, MaintenanceStore{store.kind, ids[i], hex.EncodeToString(logical.SHA256[:])})
		journal.NewStores = append(journal.NewStores, MaintenanceStore{store.kind, id, hex.EncodeToString(nextProof.SHA256[:])})
	}
	marker, err := o.restoredMarker(ctx, journal.NewStores)
	if err != nil {
		return err
	}
	next := append(append(append(images, receipts...), heads...), marker)
	for i, step := range maintenanceSteps(journal.Target) {
		file := files[i]
		before, err := file.files.requiredBytes(ctx, file.name, file.limit)
		if err != nil {
			return err
		}
		proof := MaintenanceFileProof{Step: step, OldSHA256: windowsBytesDigest(before), NewSHA256: windowsBytesDigest(next[i])}
		if i < len(stores) && (proof.OldSHA256 != journal.OldStores[i].LogicalSHA256 || proof.NewSHA256 != journal.NewStores[i].LogicalSHA256) {
			return ErrStateChanged
		}
		if err := stage.publishStagedWorktreeFile(ctx, "old-"+step, before); err != nil {
			return err
		}
		if err := stage.publishStagedWorktreeFile(ctx, "new-"+step, next[i]); err != nil {
			return err
		}
		journal.Files = append(journal.Files, proof)
	}
	return o.check()
}

func (o windowsStoreMaintenance) restoredMarker(ctx context.Context, stores []MaintenanceStore) ([]byte, error) {
	if o.files.scope == "catalog" {
		raw, err := o.files.requiredBytes(ctx, windowsCatalogSeedRecord, maintenanceJournalMaxBytes)
		if err != nil {
			return nil, err
		}
		marker, err := decodeWindowsCatalogSeed(raw, o.profile.ProfileID)
		if err != nil {
			return nil, err
		}
		marker.ActiveIncarnation = stores[0].Incarnation
		return json.Marshal(marker)
	}
	marker, err := o.files.readWindowsWorktreeStage(ctx)
	if err != nil {
		return nil, err
	}
	marker.IndexIncarnation = stores[0].Incarnation
	if len(stores) == 2 {
		marker.RecordsIncarnation = stores[1].Incarnation
	}
	return json.Marshal(marker)
}

func (o windowsStoreMaintenance) stageRestoreImage(ctx context.Context, stage windowsMaintenanceFiles, store windowsMaintenanceStore,
	input windowsRestoreInput, position int, incarnation string) (_ []byte, _ walLogicalImage, resultErr error) {
	file := input.manifest.Stores[position]
	image := input.images[position]
	hold, err := o.lease.Hold(ctx)
	if err != nil {
		return nil, walLogicalImage{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, hold.Close()) }()
	if err := withWindowsSQLiteImage(ctx, image, func(db *sql.DB) error {
		return o.verifyBackupImage(ctx, store, db, file, input.manifest)
	}); err != nil {
		return nil, walLogicalImage{}, err
	}
	return o.stageRestoredImage(ctx, stage, store, image, file, incarnation)
}

func (o windowsStoreMaintenance) stageRestoredImage(ctx context.Context, stage windowsMaintenanceFiles, store windowsMaintenanceStore,
	image []byte, file StoreBackupFile, incarnation string) (_ []byte, _ walLogicalImage, resultErr error) {
	if _, err := windowsStageBackupCopy(ctx, o.lease, image, stage, store.main, windowsCatalogSeedMaxSize); err != nil {
		return nil, walLogicalImage{}, err
	}
	if err := o.rotateStage(ctx, stage, file, incarnation); err != nil {
		return nil, walLogicalImage{}, err
	}
	if err := stage.flushArtifact(ctx, store.main); err != nil {
		return nil, walLogicalImage{}, err
	}
	dir, err := stage.directory(ctx)
	if err != nil {
		return nil, walLogicalImage{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	updated, observed, err := stage.captureWindowsCheckpoint(ctx, dir, store.main)
	if err != nil {
		return nil, walLogicalImage{}, err
	}
	err = withWindowsSQLiteImage(ctx, updated, func(db *sql.DB) error {
		return store.verifyImage(ctx, db, o.lease, incarnation)
	})
	return updated, observed, err
}

func (o windowsStoreMaintenance) rotateStage(ctx context.Context, stage windowsMaintenanceFiles, file StoreBackupFile, incarnation string) (resultErr error) {
	root, err := stage.control.CanonicalPath()
	if err != nil {
		return err
	}
	db, close, err := openWindowsMaintenanceSQLite(ctx, o.lease, filepath.Join(root, file.FileName), o.files.scope)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, close()) }()
	table := ""
	switch file.Kind {
	case "catalog":
		table = "catalog_identity"
	case "index":
		table = "store_identity"
	case "records":
		table = "records_meta"
	default:
		return ErrProfileInput
	}
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		return errors.Join(ErrStateUnsafe, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	row, err := tx.ExecContext(ctx, "UPDATE "+table+" SET incarnation=? WHERE singleton=1 AND incarnation=?", incarnation, file.SourceIncarnation)
	if err != nil {
		return err
	}
	n, err := row.RowsAffected()
	if err != nil || n != 1 {
		return errors.Join(ErrStateChanged, err)
	}
	if err := o.check(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	var busy, total, done int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &total, &done); err != nil || busy != 0 || total != done {
		return errors.Join(ErrStateChanged, err)
	}
	return nil
}

func (o windowsStoreMaintenance) runRestoreJournal(ctx context.Context, journal *MaintenanceJournal, boundary func(string)) (out ProtectedRestoreSummary, resultErr error) {
	defer func() {
		if resultErr != nil {
			out = ProtectedRestoreSummary{}
			resultErr = errors.Join(ErrStoreRecoveryRequired, resultErr)
		}
	}()
	stage, err := o.files.openWindowsPoolDirectory("restore-" + journal.OperationID)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, stage.control.Close()) }()
	stores, release, err := o.openStores()
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	var preserved *windowsMaintenanceStore
	if journal.IndexRollback != nil {
		preserved = &stores[1]
		if err := o.verifyPreservedRecords(ctx, *preserved, journal.IndexRollback.Records); err != nil {
			return out, err
		}
		stores = stores[:1]
	}
	files := o.restoreFiles(stores)
	for _, s := range stores {
		if journal.IndexRollback != nil {
			continue
		}
		if err := s.data.requireAbsent(s.main+"-wal", s.main+"-shm", s.main+"-journal"); err != nil {
			return out, err
		}
	}
	previous, next := make([][]byte, len(files)), make([][]byte, len(files))
	completed := len(journal.CompletedSteps)
	for i, proof := range journal.Files {
		previous[i], err = stage.requiredBytes(ctx, "old-"+proof.Step, files[i].limit)
		if err != nil || windowsBytesDigest(previous[i]) != proof.OldSHA256 {
			return out, errors.Join(ErrStateChanged, err)
		}
		next[i], err = stage.requiredBytes(ctx, "new-"+proof.Step, files[i].limit)
		if err != nil || windowsBytesDigest(next[i]) != proof.NewSHA256 {
			return out, errors.Join(ErrStateChanged, err)
		}
		got, err := files[i].fileDigest(ctx)
		if err != nil {
			return out, err
		}
		switch {
		case got == proof.NewSHA256 && (i < completed || i == completed && journal.Phase == "REPLACING"):
		case got == proof.OldSHA256 && i >= completed:
		default:
			return out, ErrStateChanged
		}
	}
	if journal.IndexRollback != nil {
		preimage, err := stage.requiredBytes(ctx, "old-index-committed", windowsCatalogSeedMaxSize)
		if err != nil || windowsBytesDigest(preimage) != journal.BackupManifestSHA256 {
			return out, errors.Join(ErrStoreRecoveryRequired, err)
		}
		previous[0] = preimage
	}
	if err := verifyStagedWindowsAuthorities(ctx, stores, journal.OldStores, previous, journal.IndexRollback != nil); err != nil {
		return out, err
	}
	if err := verifyStagedWindowsAuthorities(ctx, stores, journal.NewStores, next, false); err != nil {
		return out, err
	}
	if journal.Phase == "PREPARED" {
		if err := o.advanceRestore(ctx, journal, "REPLACING"); err != nil {
			return out, err
		}
	}
	if journal.IndexRollback != nil {
		if err := o.archiveIndexSidecars(ctx, stores[0], journal, boundary); err != nil {
			return out, err
		}
	}
	for i := completed; i < len(files); i++ {
		if err := o.check(); err != nil {
			return out, err
		}
		proof := journal.Files[i]
		if err := files[i].replace(ctx, proof, next[i]); err != nil {
			return out, err
		}
		if boundary != nil {
			boundary(proof.Step)
		}
		journal.CompletedSteps = append(journal.CompletedSteps, proof.Step)
		if err := o.advanceRestore(ctx, journal, "REPLACING"); err != nil {
			return out, err
		}
	}
	if journal.Phase != "VERIFYING" {
		if err := o.advanceRestore(ctx, journal, "VERIFYING"); err != nil {
			return out, err
		}
	}
	if boundary != nil {
		boundary("VERIFYING")
	}
	if err := o.verifyRestored(ctx, stores, journal); err != nil {
		return out, err
	}
	if preserved != nil {
		if err := o.verifyPreservedRecords(ctx, *preserved, journal.IndexRollback.Records); err != nil {
			return out, err
		}
	}
	journal.Outcome = "restored"
	if err := o.advanceRestore(ctx, journal, "VERIFIED"); err != nil {
		return out, err
	}
	return windowsRestoreSummary(*journal), nil
}

func windowsRestoreSummary(journal MaintenanceJournal) ProtectedRestoreSummary {
	return ProtectedRestoreSummary{OperationID: journal.OperationID, Target: journal.Target,
		WorktreeID: journal.WorktreeID, Outcome: journal.Outcome, CompletedSteps: slices.Clone(journal.CompletedSteps)}
}

func verifyStagedWindowsAuthorities(ctx context.Context, stores []windowsMaintenanceStore, proofs []MaintenanceStore, files [][]byte, pending bool) error {
	for i, store := range stores {
		key, err := store.head.requiredBytes(ctx, windowsCatalogHeadKey, 32)
		if err != nil || len(key) != 32 {
			clear(key)
			return errors.Join(ErrStateChanged, err)
		}
		identity, err := store.identity(proofs[i].Incarnation)
		if err != nil {
			clear(key)
			return err
		}
		replay := replayWindowsWALAuthority
		if pending {
			replay = replayWindowsIndexRollbackAuthority
		}
		authority, err := replay(ctx, identity, [32]byte(key), files[i+len(stores)], files[i+2*len(stores)])
		clear(key)
		clear(authority.key[:])
		main := files[i]
		if err != nil || (authority.pending != nil) != pending || len(main) < 100 {
			return errors.Join(ErrStoreRecoveryRequired, err)
		}
		image, err := digestCheckpointedMain(ctx, bytes.NewReader(main), int64(len(main)),
			binary.BigEndian.Uint32(main[28:32]), windowsCatalogSeedMaxSize)
		if err != nil || image != authority.head.Image || hex.EncodeToString(image.SHA256[:]) != proofs[i].LogicalSHA256 {
			return errors.Join(ErrStateChanged, err)
		}
	}
	return nil
}

func (o windowsStoreMaintenance) advanceRestore(ctx context.Context, j *MaintenanceJournal, phase string) error {
	if err := o.check(); err != nil {
		return err
	}
	j.Phase = phase
	return o.files.write(ctx, *j, true, nil)
}

func (f windowsRestoreFile) fileDigest(ctx context.Context) (string, error) {
	data, err := f.files.requiredBytes(ctx, f.name, f.limit)
	if err != nil {
		return "", err
	}
	return windowsBytesDigest(data), nil
}

func (f windowsRestoreFile) replace(ctx context.Context, proof MaintenanceFileProof, next []byte) (resultErr error) {
	got, err := f.fileDigest(ctx)
	if err != nil || got != proof.OldSHA256 && got != proof.NewSHA256 {
		return errors.Join(ErrStateChanged, err)
	}
	if got == proof.NewSHA256 {
		return nil
	}
	dir, err := f.files.directory(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	desc, err := f.files.acl.descriptor()
	if err != nil {
		return err
	}
	if err := f.files.publish(ctx, dir, desc, f.name, next, true, nil); err != nil {
		return err
	}
	got, err = f.fileDigest(ctx)
	if err != nil || got != proof.NewSHA256 {
		return errors.Join(ErrStateChanged, err)
	}
	return nil
}

func (o windowsStoreMaintenance) verifyRestored(ctx context.Context, stores []windowsMaintenanceStore, journal *MaintenanceJournal) error {
	ids, err := o.incarnations(ctx)
	if err != nil {
		return err
	}
	hold, err := o.lease.Hold(ctx)
	if err != nil {
		return err
	}
	return errors.Join(o.verifyRestoredImages(ctx, stores, ids, journal), hold.Close())
}

func (o windowsStoreMaintenance) verifyRestoredImages(ctx context.Context, stores []windowsMaintenanceStore, ids []string, journal *MaintenanceJournal) error {
	for i, store := range stores {
		proof := journal.NewStores[i]
		if ids[i] != proof.Incarnation {
			return ErrStateChanged
		}
		raw, _, key, err := store.capture(ctx, ids[i])
		clear(key[:])
		if err != nil || windowsBytesDigest(raw) != proof.LogicalSHA256 {
			return errors.Join(ErrStateChanged, err)
		}
		if err := withWindowsSQLiteImage(ctx, raw, func(db *sql.DB) error {
			return store.verifyImage(ctx, db, o.lease, ids[i])
		}); err != nil {
			return err
		}
	}
	return o.check()
}
