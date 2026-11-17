//go:build linux

package sqlite

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func stateOwner(file *os.File, directory bool) error {
	var st unix.Stat_t
	if file == nil || unix.Fstat(int(file.Fd()), &st) != nil {
		return ErrStateUnsafe
	}
	typeMode := uint32(unix.S_IFREG)
	if directory {
		typeMode = unix.S_IFDIR
	}
	if st.Mode&unix.S_IFMT != typeMode || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || !directory && st.Nlink != 1 {
		return ErrStateUnsafe
	}
	return nil
}

func stateRootOwner(root *paths.Root) error {
	if root == nil || root.Verify() != nil {
		return ErrStateUnsafe
	}
	file, err := root.DirectoryHandle()
	if err != nil {
		return ErrStateUnsafe
	}
	if errors.Join(stateOwner(file, true), file.Close(), root.Verify()) != nil {
		return ErrStateUnsafe
	}
	return nil
}

// Open only an identity handle. On Linux, closing O_PATH does not remove this
// process's POSIX locks on the inode, unlike closing an ordinary read fd.
// This still observes a path at a point in time; it does not reserve it.
func openStateIdentity(root *paths.Root, name string) (file *os.File, resultErr error) {
	if root == nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || root.Verify() != nil {
		return nil, ErrStateUnsafe
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return nil, errors.Join(ErrStateUnsafe, err)
	}
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			if file != nil {
				closeErr = errors.Join(closeErr, file.Close())
				file = nil
			}
			// Do not retain ENOENT here: callers treat it as an absent sidecar.
			resultErr = errors.Join(ErrStateUnsafe, closeErr)
		}
	}()
	how := &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV}
	fd, err := unix.Openat2(int(dir.Fd()), name, how)
	if err != nil {
		return nil, err
	}
	file = os.NewFile(uintptr(fd), "sqlite-state-identity")
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrStateUnsafe
	}
	// Match the previous readable-file requirement without opening a second
	// ordinary descriptor; unsupported kernels fail closed.
	if stateOwner(file, false) != nil || unix.Faccessat2(fd, "", unix.R_OK, unix.AT_EMPTY_PATH|unix.AT_EACCESS) != nil || root.Verify() != nil {
		closeErr := file.Close()
		return nil, errors.Join(ErrStateUnsafe, closeErr)
	}
	return file, nil
}

func (o *StateObservation) checkBase(ctx context.Context) error {
	if o == nil || ctx == nil || o.closed || o.root == nil || o.lease == nil || o.hold == nil || o.authorize == nil {
		return ErrStateUnsafe
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrStateUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.authorize(ctx) != nil || o.lease.CheckRoot(ctx, o.root, o.scope, ports.SharedLease) != nil || stateRootOwner(o.root) != nil || runtime.CheckCleanupRoot(o.root, o.protected) != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrStateUnsafe
	}
	if o.authorize(ctx) != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrStateUnsafe
	}
	return ctx.Err()
}

// ObserveStateFiles requires an owner-authorized state root, the complete
// protected-root set and the matching held lifecycle lease. It never creates or
// writes a database or sidecar. A callback must not reenter this observation.
func ObserveStateFiles(ctx context.Context, root *paths.Root, protected []*paths.Root, lease *runtime.Lifecycle, scope, name string, maxBytes int64, authorize func(context.Context) error) (out *StateObservation, resultErr error) {
	if ctx == nil || root == nil || lease == nil || authorize == nil || len(protected) < 1 || len(protected) > 64 || maxBytes < 1 || maxBytes > 1<<40 || scope == "catalog" && name != "catalog.sqlite" || scope != "catalog" && name != "index.sqlite" && name != "records.sqlite" {
		return nil, ErrStateUnsafe
	}
	for _, item := range protected {
		if item == nil {
			return nil, ErrStateUnsafe
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hold, err := lease.Hold(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrStateUnsafe
	}
	o := &StateObservation{root: root, protected: append([]*paths.Root(nil), protected...), lease: lease, hold: hold, scope: scope, maxBytes: maxBytes, authorize: authorize}
	defer func() {
		if resultErr != nil {
			_ = o.Close()
			out = nil
		}
	}()
	if err := o.checkBase(ctx); err != nil {
		return nil, err
	}
	var total int64
	for i, path := range []string{name, name + "-wal", name + "-shm", name + "-journal"} {
		file, err := openStateIdentity(root, path)
		if errors.Is(err, os.ErrNotExist) && i != 0 {
			o.files = append(o.files, observedStateFile{name: path})
			continue
		}
		if err != nil || file == nil || stateOwner(file, false) != nil {
			if file != nil {
				_ = file.Close()
			}
			return nil, ErrStateUnsafe
		}
		info, err := file.Stat()
		if err != nil || info.Size() < 0 || info.Size() > maxBytes-total {
			_ = file.Close()
			if err != nil {
				return nil, ErrStateUnsafe
			}
			return nil, ErrStateLimit
		}
		total += info.Size()
		o.files = append(o.files, observedStateFile{name: path, file: file})
	}
	if err := o.recheck(ctx); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *StateObservation) recheck(ctx context.Context) error {
	if err := o.checkBase(ctx); err != nil {
		return err
	}
	var total int64
	for _, previous := range o.files {
		current, err := openStateIdentity(o.root, previous.name)
		if errors.Is(err, os.ErrNotExist) && previous.file == nil {
			continue
		}
		if err != nil || current == nil || previous.file == nil {
			if current != nil {
				_ = current.Close()
			}
			return ErrStateChanged
		}
		oldInfo, oldErr := previous.file.Stat()
		newInfo, newErr := current.Stat()
		err = errors.Join(oldErr, newErr, stateOwner(previous.file, false), stateOwner(current, false), current.Close())
		if err != nil || !os.SameFile(oldInfo, newInfo) {
			return ErrStateChanged
		}
		if newInfo.Size() < 0 || newInfo.Size() > o.maxBytes-total {
			return ErrStateLimit
		}
		total += newInfo.Size()
	}
	return o.checkBase(ctx)
}

// Recheck detects changed identities, sidecar presence and current authority.
// It does not reserve a path against subsequent changes by another process.
func (o *StateObservation) Recheck(ctx context.Context) error {
	if o == nil {
		return ErrStateUnsafe
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.recheck(ctx)
}

// Close releases only the file handles and lifecycle attachment it owns.
func (o *StateObservation) Close() error {
	if o == nil {
		return ErrStateUnsafe
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrStateUnsafe
	}
	o.closed = true
	var err error
	for _, item := range o.files {
		if item.file != nil {
			err = errors.Join(err, item.file.Close())
		}
	}
	if o.hold != nil {
		err = errors.Join(err, o.hold.Close())
	}
	if err != nil {
		return ErrStateUnsafe
	}
	return nil
}
