//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

const restoreWorkerRole = "__restore-worker-v1:"

type restoreWorkerPlan struct {
	Operation    string
	Profile      string
	Scope        string
	Backup       string
	ManifestHash string
	NewIDs       []string
	RebuildIndex bool `json:",omitempty"`
}

type restoreWorkerReply struct {
	Operation string
	Old, New  []MaintenanceStore
	Error     string
}

func serveRestoreWorker(ctx context.Context, root, role string, input io.Reader, output io.Writer) (resultErr error) {
	args := strings.Split(strings.TrimPrefix(role, restoreWorkerRole), ":")
	if len(args) != 4 || !model.ValidUUID(args[1]) || args[0] != "catalog" && !model.ValidUUID(args[0]) || args[3] != "prepare" && args[3] != "recover" {
		return ErrProfileInput
	}
	wait, err := strconv.Atoi(args[2])
	if err != nil || wait < 1 || wait > 30000 {
		return ErrProfileInput
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	scope, operation := args[0], args[1]
	recovering := args[3] == "recover"
	recovery := ""
	if recovering {
		recovery = operation
	}
	o, err := openQuiescentOperation(ctx, root, scope, time.Duration(wait)*time.Millisecond, func(ctx context.Context) error { return ctx.Err() }, recovery, recovering)
	reply := restoreWorkerReply{Operation: operation}
	send := func() error { return json.NewEncoder(output).Encode(reply) }
	if err != nil {
		reply.Error = "denied"
		if errors.Is(err, runtime.ErrStoreMaintenance) || errors.Is(err, runtime.ErrWorktreeBusy) {
			reply.Error = "busy"
		}
		return errors.Join(err, send())
	}
	defer func() { resultErr = errors.Join(resultErr, o.Close()) }()
	if recovering {
		if err := o.profile.Check(ctx); err != nil {
			return err
		}
	}
	if err = send(); err != nil {
		return err
	}
	if !recovering {
		var start [1]byte
		if _, err = io.ReadFull(input, start[:]); err != nil || start[0] != 'P' {
			return errors.Join(ErrProfileIncomplete, err)
		}
		reply.Old, reply.New, err = o.prepareRestore(ctx, operation)
		if err != nil {
			reply.Error = "preparation_failed"
			return errors.Join(err, send())
		}
		if err = send(); err != nil {
			return err
		}
	}
	// The controller closes the pipe only after it has stopped exchanging
	// files. Controller death also releases the worker and both leases.
	var end [1]byte
	if n, err := input.Read(end[:]); n != 0 || err != io.EOF {
		return ErrProfileInput
	}
	return nil
}

func (o *quiescentOwner) prepareRestore(ctx context.Context, operation string) (oldStores, newStores []MaintenanceStore, resultErr error) {
	l := layoutRestore(o.profile.root, o.profile.manifest, o.scope, operation)
	planRoot, err := l.root.OpenDirectory(l.controlStage)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, planRoot.Close()) }()
	if err := rootManagedAncestors(ctx, planRoot, true); err != nil {
		return nil, nil, err
	}
	plan, err := readRestorePlan(ctx, planRoot)
	if err != nil || plan.Operation != operation || plan.Profile != o.profile.manifest.ProfileID || plan.Scope != o.scope {
		return nil, nil, errors.Join(ErrProfileDenied, err)
	}
	if plan.RebuildIndex {
		return o.prepareIndexRebuild(ctx, planRoot, operation)
	}
	if len(plan.NewIDs) != len(o.stores) {
		return nil, nil, ErrProfileDenied
	}
	backup, err := o.domain.Child(ctx, "backups/"+plan.Backup)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, backup.root.Close()) }()
	m := o.profile.manifest
	raw, digest, err := readRestoreInputFile(ctx, backup.root, "backup.json", backupManifestMaxBytes, 0, m.EngineUID, m.EngineGID, true)
	if err != nil || digest != plan.ManifestHash {
		return nil, nil, errors.Join(ErrStateChanged, err)
	}
	manifest, err := DecodeStoreBackupManifest(raw)
	if err != nil || manifest.ProfileInstallationID != m.ProfileID || len(manifest.Stores) != len(o.stores) || o.scope != "catalog" && manifest.WorktreeID != o.scope {
		return nil, nil, errors.Join(ErrProfileInput, err)
	}
	stagingRelative := "backups/.restore-" + operation
	stage, err := o.domain.Child(ctx, stagingRelative+"/db")
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, stage.root.Close()) }()
	// Validate and stage the entire pair before checkpointing either live DB.
	for i, entry := range manifest.Stores {
		store := o.stores[i]
		if entry.FileName != store.name {
			return nil, nil, ErrProfileInput
		}
		image, err := o.authenticatedImage(ctx, store)
		if err != nil {
			return nil, nil, err
		}
		if err := o.openReadPool(ctx, store); err != nil {
			return nil, nil, err
		}
		metadata, metaErr := o.sourceMetadata(ctx, store)
		if err := errors.Join(metaErr, o.closeReadPool()); err != nil {
			return nil, nil, err
		}
		oldStores = append(oldStores, MaintenanceStore{entry.Kind, metadata.Incarnation, hex.EncodeToString(image.SHA256[:])})
		image, err = o.stageRestoreDatabase(ctx, backup, stage, entry, plan.NewIDs[i])
		if err != nil {
			return nil, nil, err
		}
		if err = o.bootstrapRestoreAuthority(ctx, store, planRoot, stagingRelative, entry.Kind, plan.NewIDs[i], image); err != nil {
			return nil, nil, err
		}
		newStores = append(newStores, MaintenanceStore{entry.Kind, plan.NewIDs[i], hex.EncodeToString(image.SHA256[:])})
	}
	for i, store := range o.stores {
		if err := o.checkpointRestoreOriginal(ctx, store, oldStores[i].LogicalSHA256); err != nil {
			return nil, nil, err
		}
	}
	return oldStores, newStores, o.check(ctx)
}

