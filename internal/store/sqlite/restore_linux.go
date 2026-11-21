//go:build linux

package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

// RestoreProtectedStore operates on one installation and one backup chosen by
// its OS administrator. SQLite and the store lifecycle stay in the pinned,
// unprivileged worker process. The controller only installs verified files.
func RestoreProtectedStore(ctx context.Context, root, manifest string, confirmed bool, wait time.Duration) (ProtectedRestoreSummary, error) {
	return restoreProtectedStore(ctx, root, manifest, confirmed, wait, nil)
}

func restoreProtectedStore(ctx context.Context, root, manifest string, confirmed bool, wait time.Duration, boundary func(string)) (out ProtectedRestoreSummary, resultErr error) {
	if !confirmed || wait <= 0 || wait > RestoreMaxWait {
		return out, ErrProfileInput
	}
	target, worktree, scope, err := restoreInputScope(root, manifest)
	if err != nil {
		return out, err
	}
	input, err := openProtectedRestoreInput(ctx, root, target, worktree, manifest)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, input.root.Close()) }()
	control, err := openRestoreController(ctx, root, scope)
	if err != nil {
		return out, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, control.close())
		if resultErr != nil {
			out = ProtectedRestoreSummary{}
		}
	}()
	if control.profile != nil && control.profile.ProfileID != input.profile.ProfileID {
		return out, ErrStateChanged
	}
	old, present, err := readMaintenanceJournal(ctx, control.journal, control.profile.ProfileID, scope)
	if err != nil || present && old.Phase != "VERIFIED" {
		return out, errors.Join(ErrStoreRecoveryRequired, err)
	}
	operation, err := profileUUID()
	if err != nil {
		return out, err
	}
	layout := layoutRestore(control.root, *control.profile, scope, operation)
	var plan restoreWorkerPlan
	executable, err := restoreWorkerExecutable(ctx, *control.profile)
	if err != nil {
		return out, err
	}
	prepare := func() error {
		var err error
		plan, err = layout.createRestoreStage(ctx, input, scope, operation)
		return err
	}
	err = withRestoreWorker(ctx, executable, root, *control.profile, scope, operation, wait, prepare, func(reply restoreWorkerReply, alive func() error) error {
		journal := MaintenanceJournal{
			FormatVersion: 1, OperationID: operation, ProfileInstallationID: control.profile.ProfileID,
			Target: target, WorktreeID: worktree, BackupDirectory: plan.Backup, BackupManifestSHA256: plan.ManifestHash,
			Phase: "PREPARED", PermissionReviewConfirmed: true, OldStores: reply.Old, NewStores: reply.New,
			Files: []MaintenanceFileProof{}, CompletedSteps: []string{},
		}
		if len(reply.New) != len(plan.NewIDs) || len(reply.Old) != len(plan.NewIDs) {
			return ErrProfileIncomplete
		}
		for i, id := range plan.NewIDs {
			if reply.New[i].Incarnation != id {
				return ErrStateChanged
			}
		}
		if err := layout.stageManifest(ctx, journal); err != nil {
			return err
		}
		for _, step := range layout.exchanges(target) {
			a, err := layout.fingerprint(ctx, step.live, step.kind)
			if err != nil {
				return err
			}
			b, err := layout.fingerprint(ctx, step.staged, step.kind)
			if err != nil {
				return err
			}
			if step.kind == "main" {
				i := 0
				if step.step == "records_main" {
					i = 1
				}
				if a != journal.OldStores[i].LogicalSHA256 || b != journal.NewStores[i].LogicalSHA256 {
					return ErrStateChanged
				}
			}
			journal.Files = append(journal.Files, MaintenanceFileProof{step.step, a, b})
		}
		if err := alive(); err != nil {
			return err
		}
		if err := writeMaintenanceJournal(ctx, control.journal, journal, false); err != nil {
			return errors.Join(ErrStoreRecoveryRequired, err)
		}
		if boundary != nil {
			boundary("PREPARED")
		}
		// A lost worker before journal publication may have admitted readers.
		// A new recovery invocation must drain them before any exchange.
		if err := alive(); err != nil {
			return errors.Join(ErrStoreRecoveryRequired, err)
		}
		if err := layout.finishRestore(ctx, control.journal, &journal, boundary); err != nil {
			return errors.Join(ErrStoreRecoveryRequired, err)
		}
		out = restoreSummary(journal)
		return nil
	})
	return out, err
}

