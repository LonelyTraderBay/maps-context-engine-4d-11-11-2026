//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	goruntime "runtime"
	"syscall"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func administratorProfileProcess(ctx context.Context) error {
	if ctx == nil {
		return ErrProfileDenied
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrProfileDenied
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if unix.Getuid() != 0 || unix.Geteuid() != 0 {
		return ErrProfileDenied
	}
	f, err := os.Open("/proc/self/uid_map")
	if err != nil {
		return ErrProfileDenied
	}
	var fs unix.Statfs_t
	statErr := unix.Fstatfs(int(f.Fd()), &fs)
	b, readErr := io.ReadAll(io.LimitReader(f, 128))
	closeErr := f.Close()
	if errors.Join(statErr, readErr, closeErr) != nil || uint64(fs.Type) != uint64(unix.PROC_SUPER_MAGIC) || string(bytes.Join(bytes.Fields(b), []byte{' '})) != "0 0 4294967295" {
		return ErrProfileDenied
	}
	return nil
}

func trustedProfileExecutable(ctx context.Context) (string, string, error) {
	name, err := os.Executable()
	if err != nil {
		return "", "", ErrProfileDenied
	}
	return trustedProfileExecutablePath(ctx, name)
}

func trustedProfileExecutablePath(ctx context.Context, name string) (string, string, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return "", "", ErrProfileDenied
	}
	root, err := paths.OpenRoot(filepath.Dir(name))
	if err != nil {
		return "", "", ErrProfileDenied
	}
	defer root.Close()
	if rootManagedAncestors(ctx, root, true) != nil {
		return "", "", ErrProfileDenied
	}
	f, err := root.OpenRegular(filepath.Base(name))
	if err != nil {
		return "", "", ErrProfileDenied
	}
	var st unix.Stat_t
	statErr := unix.Fstat(int(f.Fd()), &st)
	digest := sha256.New()
	n, readErr := io.Copy(digest, io.LimitReader(f, 64<<20+1))
	closeErr := f.Close()
	if errors.Join(statErr, readErr, closeErr) != nil || n > 64<<20 || st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&06000 != 0 {
		return "", "", ErrProfileDenied
	}
	return name, hex.EncodeToString(digest.Sum(nil)), ctx.Err()
}

func profileDirectory(root *paths.Root, relative string, uid, gid uint32, mode uint32) error {
	parent := root
	name := path.Base(relative)
	if dir := path.Dir(relative); dir != "." {
		var err error
		parent, err = root.OpenDirectory(dir)
		if err != nil {
			return err
		}
		defer parent.Close()
	}
	anchor, err := parent.DirectoryHandle()
	if err != nil {
		return err
	}
	defer anchor.Close()
	if err := unix.Mkdirat(int(anchor.Fd()), name, 0700); err != nil {
		return err
	}
	fd, err := unix.Openat2(int(anchor.Fd()), name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return err
	}
	err = errors.Join(unix.Fchown(fd, int(uid), int(gid)), unix.Fchmod(fd, mode), unix.Fsync(fd), unix.Close(fd))
	return errors.Join(err, syncWALDirectory(parent))
}

func profileFile(root *paths.Root, name string, data []byte, uid, gid uint32, mode uint32, appendOnly bool) (resultErr error) {
	dir, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer dir.Close()
	fd, err := unix.Openat2(int(dir.Fd()), name, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC, Mode: 0600, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "new-profile-file")
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	if err := unix.Fchown(fd, int(uid), int(gid)); err != nil {
		return err
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return err
	}
	n, err := f.Write(data)
	if n != len(data) {
		err = errors.Join(err, io.ErrShortWrite)
	}
	if err != nil {
		return err
	}
	if appendOnly {
		flags, err := unix.IoctlGetInt(fd, unix.FS_IOC_GETFLAGS)
		if err != nil {
			return err
		}
		if err := unix.IoctlSetPointerInt(fd, unix.FS_IOC_SETFLAGS, flags|linuxAppendFlag); err != nil {
			return err
		}
	}
	return errors.Join(f.Sync(), syncWALDirectory(root))
}

type boundedProfileOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedProfileOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		b.exceeded = true
		return 0, io.ErrShortBuffer
	}
	return b.Buffer.Write(p)
}

