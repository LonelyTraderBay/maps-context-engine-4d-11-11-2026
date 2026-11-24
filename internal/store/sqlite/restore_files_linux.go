//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

const restoreStateRelative = "home/.local/state/mce4d"

type restoreLayout struct {
	root                                     *paths.Root
	profile                                  protectedProfileManifest
	control, state, stateStage, controlStage string
}

func layoutRestore(root *paths.Root, profile protectedProfileManifest, scope, operation string) restoreLayout {
	control, state := "control", restoreStateRelative
	if scope != "catalog" {
		control += "/worktrees/" + scope
		state += "/worktrees/" + scope
	}
	return restoreLayout{root, profile, control, state, state + "/backups/.restore-" + operation, control + "/restore-" + operation}
}

type restoreExchange struct {
	step, live, staged string
	kind               string
}

func (l restoreLayout) exchanges(j MaintenanceJournal) []restoreExchange {
	makeStore := func(kind, head string) []restoreExchange {
		return []restoreExchange{
			{kind + "_main", l.state + "/" + kind + ".sqlite", l.stateStage + "/db/" + kind + ".sqlite", "main"},
			{kind + "_journal", l.state + "/.maintenance/" + kind, l.stateStage + "/journal-" + kind, "journal"},
			{kind + "_head", head, l.controlStage + "/head-" + kind, "head"},
		}
	}
	if j.Target == "catalog" {
		return append(makeStore("catalog", "control/catalog"), restoreExchange{"profile_manifest", "profile.json", l.controlStage + "/manifest.json", "manifest"})
	}
	a := makeStore("index", l.control+"/index")
	manifest := restoreExchange{"worktree_manifest", l.control + "/worktree.json", l.controlStage + "/manifest.json", "manifest"}
	if j.IndexRebuild != nil {
		return append(a, manifest)
	}
	b := makeStore("records", l.control+"/records")
	return []restoreExchange{a[0], b[0], a[1], b[1], a[2], b[2], manifest}
}

func (l restoreLayout) provePreservedRecords(ctx context.Context, worktree string) (_ MaintenanceIndexRebuild, resultErr error) {
	control, err := l.root.OpenDirectory(l.control)
	if err != nil {
		return MaintenanceIndexRebuild{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, control.Close()) }()
	m, _, err := readWorktreeManifest(ctx, control, l.profile.ProfileID, worktree)
	if err != nil {
		return MaintenanceIndexRebuild{}, err
	}
	digest, err := l.recordsFingerprint(ctx)
	return MaintenanceIndexRebuild{RecordsIncarnation: m.RecordsIncarnation, RecordsSHA256: digest}, err
}

func (l restoreLayout) checkPreservedRecords(ctx context.Context, j *MaintenanceJournal) error {
	if j.IndexRebuild == nil {
		return nil
	}
	digest, err := l.recordsFingerprint(ctx)
	if err != nil || digest != j.IndexRebuild.RecordsSHA256 {
		return errors.Join(ErrStateChanged, err)
	}
	return nil
}

func (l restoreLayout) recordsFingerprint(ctx context.Context) (_ string, resultErr error) {
	state, err := l.root.OpenDirectory(l.state)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	h := sha256.New()
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		name := "records.sqlite" + suffix
		digest, err := fingerprintRestoreFile(ctx, state, name, l.profile.EngineUID, l.profile.EngineGID, 0600, walLogicalImageMaxBytes, false)
		if suffix != "" && errors.Is(err, os.ErrNotExist) {
			digest, err = "absent", nil
		}
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(h, name+"\x00"+digest+"\x00")
	}
	journal, err := l.fingerprint(ctx, l.state+"/.maintenance/records", "records_journal")
	if err != nil {
		return "", err
	}
	head, err := l.fingerprint(ctx, l.control+"/records", "head")
	if err != nil {
		return "", err
	}
	_, _ = io.WriteString(h, "journal\x00"+journal+"\x00head\x00"+head)
	return hex.EncodeToString(h.Sum(nil)), state.Verify()
}

func enterRestoreParent(root *paths.Root, name string) (*paths.Root, string, func() error, error) {
	if path.Dir(name) == "." {
		return root, path.Base(name), func() error { return nil }, nil
	}
	parent, err := root.OpenDirectory(path.Dir(name))
	if err != nil {
		return nil, "", nil, err
	}
	return parent, path.Base(name), parent.Close, nil
}