func restoreInputScope(root, manifest string) (target, worktree, scope string, err error) {
	if !validProfilePath(root) || !filepath.IsAbs(manifest) || filepath.Clean(manifest) != manifest || filepath.Base(manifest) != "backup.json" {
		return "", "", "", ErrProfileInput
	}
	relative, err := filepath.Rel(profileStatePath(root), manifest)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) == 3 && parts[0] == "backups" {
		return "catalog", "", "catalog", nil
	}
	if len(parts) == 5 && parts[0] == "worktrees" && model.ValidUUID(parts[1]) && parts[2] == "backups" {
		return "worktree", parts[1], parts[1], nil
	}
	return "", "", "", ErrProfileInput
}

// RecoverProtectedStore is an explicit confirmed roll-forward. An unknown or
// inconsistent pair stops without guessing at file identities. The archived
// old files stay available for an administrator to diagnose.
func RecoverProtectedStore(ctx context.Context, root, target, worktree string, confirmed bool, wait time.Duration) (out ProtectedRestoreSummary, resultErr error) {
	if !confirmed || wait <= 0 || wait > RestoreMaxWait {
		return out, ErrProfileInput
	}
	scope := "catalog"
	switch target {
	case "worktree":
		if !model.ValidUUID(worktree) {
			return out, ErrProfileInput
		}
		scope = worktree
	case "catalog":
		if worktree != "" {
			return out, ErrProfileInput
		}
	default:
		return out, ErrProfileInput
	}
	c, err := openRestoreController(ctx, root, scope)
	if err != nil {
		return out, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, c.close())
		if resultErr != nil {
			out = ProtectedRestoreSummary{}
		}
	}()
	j, present, err := readMaintenanceJournal(ctx, c.journal, c.profile.ProfileID, scope)
	if err != nil || !present || j.Phase == "NEEDS_RECOVERY" {
		return out, errors.Join(ErrStoreRecoveryRequired, err)
	}
	if j.Phase == "VERIFIED" {
		return restoreSummary(j), nil
	}
	executable, err := restoreWorkerExecutable(ctx, *c.profile)
	if err != nil {
		return out, err
	}
	l := layoutRestore(c.root, *c.profile, scope, j.OperationID)
	err = withRestoreWorker(ctx, executable, root, *c.profile, scope, j.OperationID, wait, nil, func(_ restoreWorkerReply, alive func() error) error {
		if err := alive(); err != nil {
			return err
		}
		if err := l.finishRestore(ctx, c.journal, &j, nil); err != nil {
			return err
		}
		out = restoreSummary(j)
		return nil
	})
	if err != nil {
		return ProtectedRestoreSummary{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return out, nil
}

func restoreSummary(j MaintenanceJournal) ProtectedRestoreSummary {
	return ProtectedRestoreSummary{j.OperationID, j.Target, j.WorktreeID, j.Outcome, append([]string{}, j.CompletedSteps...)}
}

type restoreController struct {
	root, journal *paths.Root
	profile       *protectedProfileManifest
	close         func() error
}

func openRestoreController(ctx context.Context, root, scope string) (_ *restoreController, resultErr error) {
	if administratorProfileProcess(ctx) != nil || !validProfilePath(root) {
		return nil, ErrProfileDenied
	}
	r, err := paths.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, r.Close())
		}
	}()
	l := layoutRestore(r, protectedProfileManifest{}, scope, "")
	j, err := r.OpenDirectory(l.control)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, j.Close())
		}
	}()
	if err := rootManagedAncestors(ctx, j, true); err != nil {
		return nil, err
	}
	f, err := openRestoreDirectory(j)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, f.Close())
		}
	}()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(runtime.ErrStoreMaintenance, err)
	}
	m, _, err := readProfileManifest(ctx, r)
	if err != nil || m.Stage != "READY" {
		return nil, errors.Join(ErrProfileDenied, err)
	}
	return &restoreController{r, j, &m, func() error { return errors.Join(f.Close(), j.Close(), r.Close()) }}, nil
}

func restoreWorkerExecutable(ctx context.Context, m protectedProfileManifest) (string, error) {
	name, digest, err := trustedProfileExecutable(ctx)
	if m.WorkerExecutable != "" {
		name, digest, err = trustedProfileExecutablePath(ctx, m.WorkerExecutable)
	}
	if err != nil || digest != m.BootstrapExecutableSHA256 {
		return "", errors.Join(ErrProfileDenied, err)
	}
	return name, nil
}

