package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/windows"
)

// WindowsCacheReservation serializes derived cache writers in a worktree.
// It charges live index images and all scratch left on disk, including pending
// files. Records and administrator-owned backups remain outside this cache.
type WindowsCacheReservation struct {
	slot     windowsMaintenanceFiles
	scratch  *paths.Root
	hold     *runtime.LifecycleHold
	gate     *runtime.Lifecycle
	check    func(context.Context) error
	limit    int64
	toolPath string
	toolACL  windowsControlACL
}

func (p *WindowsScopedPools) ReserveRenameCache(ctx context.Context, limit, growth int64) (*WindowsCacheReservation, error) {
	if p == nil {
		return nil, ErrProfileDenied
	}
	return reserveWindowsCache(ctx, p.worktree, p.worktreeLease, p.preparation.LspSID, p.Check, limit, growth)
}

func reserveWindowsCache(ctx context.Context, slot windowsMaintenanceFiles, lease *runtime.Lifecycle, role string,
	check func(context.Context) error, limit, growth int64) (_ *WindowsCacheReservation, resultErr error) {
	if limit < 1 || growth < 0 || growth > limit {
		return nil, ErrStateLimit
	}
	if err := errors.Join(check(ctx), lease.CheckRoot(ctx, slot.control, slot.scope, ports.SharedLease)); err != nil {
		return nil, err
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	r := &WindowsCacheReservation{slot: slot, hold: hold, check: check, limit: limit}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, r.Close())
		}
	}()
	if role != "" {
		sid, err := windows.StringToSid(role)
		if err != nil {
			return nil, err
		}
		appRoot, err := runtime.WindowsAppContainerFolder(sid)
		if err != nil {
			return nil, err
		}
		// The Go process uses this prefix for every descendant tool and
		// cache file. Keep it short within the AppContainer package path.
		key := sha256.Sum256([]byte(slot.profile + "/" + slot.scope))
		r.toolPath = filepath.Join(appRoot, "mce4d-"+base64.RawURLEncoding.EncodeToString(key[:12]))
		r.toolACL = windowsControlACL{owner: slot.acl.owner, reader: sid}
	}
	scratch, err := openWindowsRenameDirectory(ctx, slot, "scratch")
	if err != nil {
		return nil, err
	}
	r.scratch = scratch.control
	r.gate, err = runtime.AcquireLifecycle(ctx, r.scratch, slot.scope, ports.ExclusiveLease, 2*time.Second)
	if err != nil {
		return nil, err
	}
	if err := r.observe(ctx, growth); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *WindowsCacheReservation) Check(ctx context.Context) error {
	return r.observe(ctx, 0)
}

func (r *WindowsCacheReservation) observe(ctx context.Context, growth int64) error {
	if r == nil || r.gate == nil {
		return ErrOwnerClosed
	}
	if err := errors.Join(r.check(ctx), r.gate.CheckRoot(ctx, r.scratch, r.slot.scope, ports.ExclusiveLease)); err != nil {
		return err
	}
	remaining, entries := r.limit-growth, 8192
	for _, name := range []string{"scratch", "index-wal", "index-control"} {
		dir, err := r.slot.control.OpenDirectory(name)
		if err != nil {
			return err
		}
		err = errors.Join(measureWindowsCache(ctx, dir, 0, &remaining, &entries), dir.Close())
		if err != nil {
			return err
		}
	}
	if r.toolPath == "" {
		return errors.Join(r.slot.control.Verify(), ctx.Err())
	}
	tool, err := paths.OpenRoot(r.toolPath)
	if err == nil {
		files := r.slot
		files.control = tool
		files.acl = r.toolACL
		dir, err := files.directory(ctx)
		if err != nil {
			return errors.Join(err, tool.Close())
		}
		err = errors.Join(dir.Close(), measureWindowsCache(ctx, tool, 0, &remaining, &entries), tool.Close())
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return errors.Join(r.slot.control.Verify(), ctx.Err())
}

func measureWindowsCache(ctx context.Context, dir *paths.Root, depth int, remaining *int64, entries *int) (resultErr error) {
	if depth > 32 || *remaining < 0 {
		return ErrStateLimit
	}
	handle, err := dir.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.Close()) }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, readErr := handle.ReadDir(128)
		for _, entry := range batch {
			*entries--
			if *entries < 0 {
				return ErrStateLimit
			}
			if entry.IsDir() {
				child, err := dir.OpenDirectory(entry.Name())
				if err != nil {
					return err
				}
				if err := errors.Join(measureWindowsCache(ctx, child, depth+1, remaining, entries), child.Close()); err != nil {
					return err
				}
				continue
			}
			// Directory records provide size without opening SQLite or the
			// pending result, whose writer may hold an exclusive NTFS share.
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() < 0 {
				return errors.Join(ErrStateUnsafe, err)
			}
			if info.Size() > *remaining {
				return ErrStateLimit
			}
			*remaining -= info.Size()
		}
		if errors.Is(readErr, io.EOF) {
			return dir.Verify()
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (r *WindowsCacheReservation) Close() error {
	var err error
	if r.gate != nil {
		err = r.gate.Close()
		r.gate = nil
	}
	if r.scratch != nil {
		err = errors.Join(err, r.scratch.Close())
		r.scratch = nil
	}
	if r.hold != nil {
		err = errors.Join(err, r.hold.Close())
		r.hold = nil
	}
	return err
}
