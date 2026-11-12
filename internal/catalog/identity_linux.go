//go:build linux

package catalog

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

// directoryIdentity is a verified, process-observed filesystem identity, not
// permission or proof that the underlying filesystem is stable across mounts.
func directoryIdentity(root *paths.Root) (path, identity string, resultErr error) {
	path, err := root.CanonicalPath()
	if err != nil {
		return "", "", err
	}
	file, err := root.DirectoryHandle()
	if err != nil {
		return "", "", err
	}
	defer func() {
		if err := file.Close(); err != nil {
			path, identity = "", ""
			resultErr = errors.Join(resultErr, err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return "", "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return "", "", paths.ErrUnsupported
	}
	if err := root.Verify(); err != nil {
		return "", "", err
	}
	return path, fmt.Sprintf("linux:%x:%x", uint64(stat.Dev), stat.Ino), nil
}

func sameDirectory(a, b *paths.Root) (same bool, resultErr error) {
	if err := a.Verify(); err != nil {
		return false, err
	}
	if err := b.Verify(); err != nil {
		return false, err
	}
	fa, err := a.DirectoryHandle()
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, fa.Close()) }()
	fb, err := b.DirectoryHandle()
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, fb.Close()) }()
	ia, err := fa.Stat()
	if err != nil {
		return false, err
	}
	ib, err := fb.Stat()
	if err != nil {
		return false, err
	}
	if err := errors.Join(a.Verify(), b.Verify()); err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}