func withRestoreWorker(ctx context.Context, executable, root string, m protectedProfileManifest, scope, operation string, wait time.Duration, prepare func() error, use func(restoreWorkerReply, func() error) error) (resultErr error) {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	mode := "prepare"
	if prepare == nil {
		mode = "recover"
	}
	role := restoreWorkerRole + scope + ":" + operation + ":" + strconv.FormatInt(wait.Milliseconds(), 10) + ":" + mode
	cmd := exec.CommandContext(ctx, executable, role, root)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: m.EngineUID, Gid: m.EngineGID, Groups: []uint32{}}, Pdeathsig: syscall.SIGKILL}
	cmd.Env = []string{"HOME=" + filepath.Join(root, "home"), "PATH=/usr/bin:/bin", "GOMAXPROCS=4", "MODERNC_SQLITE_OFD_LOCK=0"}
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return errors.Join(err, input.Close())
	}
	if err := cmd.Start(); err != nil {
		return errors.Join(err, input.Close(), output.Close())
	}
	defer func() {
		resultErr = errors.Join(resultErr, input.Close(), cmd.Wait())
	}()
	pidfd, err := unix.PidfdOpen(cmd.Process.Pid, 0)
	if err != nil {
		return err
	}
	defer unix.Close(pidfd)
	alive := func() error {
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 0)
		if err != nil || n != 0 {
			return errors.Join(ErrProfileIncomplete, err)
		}
		return ctx.Err()
	}
	reader := bufio.NewReaderSize(output, 8192)
	read := func() (restoreWorkerReply, error) {
		var reply restoreWorkerReply
		raw, err := reader.ReadSlice('\n')
		if err != nil || model.ValidateWireJSON(raw, 8192) != nil {
			return reply, errors.Join(ErrProfileIncomplete, err)
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if d.Decode(&reply) != nil || reply.Operation != operation {
			return reply, ErrProfileIncomplete
		}
		switch reply.Error {
		case "busy":
			return reply, runtime.ErrStoreMaintenance
		case "":
			return reply, nil
		default:
			return reply, ErrProfileIncomplete
		}
	}
	reply, err := read()
	if err != nil {
		return err
	}
	if prepare != nil {
		if err := alive(); err != nil {
			return err
		}
		if err := prepare(); err != nil {
			return err
		}
		if _, err := input.Write([]byte{'P'}); err != nil {
			return err
		}
		reply, err = read()
		if err != nil {
			return err
		}
	}
	return use(reply, alive)
}

