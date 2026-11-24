package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/sys/windows"
)

func (p *WindowsScopedPools) HasRenameContainer() bool { return p.preparation.LspSID != "" }

// PinRenameProvider stages the authenticated catalog's approved bytes under
// an AppContainer read/execute grant. The original path is never executed.
func (p *WindowsScopedPools) PinRenameProvider(ctx context.Context, provider model.AllowedExecutable, limit int64) (pinned *paths.Root, release func() error, resultErr error) {
	if p == nil || filepath.Base(provider.Path) != "gopls.exe" || !model.ValidDigest(provider.SHA256) ||
		!slices.Contains(p.binding.Policy.AllowedExecutables, provider) {
		return nil, nil, ErrProfileDenied
	}
	if err := p.Check(ctx); err != nil {
		return nil, nil, err
	}
	image, err := readWindowsToolImage(ctx, provider.Path)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(image)
	if hex.EncodeToString(digest[:]) != provider.SHA256 {
		return nil, nil, ErrProfileDenied
	}
	cache, err := p.ReserveRenameCache(ctx, limit, int64(len(image)))
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, cache.Close())
		if resultErr != nil && release != nil {
			resultErr = errors.Join(resultErr, release())
			pinned, release = nil, nil
		}
	}()
	parent, err := openWindowsToolCache(ctx, p.config.control, p.preparation.TargetRoots, p.worktree, cache)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.control.Close()) }()
	staged, drop, err := parent.pinWindowsRoleImage(ctx, p.preparation.LspSID, "gopls", "gopls.exe", image, digest)
	if err != nil {
		return nil, nil, err
	}
	root, err := paths.OpenRoot(filepath.Dir(staged))
	if err != nil {
		return nil, nil, errors.Join(err, drop())
	}
	close := func() error { return errors.Join(root.Close(), drop()) }
	if err := cache.Check(ctx); err != nil {
		return nil, nil, errors.Join(err, close())
	}
	return root, close, nil
}

type WindowsRenameScratch struct {
	Root        *paths.Root
	Reader      *windows.SID
	Path, Input string
	cache       *WindowsCacheReservation
	parent      *paths.Root
	name        string
}

// OpenRenameScratch borrows the cache growth gate until the typed loader and
// LSP session settle. Only the configured LSP role may read the capture or
// write the five disposable tool-output directories.
func (p *WindowsScopedPools) OpenRenameScratch(ctx context.Context, captured *snapshot.Snapshot, limit int64) (_ *WindowsRenameScratch, resultErr error) {
	if p == nil {
		return nil, ErrProfileDenied
	}
	return openWindowsGoScratch(ctx, captured, limit, p.binding, p.config.control, p.preparation, p.worktree, p.worktreeLease, p.Check)
}

func openWindowsGoScratch(ctx context.Context, captured *snapshot.Snapshot, limit int64, binding catalog.Binding,
	config *paths.Root, preparation windowsProfilePreparation, worktree windowsMaintenanceFiles,
	lease *runtime.Lifecycle, check func(context.Context) error) (_ *WindowsRenameScratch, resultErr error) {
	if captured == nil {
		return nil, ErrProfileDenied
	}
	m := captured.Manifest()
	if m.WorktreeID != binding.WorktreeID || m.ProfileID != binding.ProfileID || m.PolicyDigest != binding.PolicyDigest ||
		len(m.Files) == 0 || len(m.Files) > 128 || model.VerifyCaptureDigest(m, 1<<20) != nil {
		return nil, ErrProfileDenied
	}
	sid, err := windows.StringToSid(preparation.LspSID)
	if err != nil || sid.SubAuthorityCount() != 8 {
		return nil, errors.Join(ErrProfileDenied, err)
	}
	cache, err := reserveWindowsCache(ctx, worktree, lease, preparation.LspSID, check, limit, min(int64(256<<20), max(1, limit/2)))
	if err != nil {
		return nil, err
	}
	scratch := &WindowsRenameScratch{cache: cache, Reader: sid}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, scratch.Close(ctx))
		}
	}()
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	parent, err := openWindowsToolCache(ctx, config, preparation.TargetRoots, worktree, cache)
	if err != nil {
		return nil, err
	}
	scratch.parent = parent.control
	readACL := windowsControlACL{owner: parent.acl.owner, reader: sid}
	name := "lsp-" + base64.RawURLEncoding.EncodeToString(id[:])
	if err := createWindowsProfileDirectoryWithACL(ctx, parent.control, parent.acl, readACL, name); err != nil {
		return nil, err
	}
	scratch.name = name
	scratch.Root, err = parent.control.OpenDirectory(name)
	if err != nil {
		return nil, err
	}
	scratch.Path, err = scratch.Root.CanonicalPath()
	if err != nil {
		return nil, err
	}
	scratch.Input = filepath.Join(scratch.Path, "input")
	if err := createWindowsProfileDirectory(ctx, scratch.Root, readACL, "input"); err != nil {
		return nil, err
	}
	writeACL := windowsControlACL{owner: parent.acl.owner, reader: sid, toolOutput: true}
	for _, leaf := range []string{"home", "cache", "modcache", "gopath", "tmp"} {
		if err := createWindowsProfileDirectoryWithACL(ctx, scratch.Root, readACL, writeACL, leaf); err != nil {
			return nil, err
		}
	}
	input, err := scratch.Root.OpenDirectory("input")
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, input.Close()) }()
	for _, item := range m.Files {
		file, ok := captured.File(item.Path)
		if !ok || item.Status != "captured" || file.Len() > 64<<10 {
			return nil, ErrProfileDenied
		}
		if err := writeWindowsScratchFile(ctx, input, readACL, strings.Split(item.Path, "/"), file.Bytes()); err != nil {
			return nil, err
		}
	}
	if err := scratch.Check(ctx); err != nil {
		return nil, err
	}
	return scratch, nil
}