func runProfileWorkerBytes(ctx context.Context, executable, root, role string, m protectedProfileManifest) ([]byte, error) {
	// Pdeathsig is tied to the creating Linux thread. Keep that thread alive
	// until Wait completes, including while stdout/stderr pumps run elsewhere.
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	cmd := exec.CommandContext(ctx, executable, role, root)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: m.EngineUID, Gid: m.EngineGID, Groups: []uint32{}}, Pdeathsig: syscall.SIGKILL}
	cmd.Env = []string{"HOME=" + filepath.Join(root, "home"), "PATH=/usr/bin:/bin", "GOMAXPROCS=4", "MODERNC_SQLITE_OFD_LOCK=0"}
	out := boundedProfileOutput{limit: 8192}
	stderr := boundedProfileOutput{limit: 1024}
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || out.exceeded || stderr.exceeded {
		return nil, ErrProfileIncomplete
	}
	return append([]byte(nil), out.Bytes()...), ctx.Err()
}

func runProfileWorker(ctx context.Context, executable, root, role string, m protectedProfileManifest) (ProtectedProfileSummary, error) {
	raw, err := runProfileWorkerBytes(ctx, executable, root, role, m)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	var report ProtectedProfileSummary
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&report) != nil || decoder.Decode(&struct{}{}) != io.EOF || report.ProfileID != m.ProfileID || !report.CatalogVerified || !report.TrustedHead {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	if _, err := profileUUIDBytes(report.CatalogIncarnation); err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	return report, ctx.Err()
}

// ProvisionProtectedProfile creates only a fresh root. It never adopts,
// migrates, repairs, chmods or removes an existing profile or target. Partial
// creation is retained as CREATING/incomplete; only a verified bootstrap is
// sealed READY. Invoke through the root-owned tools/state_profile executable.
func ProvisionProtectedProfile(ctx context.Context, opts ProtectedProfileOptions) (ProtectedProfileSummary, error) {
	if err := administratorProfileProcess(ctx); err != nil {
		return ProtectedProfileSummary{}, err
	}
	if !validProfilePath(opts.Root) {
		return ProtectedProfileSummary{}, ErrProfileInput
	}
	executable, exeHash, err := trustedProfileExecutable(ctx)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	if opts.WorkerExecutable != "" {
		executable, exeHash, err = trustedProfileExecutablePath(ctx, opts.WorkerExecutable)
		if err != nil {
			return ProtectedProfileSummary{}, err
		}
	}
	profileID, err := profileUUID()
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	authorityID, err := profileUUID()
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	key := make([]byte, 32)
	defer clear(key)
	if _, err := rand.Read(key); err != nil {
		return ProtectedProfileSummary{}, err
	}
	keyHash := sha256.Sum256(key)
	m := protectedProfileManifest{FormatVersion: 1, ProfileID: profileID, Stage: "CREATING", EngineUID: opts.EngineUID, EngineGID: opts.EngineGID, ClientUID: opts.ClientUID, TargetRoots: sortedProfileTargets(opts.TargetRoots), KeyDigest: hex.EncodeToString(keyHash[:]), CatalogAuthorityID: authorityID, BootstrapExecutableSHA256: exeHash}
	m.WorkerExecutable = opts.WorkerExecutable
	if validateProfileManifest(m) != nil {
		return ProtectedProfileSummary{}, ErrProfileInput
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > profileManifestLimit {
		return ProtectedProfileSummary{}, ErrProfileInput
	}
	parent, err := paths.OpenRoot(filepath.Dir(opts.Root))
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileDenied
	}
	defer parent.Close()
	if rootManagedAncestors(ctx, parent, true) != nil {
		return ProtectedProfileSummary{}, ErrProfileDenied
	}
	var targets []*paths.Root
	defer func() {
		for _, r := range targets {
			_ = r.Close()
		}
	}()
	for _, name := range m.TargetRoots {
		r, err := paths.OpenRoot(name)
		if err != nil {
			return ProtectedProfileSummary{}, ErrProfileDenied
		}
		targets = append(targets, r)
	}
	if runtime.CheckCleanupRoot(parent, targets) != nil {
		return ProtectedProfileSummary{}, ErrProfileDenied
	}
	if err := profileDirectory(parent, filepath.Base(opts.Root), 0, 0, 0755); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return ProtectedProfileSummary{}, ErrProfileExists
		}
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	root, err := parent.OpenDirectory(filepath.Base(opts.Root))
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	defer root.Close()
	for _, item := range []struct {
		name           string
		uid, gid, mode uint32
	}{
		{"home", 0, 0, 0755}, {"home/.local", 0, 0, 0755}, {"home/.local/state", 0, 0, 0755},
		{"home/.local/state/mce4d", m.EngineUID, m.EngineGID, 0700},
		{"home/.local/state/mce4d/.maintenance", m.EngineUID, m.EngineGID, 0700},
		{"home/.local/state/mce4d/.maintenance/catalog", m.EngineUID, m.EngineGID, 0700},
		{"home/.local/state/mce4d/worktrees", m.EngineUID, m.EngineGID, 0700},
		{"control", 0, 0, 0755}, {"control/catalog", 0, 0, 0755},
	} {
		if ctx.Err() != nil {
			return ProtectedProfileSummary{}, ctx.Err()
		}
		if profileDirectory(root, item.name, item.uid, item.gid, item.mode) != nil {
			return ProtectedProfileSummary{}, ErrProfileIncomplete
		}
	}
	control, err := root.OpenDirectory("control")
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	err = profileFile(control, "profile.key", key, m.EngineUID, m.EngineGID, 0600, false)
	err = errors.Join(err, control.Close())
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	cursorKey := make([]byte, 32)
	defer clear(cursorKey)
	if _, err := rand.Read(cursorKey); err != nil {
		return ProtectedProfileSummary{}, err
	}
	state, err := root.OpenDirectory("home/.local/state/mce4d")
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	err = profileFile(state, "cursor.key", cursorKey, m.EngineUID, m.EngineGID, 0600, false)
	err = errors.Join(err, state.Close())
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	for _, item := range []struct {
		dir, name  string
		appendOnly bool
	}{
		{"home/.local/state/mce4d", "catalog.sqlite", false},
		{"home/.local/state/mce4d/.maintenance/catalog", walAuthorityName, false},
		{"control/catalog", walHeadName, true},
	} {
		dir, err := root.OpenDirectory(item.dir)
		if err != nil {
			return ProtectedProfileSummary{}, ErrProfileIncomplete
		}
		err = profileFile(dir, item.name, nil, m.EngineUID, m.EngineGID, 0600, item.appendOnly)
		err = errors.Join(err, dir.Close())
		if err != nil {
			return ProtectedProfileSummary{}, ErrProfileIncomplete
		}
	}
	if profileFile(root, profileManifestName, raw, 0, 0, 0444, false) != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	report, err := runProfileWorker(ctx, executable, opts.Root, "__bootstrap-profile-v1", m)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	// Bootstrap and check are separate processes. Check the created catalog
	// against its persisted head before publishing root-owned READY metadata.
	m.CatalogIncarnation = report.CatalogIncarnation
	if err := verifyProfileBootstrap(ctx, root, m, key); err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	current, _, err := readProfileManifest(ctx, root)
	if err != nil || current.Stage != "CREATING" || current.ProfileID != m.ProfileID {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	m.Stage = "READY"
	raw, err = json.Marshal(m)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	if profileFile(root, "profile.ready.pending", raw, 0, 0, 0444, false) != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	err = unix.Renameat(int(dir.Fd()), "profile.ready.pending", int(dir.Fd()), profileManifestName)
	err = errors.Join(err, dir.Close(), syncWALDirectory(root))
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	return runProfileWorker(ctx, executable, opts.Root, "__check-profile-v1", m)
}

