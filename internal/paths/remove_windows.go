package paths

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RemoveWindowsOwnedTree removes a disposable tree by relative handles. The
// caller must authenticate its parent, own the named tree and stop all its
// writers first. Any unexpected file kind or sharing conflict stops cleanup.
func RemoveWindowsOwnedTree(ctx context.Context, parent *Root, leaf string) error {
	parts, err := windowsSegments(leaf)
	if ctx == nil || err != nil || len(parts) != 1 {
		return ErrInvalidPath
	}
	remaining := 8192
	return removeWindowsTree(ctx, parent, leaf, 0, &remaining)
}

func removeWindowsTree(ctx context.Context, parent *Root, leaf string, depth int, remaining *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 32 || *remaining <= 0 {
		return ErrInvalidPath
	}
	child, err := parent.OpenDirectory(leaf)
	if err != nil {
		return err
	}
	dir, err := child.DirectoryHandle()
	if err != nil {
		return errors.Join(err, child.Close())
	}
	state, statErr := dir.Stat()
	err = statErr
	for err == nil {
		if err = ctx.Err(); err != nil {
			break
		}
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			*remaining--
			if *remaining < 0 || ctx.Err() != nil {
				err = errors.Join(ErrInvalidPath, ctx.Err())
				break
			}
			if entry.IsDir() {
				err = removeWindowsTree(ctx, child, entry.Name(), depth+1, remaining)
			} else {
				err = deleteWindowsLeaf(child, entry.Name(), nil)
			}
			if err != nil {
				break
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		err = errors.Join(err, readErr)
	}
	if err := errors.Join(err, child.Verify(), dir.Close(), child.Close(), ctx.Err()); err != nil {
		return err
	}
	return deleteWindowsLeaf(parent, leaf, state)
}

func deleteWindowsLeaf(parent *Root, leaf string, directory os.FileInfo) (resultErr error) {
	parts, err := windowsSegments(leaf)
	if err != nil || len(parts) != 1 {
		return ErrInvalidPath
	}
	relative, err := windows.NewNTUnicodeString(leaf)
	if err != nil {
		return err
	}
	name, err := parent.CanonicalPath()
	if err != nil {
		return err
	}
	opts := uint32(windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_NON_DIRECTORY_FILE)
	if directory != nil {
		opts = (opts &^ windows.FILE_NON_DIRECTORY_FILE) | windows.FILE_DIRECTORY_FILE
	}
	var handle windows.Handle
	err = withWindowsHandle(parent.dir, func(h windows.Handle) error {
		attr := windows.OBJECT_ATTRIBUTES{RootDirectory: h, ObjectName: relative, Attributes: windows.OBJ_DONT_REPARSE}
		attr.Length = uint32(unsafe.Sizeof(attr))
		var status windows.IO_STATUS_BLOCK
		return windows.NtCreateFile(&handle, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, &attr,
			&status, nil, 0, 0, windows.FILE_OPEN, opts, 0, 0)
	})
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(handle), "owned-windows-removal")
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := errors.Join(verifyWindowsName(file, filepath.Join(name, leaf), true), parent.Verify()); err != nil {
		return err
	}
	if directory != nil {
		info, err := file.Stat()
		if err != nil || !os.SameFile(directory, info) {
			return errors.Join(ErrRootChanged, err)
		}
		if _, err := windowsEntryInfo(handle, true); err != nil {
			return err
		}
	} else if err := VerifyRegularFile(file); err != nil {
		return err
	}
	remove := byte(1)
	var status windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(handle, &status, &remove, 1, windows.FileDispositionInformation)
}