func readRestorePlan(ctx context.Context, root *paths.Root) (restoreWorkerPlan, error) {
	var plan restoreWorkerPlan
	before, err := fingerprintRestoreFile(ctx, root, "request.json", 0, 0, 0444, profileManifestLimit, false)
	if err != nil {
		return plan, err
	}
	f, err := root.OpenRegular("request.json")
	if err != nil {
		return plan, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, profileManifestLimit+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return plan, err
	}
	after, err := fingerprintRestoreFile(ctx, root, "request.json", 0, 0, 0444, profileManifestLimit, false)
	if err != nil || after != before || model.ValidateWireJSON(raw, profileManifestLimit) != nil {
		return plan, ErrProfileInput
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&plan) != nil {
		return restoreWorkerPlan{}, ErrProfileInput
	}
	if plan.RebuildIndex {
		if !model.ValidUUID(plan.Scope) || plan.Backup != "" || plan.ManifestHash != "" || len(plan.NewIDs) != 0 {
			return restoreWorkerPlan{}, ErrProfileInput
		}
	} else if !model.ValidDigest(plan.ManifestHash) || plan.Backup == "" || strings.Contains(plan.Backup, "/") || model.ValidateRelativePath(plan.Backup, false) != nil {
		return restoreWorkerPlan{}, ErrProfileInput
	}
	return plan, nil
}

func (o *quiescentOwner) bootstrapRestoreAuthority(ctx context.Context, original *nativeWALStore, control *paths.Root, stage, kind, incarnation string, image walLogicalImage) (resultErr error) {
	journal, err := o.domain.Child(ctx, stage+"/journal-"+kind)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, journal.root.Close()) }()
	headRoot, err := control.OpenDirectory("head-" + kind)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, headRoot.Close()) }()
	next := *original
	next.journal, next.control = journal, headRoot
	next.identity.Incarnation, err = profileUUIDBytes(incarnation)
	if err != nil {
		return err
	}
	if err := profileFile(journal.root, walAuthorityName, nil, uint32(unix.Geteuid()), uint32(unix.Getegid()), 0600, false); err != nil {
		return err
	}
	log, head, err := next.openLog(ctx, &image)
	if err != nil {
		return err
	}
	receipt, readErr := log.Head(ctx)
	if err := errors.Join(readErr, log.Close(), head.Close()); err != nil {
		return err
	}
	if receipt.Sequence != 0 || receipt.Image != image {
		return ErrStateChanged
	}
	return nil
}

func (o *quiescentOwner) checkpointRestoreOriginal(ctx context.Context, store *nativeWALStore, expected string) error {
	before, err := o.authenticatedImage(ctx, store)
	if err != nil || hex.EncodeToString(before.SHA256[:]) != expected {
		return errors.Join(ErrStateChanged, err)
	}
	base, err := o.domain.root.CanonicalPath()
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(base, store.name)))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	o.pool, err = OpenMaintenanceOwner(ctx, o.lease, db, o.scope)
	if err != nil {
		return errors.Join(err, db.Close())
	}
	var busy, pages, done int
	checkErr := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &done)
	if err := errors.Join(checkErr, o.closeReadPool()); err != nil {
		return err
	}
	if busy != 0 || pages != done {
		return ErrStateUnsafe
	}
	if err := checkNoRestoreSidecars(o.domain.root, store.name); err != nil {
		return err
	}
	after, err := o.authenticatedImage(ctx, store)
	if err != nil || after != before {
		return errors.Join(ErrStateChanged, err)
	}
	return nil
}