func writeWindowsScratchFile(ctx context.Context, root *paths.Root, acl windowsControlACL, parts []string, contents []byte) (resultErr error) {
	if len(parts) < 1 || len(parts) > 64 || ctx.Err() != nil {
		return errors.Join(ErrProfileDenied, ctx.Err())
	}
	for _, leaf := range parts[:len(parts)-1] {
		if err := createWindowsProfileDirectory(ctx, root, acl, leaf); err != nil && !errors.Is(err, ErrProfileExists) {
			return err
		}
		child, err := root.OpenDirectory(leaf)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, child.Close()) }()
		root = child
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	if err := acl.check(dir); err != nil {
		return err
	}
	sd, err := acl.descriptor()
	if err != nil {
		return err
	}
	file, err := paths.OpenWindowsEntry(dir, parts[len(parts)-1], sd, false, windows.FILE_CREATE, 0)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := errors.Join(acl.check(file), paths.VerifyRegularFile(file), ctx.Err()); err != nil {
		return err
	}
	n, err := file.Write(contents)
	if err == nil && n != len(contents) {
		err = io.ErrShortWrite
	}
	return errors.Join(err, root.Verify())
}

func (s *WindowsRenameScratch) Check(ctx context.Context) error {
	if s.cache == nil || s.Root == nil {
		return ErrOwnerClosed
	}
	return errors.Join(s.cache.Check(ctx), s.Root.Verify())
}

func (s *WindowsRenameScratch) Close(parent context.Context) error {
	if s.cache == nil {
		return nil
	}
	var err error
	if s.Root != nil {
		err = s.Root.Close()
		s.Root = nil
	}
	if err == nil && s.name != "" && parent.Err() == nil {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		// Cancellation or an unsafe residual leaves the owned scratch charged.
		err = paths.RemoveWindowsOwnedTree(ctx, s.parent, s.name)
	}
	if s.parent != nil {
		err = errors.Join(err, s.parent.Close())
		s.parent = nil
	}
	err = errors.Join(err, s.cache.Close())
	s.cache = nil
	return err
}

func openWindowsToolCache(ctx context.Context, config *paths.Root, targets []string, slot windowsMaintenanceFiles, reservation *WindowsCacheReservation) (windowsMaintenanceFiles, error) {
	name := reservation.toolPath
	state, err := config.CanonicalPath()
	if err != nil {
		return windowsMaintenanceFiles{}, err
	}
	for _, approved := range append(append([]string{}, targets...), state) {
		if windowsPathContains(name, approved) || windowsPathContains(approved, name) {
			return windowsMaintenanceFiles{}, ErrProfileDenied
		}
	}
	folder, err := paths.OpenRoot(filepath.Dir(name))
	if err != nil {
		return windowsMaintenanceFiles{}, err
	}
	defer folder.Close()
	child, err := folder.OpenDirectory(filepath.Base(name))
	if errors.Is(err, os.ErrNotExist) {
		if err := createWindowsDirectory(ctx, folder, func(*os.File) error { return folder.Verify() }, reservation.toolACL, filepath.Base(name)); err != nil && !errors.Is(err, ErrProfileExists) {
			return windowsMaintenanceFiles{}, err
		}
		child, err = folder.OpenDirectory(filepath.Base(name))
	}
	if err != nil {
		return windowsMaintenanceFiles{}, err
	}
	slot.control = child
	slot.acl = reservation.toolACL
	dir, err := slot.directory(ctx)
	if err == nil {
		err = dir.Close()
	}
	if err != nil {
		return windowsMaintenanceFiles{}, errors.Join(err, child.Close())
	}
	return slot, nil
}
