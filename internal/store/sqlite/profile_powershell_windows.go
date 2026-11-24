package sqlite

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// openWindowsProviderRuntime checks every entry in an installation staged
// by the controller, including loader dependencies beside the executable.
func (j windowsMaintenanceFiles) openWindowsProviderRuntime(ctx context.Context, name string) (_ *paths.Root, resultErr error) {
	if err := j.acl.checkWriter(); err != nil {
		return nil, err
	}
	child, err := j.openWindowsPoolDirectory(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, child.control.Close())
		}
	}()
	count := 0
	var verify func(windowsMaintenanceFiles, int) error
	verify = func(dir windowsMaintenanceFiles, depth int) (resultErr error) {
		if depth > 16 {
			return ErrStateLimit
		}
		handle, err := dir.directory(ctx)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, handle.Close()) }()
		name, err := dir.control.CanonicalPath()
		if err != nil {
			return err
		}
		list, err := os.Open(name)
		if err != nil {
			return err
		}
		entries, err := list.ReadDir(4097 - count)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if err := errors.Join(err, list.Close()); err != nil {
			return err
		}
		count += len(entries)
		if count > 4096 {
			return ErrStateLimit
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				subdir, err := dir.openWindowsPoolDirectory(entry.Name())
				if err != nil {
					return err
				}
				if err := errors.Join(verify(subdir, depth+1), subdir.control.Close()); err != nil {
					return err
				}
			} else {
				file, err := dir.control.OpenRegular(entry.Name())
				if err != nil {
					return err
				}
				if err := errors.Join(dir.acl.check(file), file.Close()); err != nil {
					return err
				}
			}
		}
		return dir.control.Verify()
	}
	if err := verify(child, 0); err != nil {
		return nil, err
	}
	return child.control, nil
}
