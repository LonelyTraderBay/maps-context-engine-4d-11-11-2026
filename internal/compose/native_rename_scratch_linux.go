//go:build linux

package compose

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/sys/unix"
)

func privateNativeDirectory(root *paths.Root) error {
	if root == nil || root.Verify() != nil {
		return app.ErrValidationAuthority
	}
	fd, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer fd.Close()
	var st unix.Stat_t
	if unix.Fstat(int(fd.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return app.ErrValidationAuthority
	}
	return nil
}

// A missing fixed disposable directory in an already-provisioned worktree may
// be created under its real protected root/lease. Existing paths are verified,
// never chmodded, adopted or used to migrate a database/control manifest.
func ensureNativeRenameDirectory(ctx context.Context, n *nativeScope, root *paths.Root, protected []*paths.Root) (resultErr error) {
	if err := errors.Join(n.stores.Check(ctx), privateNativeDirectory(root), runtime.CheckCleanupRoot(root, protected)); err != nil {
		return err
	}
	parent, err := root.OpenDirectory("scratch")
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := privateNativeDirectory(parent); err != nil {
		return err
	}
	dir, err := parent.OpenDirectory("rename")
	if errors.Is(err, os.ErrNotExist) {
		reservation, e := n.stores.ReserveCache(ctx, 4096)
		if e != nil {
			return errors.Join(app.ErrCoreLimit, e)
		}
		defer func() { resultErr = errors.Join(resultErr, reservation.Close()) }()
		fd, e := parent.DirectoryHandle()
		if e != nil {
			return e
		}
		e = unix.Mkdirat(int(fd.Fd()), "rename", 0700)
		e = errors.Join(e, fd.Close())
		if e != nil && !errors.Is(e, unix.EEXIST) {
			return e
		}
		dir, err = parent.OpenDirectory("rename")
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	return errors.Join(privateNativeDirectory(dir), n.stores.Check(ctx))
}

type nativeRenameScratch struct {
	n                    *nativeScope
	root, parent, goRoot *paths.Root
	protected            []*paths.Root
	path, name           string
	captured             *snapshot.Snapshot
	reservation          packstore.BudgetReservation
	closed               bool
}

func newNativeRenameScratch(ctx context.Context, n *nativeScope, captured *snapshot.Snapshot, goRootPath string, additionalProtected ...*paths.Root) (_ *nativeRenameScratch, resultErr error) {
	root, _, protected, err := n.stores.CacheResources(ctx)
	if err != nil {
		return nil, err
	}
	if err := runtime.CheckCleanupRoot(root, protected); err != nil {
		return nil, err
	}
	parent, err := root.OpenDirectory("scratch")
	if err != nil {
		return nil, err
	}
	s := &nativeRenameScratch{n: n, parent: parent, captured: captured}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, s.Close(ctx))
		}
	}()
	if err := privateNativeDirectory(parent); err != nil {
		return nil, err
	}
	// This is a cooperative reservation against the existing worktree budget,
	// not an OS filesystem quota. Trusted tools have finite load/LSP deadlines;
	// actual usage is rechecked before/after them and before result publication.
	s.reservation, err = n.stores.ReserveCache(ctx, minInt64(256<<20, n.effective.Core.MaxWorktreeCacheBytes/2))
	if err != nil {
		return nil, errors.Join(app.ErrCoreLimit, err)
	}
	operation, err := nativeOperationID()
	if err != nil {
		return nil, err
	}
	s.name = "lsp-" + operation
	fd, err := parent.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	err = errors.Join(unix.Mkdirat(int(fd.Fd()), s.name, 0700), fd.Close())
	if err != nil {
		return nil, err
	}
	s.root, err = parent.OpenDirectory(s.name)
	if err != nil {
		return nil, err
	}
	s.path, err = s.root.CanonicalPath()
	if err != nil {
		return nil, err
	}
	s.goRoot, err = paths.OpenRoot(goRootPath)
	if err != nil {
		return nil, err
	}
	s.protected = append(append(append([]*paths.Root{}, protected...), s.goRoot), additionalProtected...)
	if err := runtime.CheckCleanupRoot(s.root, s.protected); err != nil {
		return nil, err
	}
	anchor, err := s.root.DirectoryHandle()
	if err != nil {
		return nil, err
	}
	defer anchor.Close()
	for _, name := range []string{"input", "home", "cache", "modcache", "gopath", "tmp"} {
		if err := unix.Mkdirat(int(anchor.Fd()), name, 0700); err != nil {
			return nil, err
		}
	}
	for _, item := range captured.Manifest().Files {
		file, ok := captured.File(item.Path)
		if !ok || item.Status != "captured" || file.Len() > 64<<10 {
			return nil, app.ErrCoreUnsupported
		}
		name := filepath.Join(s.path, "input", filepath.FromSlash(item.Path))
		// All ancestors are this request's private, freshly created scratch.
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return nil, err
		}
		out, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, writeErr := out.Write(file.Bytes())
		if err := errors.Join(writeErr, out.Close()); err != nil {
			return nil, err
		}
	}
	if err := s.Check(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *nativeRenameScratch) Check(ctx context.Context) error {
	if s == nil || s.closed || s.root == nil || s.reservation == nil {
		return app.ErrValidationAuthority
	}
	if err := errors.Join(s.n.stores.Check(ctx), s.reservation.Check(ctx), privateNativeDirectory(s.root), runtime.CheckCleanupRoot(s.root, s.protected)); err != nil {
		return err
	}
	input, err := s.root.OpenDirectory("input")
	if err != nil {
		return err
	}
	defer input.Close()
	for _, item := range s.captured.Manifest().Files {
		file, ok := s.captured.File(item.Path)
		if !ok {
			return app.ErrValidationPrecondition
		}
		current, err := input.OpenRegular(item.Path)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(current, int64(file.Len())+1))
		closeErr := current.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(data, file.Bytes()) {
			return app.ErrValidationPrecondition
		}
	}
	return ctx.Err()
}

