//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// ProvisionProtectedWorktree initializes a fresh owned index/records slot.
// It does not register a repository, grant target access, adopt an existing
// directory, migrate data or alter the profile installation manifest.
func ProvisionProtectedWorktree(ctx context.Context, rootPath, worktree string) (out ProtectedWorktreeSummary, resultErr error) {
	defer func() {
		if resultErr != nil {
			out = ProtectedWorktreeSummary{}
		}
	}()
	if administratorProfileProcess(ctx) != nil || !validProfilePath(rootPath) {
		return out, ErrProfileDenied
	}
	if _, err := profileUUIDBytes(worktree); err != nil {
		return out, err
	}
	executable, executableHash, err := trustedProfileExecutable(ctx)
	if err != nil {
		return out, err
	}
	root, err := paths.OpenRoot(rootPath)
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	m, _, err := readProfileManifest(ctx, root)
	if err != nil || m.Stage != "READY" {
		return out, ErrProfileDenied
	}
	if m.WorkerExecutable != "" {
		executable, executableHash, err = trustedProfileExecutablePath(ctx, m.WorkerExecutable)
		if err != nil {
			return out, err
		}
	}
	if executableHash != m.BootstrapExecutableSHA256 {
		return out, ErrProfileDenied
	}
	control, err := root.OpenDirectory("control")
	if err != nil {
		return out, err
	}
	defer control.Close()
	if err := profileDirectory(control, "worktrees", 0, 0, 0755); err != nil && !errors.Is(err, unix.EEXIST) {
		return out, err
	}
	parent, err := control.OpenDirectory("worktrees")
	if err != nil {
		return out, err
	}
	defer parent.Close()
	if rootManagedAncestors(ctx, parent, true) != nil {
		return out, ErrProfileDenied
	}
	if err := profileDirectory(parent, worktree, 0, 0, 0755); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return out, ErrProfileExists
		}
		return out, err
	}
	worktreeControl, err := parent.OpenDirectory(worktree)
	if err != nil {
		return out, err
	}
	defer worktreeControl.Close()
	manifest := protectedWorktreeManifest{FormatVersion: 1, ProfileInstallationID: m.ProfileID, WorktreeID: worktree, Stage: "CREATING"}
	raw, err := json.Marshal(manifest)
	if err != nil || profileFile(worktreeControl, "worktree.json", raw, 0, 0, 0444, false) != nil {
		return out, ErrProfileIncomplete
	}
	stateParent, err := root.OpenDirectory("home/.local/state/mce4d/worktrees")
	if err != nil {
		return out, err
	}
	defer stateParent.Close()
	if err := profileDirectory(stateParent, worktree, m.EngineUID, m.EngineGID, 0700); err != nil {
		return out, ErrProfileIncomplete
	}
	state, err := stateParent.OpenDirectory(worktree)
	if err != nil {
		return out, err
	}
	defer state.Close()
	if profileDirectory(state, ".maintenance", m.EngineUID, m.EngineGID, 0700) != nil {
		return out, ErrProfileIncomplete
	}
	for _, name := range []string{"scratch", "scratch/pack", "scratch/rename", "backups", "logs"} {
		if profileDirectory(state, name, m.EngineUID, m.EngineGID, 0700) != nil {
			return out, ErrProfileIncomplete
		}
	}
	for _, kind := range []string{"index", "records"} {
		if profileDirectory(state, ".maintenance/"+kind, m.EngineUID, m.EngineGID, 0700) != nil || profileDirectory(worktreeControl, kind, 0, 0, 0755) != nil || profileFile(state, kind+".sqlite", nil, m.EngineUID, m.EngineGID, 0600, false) != nil {
			return out, ErrProfileIncomplete
		}
		journal, err := state.OpenDirectory(".maintenance/" + kind)
		if err != nil {
			return out, err
		}
		err = profileFile(journal, walAuthorityName, nil, m.EngineUID, m.EngineGID, 0600, false)
		if err = errors.Join(err, journal.Close()); err != nil {
			return out, err
		}
		head, err := worktreeControl.OpenDirectory(kind)
		if err != nil {
			return out, err
		}
		err = profileFile(head, walHeadName, nil, m.EngineUID, m.EngineGID, 0600, true)
		if err = errors.Join(err, head.Close()); err != nil {
			return out, err
		}
	}
	workerOutput, err := runProfileWorkerBytes(ctx, executable, rootPath, "__bootstrap-worktree-v1:"+worktree, m)
	if err != nil {
		return out, err
	}
	d := json.NewDecoder(bytes.NewReader(workerOutput))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(&struct{}{}) != io.EOF || out.WorktreeID != worktree || !out.Verified {
		return ProtectedWorktreeSummary{}, ErrProfileIncomplete
	}
	manifest.IndexIncarnation, manifest.RecordsIncarnation = out.IndexIncarnation, out.RecordsIncarnation
	// No SQLite runs in the administrator: independently verify the exact fresh
	// main/BOOT/head bytes after the unprivileged bootstrap worker has exited.
	keyFile, err := control.OpenRegular("profile.key")
	if err != nil {
		return out, err
	}
	master, readErr := io.ReadAll(io.LimitReader(keyFile, 33))
	defer clear(master)
	keyHash := sha256.Sum256(master)
	if errors.Join(readErr, keyFile.Close()) != nil || len(master) != 32 || hex.EncodeToString(keyHash[:]) != m.KeyDigest {
		return out, ErrProfileDenied
	}
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("MCE4D-PROFILE-CATALOG1\x00" + m.ProfileID))
	var catalogKey [32]byte
	copy(catalogKey[:], mac.Sum(nil))
	defer clear(catalogKey[:])
	for _, item := range []struct{ kind, incarnation string }{{"index", out.IndexIncarnation}, {"records", out.RecordsIncarnation}} {
		identity, key, err := worktreeStoreIdentity(m, worktree, item.kind, item.incarnation, catalogKey)
		if err != nil {
			return out, err
		}
		err = verifyFreshWorktreeStore(ctx, state, worktreeControl, item.kind, m, identity, key)
		clear(key[:])
		if err != nil {
			return out, err
		}
	}
	current, _, err := readWorktreeManifest(ctx, worktreeControl, m.ProfileID, worktree)
	if err != nil || current.Stage != "CREATING" {
		return out, ErrProfileIncomplete
	}
	manifest.Stage = "READY"
	raw, err = json.Marshal(manifest)
	if err != nil || profileFile(worktreeControl, "worktree.ready.pending", raw, 0, 0, 0444, false) != nil {
		return out, ErrProfileIncomplete
	}
	dir, err := worktreeControl.DirectoryHandle()
	if err != nil {
		return out, err
	}
	err = unix.Renameat(int(dir.Fd()), "worktree.ready.pending", int(dir.Fd()), "worktree.json")
	return out, errors.Join(err, dir.Close(), syncWALDirectory(worktreeControl), ctx.Err())
}

