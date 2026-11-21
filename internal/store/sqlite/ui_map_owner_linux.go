//go:build linux

package sqlite

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/uimap"
	"golang.org/x/sys/unix"
)

func NativeUIMapsEnabled() bool { return nativeUIMaps }

func (o *ProtectedState) ReadUIBuild(ctx context.Context, path, worktree string) (uimap.ImportedBuild, error) {
	return o.readUIBuild(ctx, path, worktree, 16<<20)
}

func (o *ProtectedState) readUIBuild(ctx context.Context, path, worktree string, maxBytes int64) (out uimap.ImportedBuild, resultErr error) {
	if !nativeUIMaps || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".json" || len(path) > 4096 {
		return out, uimap.ErrBuildArtifact
	}
	root, err := o.openUIBuildDirectory(ctx, filepath.Dir(path))
	if err != nil {
		return out, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	out, err = uimap.ReadSelectedArtifact(ctx, root, filepath.Base(path), worktree, int(min(maxBytes, 16<<20)), func(file *os.File) error {
		return stateOwner(file, false)
	})
	return out, errors.Join(err, o.Check(ctx))
}

func (o *ProtectedState) ReadWatchedUIBuild(ctx context.Context, directory, worktree, buildID string, maxBytes int64) (uimap.ImportedBuild, error) {
	if !model.ValidDigest(buildID) || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || maxBytes < 1 {
		return uimap.ImportedBuild{}, uimap.ErrBuildArtifact
	}
	return o.readUIBuild(ctx, filepath.Join(directory, buildID+".json"), worktree, maxBytes)
}

func (o *ProtectedState) openUIBuildDirectory(ctx context.Context, directory string) (*paths.Root, error) {
	if !nativeUIMaps || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || len(directory) > 4096 {
		return nil, uimap.ErrBuildArtifact
	}
	if err := o.Check(ctx); err != nil {
		return nil, err
	}
	inside := func(parent, child string) bool {
		relative, err := filepath.Rel(parent, child)
		return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	for _, root := range o.TargetRoots() {
		target, err := root.CanonicalPath()
		if err != nil || inside(target, directory) || inside(directory, target) {
			return nil, ErrProfileDenied
		}
	}
	root, err := paths.OpenRoot(directory)
	if err != nil {
		return nil, uimap.ErrBuildArtifact
	}
	if err := stateRootOwner(root); err != nil {
		return nil, errors.Join(ErrProfileDenied, root.Close())
	}
	return root, nil
}

type WatchedUIBuild struct {
	ID       string
	Modified time.Time
}

// NewestUIBuild examines committed content-addressed names under the approved,
// currently owned directory. Selected bytes are opened separately and verified
// against the exact identifier by the importer.
func (o *ProtectedState) NewestUIBuild(ctx context.Context, directory string) (latest WatchedUIBuild, resultErr error) {
	root, err := o.openUIBuildDirectory(ctx, directory)
	if err != nil {
		return latest, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return latest, err
	}
	fd, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	closeErr := anchor.Close()
	if err != nil || closeErr != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return latest, errors.Join(err, closeErr)
	}
	listing := os.NewFile(uintptr(fd), "ui-map-inbox")
	defer func() { resultErr = errors.Join(resultErr, listing.Close()) }()
	seen := 0
	for {
		entries, readErr := listing.ReadDir(128)
		seen += len(entries)
		if seen > 8192 {
			return WatchedUIBuild{}, uimap.ErrBuildArtifact
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return WatchedUIBuild{}, readErr
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return WatchedUIBuild{}, err
			}
			id, committed := strings.CutSuffix(entry.Name(), ".json")
			if !committed || !model.ValidDigest(id) || !entry.Type().IsRegular() {
				continue
			}
			var metadata unix.Stat_t
			if err := unix.Fstatat(fd, entry.Name(), &metadata, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
				continue
			} else if err != nil {
				return WatchedUIBuild{}, err
			}
			if metadata.Mode&unix.S_IFMT != unix.S_IFREG || metadata.Nlink != 1 ||
				metadata.Uid != uint32(os.Geteuid()) || metadata.Mode&077 != 0 {
				continue
			}
			modified := time.Unix(metadata.Ctim.Sec, metadata.Ctim.Nsec)
			if latest.ID == "" || modified.After(latest.Modified) || modified.Equal(latest.Modified) && id > latest.ID {
				latest = WatchedUIBuild{ID: id, Modified: modified}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if err := errors.Join(root.Verify(), o.Check(ctx)); err != nil {
		return WatchedUIBuild{}, err
	}
	return latest, nil
}

// UpdateUIIndex borrows a protected writer pool for the selected worktree.
// Import and bind each check the outer writer and commit their own WAL receipt.
func (o *ProtectedWorktreeOwner) UpdateUIIndex(ctx context.Context, update func(*index.UIStore) error) (resultErr error) {
	if !nativeUIMaps || update == nil {
		return ErrStateUnsupported
	}
	if err := o.check(ctx); err != nil {
		return err
	}
	if len(o.stores) != 2 || o.stores[0].name != "index.sqlite" {
		return ErrStateUnsafe
	}
	pool := o.stores[0].pool(false, 1)
	defer func() {
		if err := pool.Close(); err != nil {
			resultErr = errors.Join(resultErr, index.ErrAfterCommit, err)
		}
	}()
	store, err := index.OpenUIAddon(ctx, pool, o.manifest.WorktreeID, o.lease)
	if err != nil {
		return err
	}
	return update(store)
}
