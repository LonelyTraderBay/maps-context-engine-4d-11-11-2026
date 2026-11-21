package golang

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/sys/windows"
)

func LoaderScratchVerifier(scratch *paths.Root, authorize func(context.Context) error) func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
	return windowsScratchVerifier(scratch, nil, authorize)
}

// WindowsLoaderScratchVerifier also permits the identified AppContainer to
// read the capture. Writable tool output belongs outside input.
func WindowsLoaderScratchVerifier(scratch *paths.Root, reader *windows.SID, authorize func(context.Context) error) func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
	return windowsScratchVerifier(scratch, reader, authorize)
}

func windowsScratchVerifier(scratch *paths.Root, reader *windows.SID, authorize func(context.Context) error) func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
	return func(ctx context.Context, scratchPath, workspacePath string, captured *snapshot.Snapshot) (ok bool, resultErr error) {
		if ctx == nil || scratch == nil || authorize == nil || captured == nil {
			return false, ErrLoaderInput
		}
		if _, bounded := ctx.Deadline(); !bounded {
			return false, ErrLoaderInput
		}
		if err := authorize(ctx); err != nil {
			return false, loaderAuthorityError(ctx)
		}
		actual, err := scratch.CanonicalPath()
		if err != nil || scratchPath != actual || workspacePath != filepath.Join(actual, "input") {
			return false, nil
		}
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return false, err
		}
		acl := func(file *os.File) error {
			if reader == nil {
				return runtime.VerifyWindowsPrivateACL(file, user.User.Sid)
			}
			return runtime.VerifyWindowsReadACL(file, user.User.Sid, reader)
		}
		anchor, err := scratch.DirectoryHandle()
		if err != nil {
			return false, nil
		}
		if err := errors.Join(acl(anchor), anchor.Close()); err != nil {
			return false, nil
		}
		files := captured.Manifest().Files
		if len(files) == 0 || len(files) > 128 {
			return false, nil
		}
		tree := &windowsCapturedTree{children: map[string]*windowsCapturedTree{}}
		nodes := 1
		for _, item := range files {
			file, exists := captured.File(item.Path)
			parts := strings.Split(item.Path, "/")
			if !exists || item.Status != "captured" || item.ContentHash == nil || item.ByteLength == nil ||
				*item.ContentHash != file.ContentHash() || *item.ByteLength != int64(file.Len()) || file.Len() > 64<<10 || len(parts) > 64 {
				return false, nil
			}
			current := tree
			for _, part := range parts {
				if strings.EqualFold(part, ".git") || current.file != nil {
					return false, nil
				}
				child := current.children[part]
				if child == nil {
					nodes++
					if nodes > 384 {
						return false, nil
					}
					child = &windowsCapturedTree{children: map[string]*windowsCapturedTree{}}
					current.children[part] = child
				}
				current = child
			}
			if len(current.children) != 0 || current.file != nil {
				return false, nil
			}
			current.file = &file
		}
		workspace, err := scratch.OpenDirectory("input")
		if err != nil {
			return false, nil
		}
		defer func() {
			if err := workspace.Close(); err != nil {
				ok = false
				resultErr = errors.Join(resultErr, err)
			}
		}()
		if err := tree.verify(ctx, workspace, acl); err != nil {
			return false, ctx.Err()
		}
		if err := errors.Join(scratch.Verify(), workspace.Verify(), authorize(ctx)); err != nil {
			return false, loaderAuthorityError(ctx)
		}
		return ctx.Err() == nil, ctx.Err()
	}
}

type windowsCapturedTree struct {
	children map[string]*windowsCapturedTree
	file     *snapshot.File
}

func (tree *windowsCapturedTree) verify(ctx context.Context, dir *paths.Root, acl func(*os.File) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return err
	}
	if err := acl(anchor); err != nil {
		return errors.Join(err, anchor.Close())
	}
	items, readErr := anchor.ReadDir(len(tree.children) + 1)
	if readErr == nil && len(items) == len(tree.children) {
		var extra []os.DirEntry
		extra, readErr = anchor.ReadDir(1)
		items = append(items, extra...)
	}
	if err := errors.Join(anchor.Close(), dir.Verify()); err != nil {
		return err
	}
	if !errors.Is(readErr, io.EOF) || len(items) != len(tree.children) {
		return ErrLoaderAuthority
	}
	for _, entry := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		child := tree.children[entry.Name()]
		if child == nil {
			return ErrLoaderAuthority
		}
		if child.file == nil {
			sub, err := dir.OpenDirectory(entry.Name())
			if err != nil {
				return err
			}
			if err := errors.Join(child.verify(ctx, sub, acl), sub.Close()); err != nil {
				return err
			}
			continue
		}
		file, err := dir.OpenRegular(entry.Name())
		if err != nil {
			return err
		}
		if err := acl(file); err != nil {
			return errors.Join(err, file.Close())
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, int64(child.file.Len())+1))
		if err := errors.Join(readErr, paths.VerifyRegularFile(file), file.Close()); err != nil || !bytes.Equal(raw, child.file.Bytes()) {
			return errors.Join(ErrLoaderAuthority, err)
		}
	}
	return dir.Verify()
}