func verifyFreshWorktreeStore(ctx context.Context, state, control *paths.Root, kind string, m protectedProfileManifest, identity walAuthorityIdentity, key [32]byte) error {
	read := func(root *paths.Root, name string, limit int64) ([]byte, error) {
		f, err := root.OpenRegular(name)
		if err != nil {
			return nil, err
		}
		var st unix.Stat_t
		statErr := unix.Fstat(int(f.Fd()), &st)
		data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
		closeErr := f.Close()
		if errors.Join(statErr, readErr, closeErr) != nil || st.Uid != m.EngineUID || st.Gid != m.EngineGID || st.Nlink != 1 || st.Mode&0077 != 0 || int64(len(data)) > limit {
			return nil, ErrProfileIncomplete
		}
		return data, ctx.Err()
	}
	main, err := read(state, kind+".sqlite", checkpointedSnapshotMaxBytes)
	if err != nil {
		return err
	}
	for _, sidecar := range []string{"-wal", "-journal"} {
		f, err := state.OpenRegular(kind + ".sqlite" + sidecar)
		if err == nil {
			return errors.Join(ErrProfileIncomplete, f.Close())
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	journal, err := state.OpenDirectory(".maintenance/" + kind)
	if err != nil {
		return err
	}
	defer journal.Close()
	raw, err := read(journal, walAuthorityName, walAuthorityRecordSize)
	if err != nil || len(raw) != walAuthorityRecordSize {
		return ErrProfileIncomplete
	}
	decoder := walAuthorityLog{identity: identity, key: key}
	record, err := decoder.decode(raw)
	if err != nil || decoder.apply(record, true) != nil || record.Image.SHA256 != sha256.Sum256(main) {
		return ErrProfileIncomplete
	}
	head, err := control.OpenDirectory(kind)
	if err != nil {
		return err
	}
	defer head.Close()
	if rootManagedAncestors(ctx, head, true) != nil {
		return ErrProfileDenied
	}
	headBytes, err := read(head, walHeadName, walHeadRecordSize)
	if err != nil {
		return err
	}
	f, err := head.OpenRegular(walHeadName)
	if err != nil {
		return err
	}
	flags, flagErr := unix.IoctlGetInt(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	closeErr := f.Close()
	encoder := walHeadAuthority{identity: identity, key: key}
	expected := encoder.encode(walJournalPosition{Size: walAuthorityRecordSize, SHA256: sha256.Sum256(raw)})
	if errors.Join(flagErr, closeErr) != nil || flags&linuxAppendFlag == 0 || !hmac.Equal(headBytes, expected) {
		return ErrProfileIncomplete
	}
	return ctx.Err()
}