// Close is called only after packages.Load has returned and managed LSP
// shutdown completed. Canceled/uncertain loads retain their scratch, which
// remains charged to the worktree; cleanup never guesses stopped writers.
func (s *nativeRenameScratch) Close(ctx context.Context) error {
	if s == nil || s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.root != nil && ctx.Err() == nil && len(s.protected) > 0 {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if check := errors.Join(s.n.stores.Check(cleanup), s.root.Verify(), runtime.CheckCleanupRoot(s.root, s.protected)); check != nil {
			err = check
		} else {
			err = removeNativeRenameScratch(cleanup, s)
		}
		cancel()
	}
	if s.root != nil {
		err = errors.Join(err, s.root.Close())
	}
	if s.parent != nil {
		err = errors.Join(err, s.parent.Close())
	}
	if s.goRoot != nil {
		err = errors.Join(err, s.goRoot.Close())
	}
	if s.reservation != nil {
		err = errors.Join(err, s.reservation.Close())
	}
	return err
}

func removeNativeRenameScratch(ctx context.Context, s *nativeRenameScratch) error {
	if !strings.HasPrefix(s.name, "lsp-") || len(s.name) != 40 {
		return app.ErrValidationAuthority
	}
	return removeNativeJobScratch(ctx, s.root, s.parent, s.name, func() error {
		return errors.Join(s.n.stores.Check(ctx), s.root.Verify(), runtime.CheckCleanupRoot(s.root, s.protected))
	})
}

func removeNativeJobScratch(ctx context.Context, root, parent *paths.Root, name string, check func() error) error {
	if err := check(); err != nil {
		return err
	}
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer anchor.Close()
	count := 0
	var walk func(int, int) error
	walk = func(fd, depth int) error {
		if depth > 64 {
			return app.ErrCoreLimit
		}
		listFD, e := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return e
		}
		listing := os.NewFile(uintptr(listFD), "owned-job-scratch")
		defer listing.Close()
		for {
			entries, e := listing.ReadDir(128)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				count++
				if count > 50000 {
					return app.ErrCoreLimit
				}
				var st unix.Stat_t
				if unix.Fstatat(fd, entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0022 != 0 {
					return app.ErrValidationAuthority
				}
				kind := st.Mode & unix.S_IFMT
				if kind == unix.S_IFDIR {
					child, e := unix.Openat2(fd, entry.Name(), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
					if e != nil {
						return e
					}
					e = errors.Join(walk(child, depth+1), unix.Close(child))
					if e != nil {
						return e
					}
					if e := unix.Unlinkat(fd, entry.Name(), unix.AT_REMOVEDIR); e != nil {
						return e
					}
				} else if kind == unix.S_IFREG && st.Nlink == 1 {
					if e := unix.Unlinkat(fd, entry.Name(), 0); e != nil {
						return e
					}
				} else {
					return app.ErrValidationAuthority
				}
			}
			if errors.Is(e, io.EOF) {
				break
			}
			if e != nil {
				return e
			}
		}
		return nil
	}
	if err := walk(int(anchor.Fd()), 0); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	dir, err := parent.DirectoryHandle()
	if err != nil {
		return err
	}
	defer dir.Close()
	return unix.Unlinkat(int(dir.Fd()), name, unix.AT_REMOVEDIR)
}