func (l restoreLayout) createRestoreStage(ctx context.Context, input *protectedRestoreInput, scope, operation string) (plan restoreWorkerPlan, resultErr error) {
	m := l.profile
	plan = restoreWorkerPlan{Operation: operation, Profile: m.ProfileID, Scope: scope, Backup: filepath.Base(input.directory), ManifestHash: input.manifestSHA256}
	if err := profileDirectory(l.root, l.controlStage, 0, 0, 0755); err != nil {
		return plan, err
	}
	if err := profileDirectory(l.root, l.stateStage, 0, 0, 0711); err != nil {
		return plan, err
	}
	if err := profileDirectory(l.root, l.stateStage+"/db", m.EngineUID, m.EngineGID, 0700); err != nil {
		return plan, err
	}
	c, err := l.root.OpenDirectory(l.controlStage)
	if err != nil {
		return plan, err
	}
	defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
	for _, entry := range input.manifest.Stores {
		id, err := profileUUID()
		if err != nil {
			return plan, err
		}
		plan.NewIDs = append(plan.NewIDs, id)
		if err := profileDirectory(l.root, l.stateStage+"/journal-"+entry.Kind, m.EngineUID, m.EngineGID, 0700); err != nil {
			return plan, err
		}
		name := "head-" + entry.Kind
		if err := profileDirectory(c, name, 0, 0, 0755); err != nil {
			return plan, err
		}
		h, err := c.OpenDirectory(name)
		if err != nil {
			return plan, err
		}
		if err := errors.Join(profileFile(h, walHeadName, nil, m.EngineUID, m.EngineGID, 0600, true), h.Close()); err != nil {
			return plan, err
		}
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	return plan, errors.Join(ctx.Err(), profileFile(c, "request.json", raw, 0, 0, 0444, false))
}

func (l restoreLayout) stageManifest(ctx context.Context, j MaintenanceJournal) (resultErr error) {
	c, err := l.root.OpenDirectory(l.controlStage)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
	var next any
	if j.Target == "catalog" {
		m, _, err := readProfileManifest(ctx, l.root)
		if err != nil || m.ProfileID != l.profile.ProfileID || m.CatalogIncarnation != j.OldStores[0].Incarnation {
			return errors.Join(ErrStateChanged, err)
		}
		m.CatalogIncarnation = j.NewStores[0].Incarnation
		next = m
	} else {
		w, err := l.root.OpenDirectory(l.control)
		if err != nil {
			return err
		}
		m, _, readErr := readWorktreeManifest(ctx, w, l.profile.ProfileID, j.WorktreeID)
		if err := errors.Join(readErr, w.Close()); err != nil {
			return err
		}
		if m.IndexIncarnation != j.OldStores[0].Incarnation || m.RecordsIncarnation != j.OldStores[1].Incarnation {
			return ErrStateChanged
		}
		m.IndexIncarnation, m.RecordsIncarnation = j.NewStores[0].Incarnation, j.NewStores[1].Incarnation
		next = m
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return profileFile(c, "manifest.json", raw, 0, 0, 0444, false)
}

func (l restoreLayout) finishRestore(ctx context.Context, control *paths.Root, j *MaintenanceJournal, boundary func(string)) error {
	steps := l.exchanges(j.Target)
	if len(steps) != len(j.Files) {
		return ErrStateChanged
	}
	completed := len(j.CompletedSteps)
	// Check the entire pair set before another exchange. An interrupted rename
	// can be exactly one step ahead of the durable journal.
	for i, step := range steps {
		swapped, err := l.restorePairState(ctx, step, j.Files[i])
		if err != nil {
			return err
		}
		if i < completed && !swapped || i > completed && swapped || i == completed && swapped && j.Phase != "REPLACING" {
			return ErrStateChanged
		}
	}
	if j.Phase == "PREPARED" {
		j.Phase = "REPLACING"
		if err := writeMaintenanceJournal(ctx, control, *j, true); err != nil {
			return err
		}
		if boundary != nil {
			boundary("REPLACING")
		}
	}
	if j.Phase == "REPLACING" {
		for i := completed; i < len(steps); i++ {
			step := steps[i]
			swapped, err := l.restorePairState(ctx, step, j.Files[i])
			if err != nil {
				return err
			}
			if !swapped {
				if err := l.exchange(ctx, step, boundary); err != nil {
					return err
				}
				if boundary != nil {
					boundary("exchange:" + step.step)
				}
			} else {
				// The previous process may have stopped just after rename,
				// before synchronizing either directory.
				if err := l.syncExchangeParents(step); err != nil {
					return err
				}
			}
			j.CompletedSteps = append(j.CompletedSteps, step.step)
			if err := writeMaintenanceJournal(ctx, control, *j, true); err != nil {
				return err
			}
			if boundary != nil {
				boundary("record:" + step.step)
			}
		}
		j.Phase = "VERIFYING"
		if err := writeMaintenanceJournal(ctx, control, *j, true); err != nil {
			return err
		}
		if boundary != nil {
			boundary("VERIFYING")
		}
	}
	if j.Phase != "VERIFYING" {
		return ErrStoreRecoveryRequired
	}
	for i, step := range steps {
		swapped, err := l.restorePairState(ctx, step, j.Files[i])
		if err != nil || !swapped {
			return errors.Join(ErrStateChanged, err)
		}
	}
	j.Phase, j.Outcome = "VERIFIED", "restored"
	return writeMaintenanceJournal(ctx, control, *j, true)
}

func (l restoreLayout) restorePairState(ctx context.Context, step restoreExchange, proof MaintenanceFileProof) (bool, error) {
	a, err := l.fingerprint(ctx, step.live, step.kind)
	if err != nil {
		return false, fmt.Errorf("%s live: %w", step.step, err)
	}
	b, err := l.fingerprint(ctx, step.staged, step.kind)
	if err != nil {
		return false, fmt.Errorf("%s staged: %w", step.step, err)
	}
	if proof.Step != step.step {
		return false, ErrStateChanged
	}
	if a == proof.OldSHA256 && b == proof.NewSHA256 {
		return false, nil
	}
	if a == proof.NewSHA256 && b == proof.OldSHA256 {
		return true, nil
	}
	return false, fmt.Errorf("%s: %w", step.step, ErrStateChanged)
}

func (l restoreLayout) syncExchangeParents(step restoreExchange) error {
	for _, name := range []string{step.live, step.staged} {
		parent, _, closeParent, err := enterRestoreParent(l.root, name)
		if err != nil {
			return err
		}
		if err := errors.Join(syncWALDirectory(parent), closeParent()); err != nil {
			return err
		}
	}
	return nil
}
