//go:build linux

package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// Pre-SQL input only. Owning this value grants neither an exclusive lifecycle
// nor permission to replace anything. No source database or key is opened.
type protectedRestoreInput struct {
	root           *paths.Root
	profile        protectedProfileManifest
	manifest       StoreBackupManifest
	manifestSHA256 string
	directory      string
}

// The administrator approves a backup from the same installed profile and
// explicit scope. Every declared byte is hashed before a worker can be started.
// Artifact validation through SQLite is a subsequent unprivileged boundary.
func openProtectedRestoreInput(ctx context.Context, root, target, worktree, input string) (result *protectedRestoreInput, resultErr error) {
	if administratorProfileProcess(ctx) != nil || !validProfilePath(root) || !filepath.IsAbs(input) || filepath.Clean(input) != input || filepath.Base(input) != "backup.json" {
		return nil, ErrProfileInput
	}
	base := profileStatePath(root)
	if target == "worktree" {
		if !model.ValidUUID(worktree) {
			return nil, ErrProfileInput
		}
		base = worktreeStatePath(root, worktree)
	} else if target != "catalog" || worktree != "" {
		return nil, ErrProfileInput
	}
	directory := filepath.Dir(input)
	leaf := filepath.Base(directory)
	if filepath.Dir(directory) != filepath.Join(base, "backups") || len(leaf) > 128 || model.ValidateRelativePath(leaf, false) != nil {
		return nil, ErrProfileInput
	}
	profileRoot, err := paths.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, profileRoot.Close())
		if resultErr != nil && result != nil {
			resultErr = errors.Join(resultErr, result.root.Close())
			result = nil
		}
	}()
	m, _, err := readProfileManifest(ctx, profileRoot)
	if err != nil || m.Stage != "READY" {
		return nil, errors.Join(ErrProfileDenied, err)
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return nil, err
	}
	backup, err := profileRoot.OpenDirectory(filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	out := &protectedRestoreInput{root: backup, profile: m, directory: directory}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, backup.Close())
		}
	}()
	anchor, err := backup.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(anchor.Fd()), &st); err != nil || st.Uid != m.EngineUID || st.Gid != m.EngineGID || st.Mode&0077 != 0 {
		return nil, errors.Join(ErrProfileDenied, err, anchor.Close())
	}
	fd, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if closeErr := anchor.Close(); err != nil || closeErr != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return nil, errors.Join(err, closeErr)
	}
	dir := os.NewFile(uintptr(fd), "approved-backup-directory")
	entries, readErr := dir.ReadDir(5)
	closeErr := dir.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil || len(entries) < 2 || len(entries) > 3 {
		return nil, errors.Join(ErrProfileInput, readErr, closeErr)
	}
	raw, hash, err := readRestoreInputFile(ctx, backup, "backup.json", int64(backupManifestMaxBytes), 0, m.EngineUID, m.EngineGID, true)
	if err != nil {
		return nil, err
	}
	manifest, err := DecodeStoreBackupManifest(raw)
	if err != nil || manifest.ProfileInstallationID != m.ProfileID || manifest.Target != target || manifest.WorktreeID != worktree || len(entries) != len(manifest.Stores)+1 {
		return nil, errors.Join(ErrProfileInput, err)
	}
	allowed := map[string]bool{"backup.json": true}
	for _, store := range manifest.Stores {
		allowed[store.FileName] = true
		_, digest, err := readRestoreInputFile(ctx, backup, store.FileName, store.ByteLength, store.ByteLength, m.EngineUID, m.EngineGID, false)
		if err != nil || digest != store.RawSHA256 {
			return nil, errors.Join(ErrStateChanged, err)
		}
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !allowed[entry.Name()] {
			return nil, ErrProfileInput
		}
	}
	// Detect a replaced manifest or directory after potentially long hashing.
	after, afterHash, err := readRestoreInputFile(ctx, backup, "backup.json", int64(backupManifestMaxBytes), 0, m.EngineUID, m.EngineGID, true)
	if err != nil || afterHash != hash || len(after) != len(raw) || backup.Verify() != nil {
		return nil, errors.Join(ErrStateChanged, err)
	}
	out.manifest, out.manifestSHA256 = manifest, hash
	return out, nil
}

// exact=0 is a bounded metadata read; database streams retain no source bytes.
func readRestoreInputFile(ctx context.Context, root *paths.Root, name string, limit, exact int64, uid, gid uint32, retain bool) (raw []byte, digest string, resultErr error) {
	if ctx == nil || limit < 1 || limit > 4<<30 || exact < 0 || exact > limit {
		return nil, "", ErrProfileInput
	}
	f, err := root.OpenRegular(name)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		resultErr = errors.Join(resultErr, f.Close())
		if resultErr != nil {
			raw = nil
			digest = ""
		}
	}()
	var before unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &before); err != nil || before.Uid != uid || before.Gid != gid || before.Mode&0077 != 0 || before.Nlink != 1 || before.Size < 1 || before.Size > limit || exact != 0 && before.Size != exact {
		return nil, "", errors.Join(ErrProfileDenied, err)
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		n, err := f.Read(buffer)
		total += int64(n)
		if total > limit {
			return nil, "", ErrStateLimit
		}
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
			if retain {
				raw = append(raw, buffer[:n]...)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
	}
	var after unix.Stat_t
	statErr := unix.Fstat(int(f.Fd()), &after)
	after.Atim = before.Atim // Reading may legitimately update access time.
	if statErr != nil || before != after || total != before.Size {
		return nil, "", errors.Join(ErrStateChanged, statErr)
	}
	if err := errors.Join(ctx.Err(), root.Verify(), paths.VerifyRegularFile(f)); err != nil {
		return nil, "", err
	}
	return raw, hex.EncodeToString(hash.Sum(nil)), nil
}