// File contents, ownership and exact directory members are part of the proof.
// The short SQLite authority lock is retained with the old journal directory.
func (l restoreLayout) fingerprint(ctx context.Context, name, kind string) (digest string, resultErr error) {
	uid, gid := l.profile.EngineUID, l.profile.EngineGID
	parent, leaf, closeParent, err := enterRestoreParent(l.root, name)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, closeParent()) }()
	switch kind {
	case "main":
		if err := checkNoRestoreSidecars(parent, leaf); err != nil {
			return "", err
		}
		_, hash, err := readRestoreInputFile(ctx, parent, leaf, walLogicalImageMaxBytes, 0, uid, gid, false)
		return hash, err
	case "manifest":
		return fingerprintRestoreFile(ctx, parent, leaf, 0, 0, 0444, profileManifestLimit, false)
	case "journal", "records_journal", "head":
		dir, err := parent.OpenDirectory(leaf)
		if err != nil {
			return "", err
		}
		defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
		mode, ownerUID, ownerGID := uint32(0700), uid, gid
		logName, limit := walAuthorityName, int64(walAuthorityMaxBytes)
		if kind == "head" {
			mode, ownerUID, ownerGID = 0755, 0, 0
			logName, limit = walHeadName, int64(walHeadMaxBytes)
			if err := rootManagedAncestors(ctx, dir, true); err != nil {
				return "", err
			}
		}
		h, err := openRestoreDirectory(dir)
		if err != nil {
			return "", err
		}
		var st unix.Stat_t
		statErr := unix.Fstat(int(h.Fd()), &st)
		entries, readErr := h.ReadDir(3)
		if errors.Is(readErr, io.EOF) {
			readErr = nil
		}
		if err := errors.Join(statErr, readErr, h.Close()); err != nil {
			return "", err
		}
		if st.Uid != ownerUID || st.Gid != ownerGID || st.Mode&0777 != mode || len(entries) == 0 || len(entries) > 2 {
			return "", ErrStateUnsafe
		}
		logHash, err := fingerprintRestoreFile(ctx, dir, logName, uid, gid, 0600, limit, kind == "head")
		if err != nil {
			return "", err
		}
		gateHash := ""
		for _, entry := range entries {
			if entry.Name() == logName {
				continue
			}
			if kind == "head" || entry.Name() != "lifecycle.lock" {
				return "", ErrStateUnsafe
			}
			gateHash, err = fingerprintRestoreFile(ctx, dir, entry.Name(), uid, gid, 0600, 1024, false)
			if err != nil {
				return "", err
			}
		}
		if kind == "records_journal" {
			// The first authenticated read can create the coordination lock.
			// Its ownership is checked above; durable records exclude the lock.
			gateHash = ""
		}
		hash := sha256.Sum256([]byte(kind + "\x00" + logHash + "\x00" + gateHash))
		return hex.EncodeToString(hash[:]), dir.Verify()
	default:
		return "", ErrProfileInput
	}
}

func fingerprintRestoreFile(ctx context.Context, root *paths.Root, name string, uid, gid, mode uint32, limit int64, appendOnly bool) (digest string, resultErr error) {
	f, err := root.OpenRegular(name)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil || st.Uid != uid || st.Gid != gid || st.Mode&0777 != mode || st.Nlink != 1 || st.Size < 0 || st.Size > limit {
		return "", errors.Join(ErrStateUnsafe, err)
	}
	if appendOnly {
		flags, err := unix.IoctlGetInt(int(f.Fd()), unix.FS_IOC_GETFLAGS)
		if err != nil || flags&linuxAppendFlag == 0 {
			return "", errors.Join(ErrStateUnsafe, err)
		}
	}
	hash, err := hashLogicalReader(ctx, f, st.Size)
	if err != nil {
		return "", err
	}
	var after unix.Stat_t
	statErr := unix.Fstat(int(f.Fd()), &after)
	after.Atim = st.Atim
	if statErr != nil || after != st {
		return "", errors.Join(ErrStateChanged, statErr)
	}
	return hex.EncodeToString(hash[:]), errors.Join(root.Verify(), paths.VerifyRegularFile(f))
}

func openRestoreDirectory(root *paths.Root) (*os.File, error) {
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	fd, openErr := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err := errors.Join(openErr, anchor.Close()); err != nil {
		if openErr == nil {
			_ = unix.Close(fd)
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), "restore-directory"), nil
}

func checkNoRestoreSidecars(root *paths.Root, name string) error {
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer anchor.Close()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		var st unix.Stat_t
		err := unix.Fstatat(int(anchor.Fd()), name+suffix, &st, unix.AT_SYMLINK_NOFOLLOW)
		if !errors.Is(err, unix.ENOENT) {
			return errors.Join(ErrStateUnsafe, err)
		}
	}
	return root.Verify()
}

func (l restoreLayout) exchange(ctx context.Context, step restoreExchange, boundary func(string)) (resultErr error) {
	live, name, closeLive, err := enterRestoreParent(l.root, step.live)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeLive()) }()
	staged, other, closeStaged, err := enterRestoreParent(l.root, step.staged)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeStaged()) }()
	a, err := openRestoreDirectory(live)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, a.Close()) }()
	b, err := openRestoreDirectory(staged)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, b.Close()) }()
	if err := errors.Join(ctx.Err(), live.Verify(), staged.Verify()); err != nil {
		return err
	}
	if err := unix.Renameat2(int(a.Fd()), name, int(b.Fd()), other, unix.RENAME_EXCHANGE); err != nil {
		return fmt.Errorf("exchange %s: %w", step.step, err)
	}
	if boundary != nil {
		boundary("rename:" + step.step)
	}
	return errors.Join(a.Sync(), b.Sync(), live.Verify(), staged.Verify())
}