// CheckProtectedProfile verifies in a new unprivileged worker; it performs no
// installation or repair and does not grant a runtime query service.
func CheckProtectedProfile(ctx context.Context, rootPath string) (ProtectedProfileSummary, error) {
	if administratorProfileProcess(ctx) != nil || !validProfilePath(rootPath) {
		return ProtectedProfileSummary{}, ErrProfileDenied
	}
	executable, digest, err := trustedProfileExecutable(ctx)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	root, err := paths.OpenRoot(rootPath)
	if err != nil {
		return ProtectedProfileSummary{}, ErrProfileDenied
	}
	defer root.Close()
	m, _, err := readProfileManifest(ctx, root)
	if err != nil {
		return ProtectedProfileSummary{}, err
	}
	if m.Stage != "READY" {
		return ProtectedProfileSummary{}, ErrProfileIncomplete
	}
	if m.WorkerExecutable != "" {
		executable, digest, err = trustedProfileExecutablePath(ctx, m.WorkerExecutable)
		if err != nil {
			return ProtectedProfileSummary{}, err
		}
	}
	if m.BootstrapExecutableSHA256 != digest {
		return ProtectedProfileSummary{}, ErrProfileDenied
	}
	return runProfileWorker(ctx, executable, rootPath, "__check-profile-v1", m)
}
