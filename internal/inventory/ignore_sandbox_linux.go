//go:build linux

package inventory

import (
	"context"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/unix"
)

// PrepareIgnoreSandbox creates only fixed, empty scaffolding in an already
// authorized, empty, owner-only directory. It never removes existing files.
// If interrupted, the caller must inspect the partial directory and choose a
// new owned location; this function does not retry or clean it automatically.
func PrepareIgnoreSandbox(ctx context.Context, root *paths.Root, authorize func(context.Context) error) (resultErr error) {
	if ctx == nil || root == nil || authorize == nil {
		return ErrIgnoreUnsupported
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrIgnoreBudget
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authorize(ctx); err != nil {
		return err
	}
	if err := checkSandboxNames(root, []string{}); err != nil {
		return err
	}
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	for _, name := range []string{"git", "work"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := unix.Mkdirat(int(anchor.Fd()), name, 0700); err != nil {
			return err
		}
	}
	gitRoot, err := root.OpenDirectory("git")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, gitRoot.Close()) }()
	gitFD, err := gitRoot.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, gitFD.Close()) }()
	for _, name := range []string{"objects", "refs"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := unix.Mkdirat(int(gitFD.Fd()), name, 0700); err != nil {
			return err
		}
	}
	refs, err := gitRoot.OpenDirectory("refs")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, refs.Close()) }()
	refsFD, err := refs.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, refsFD.Close()) }()
	if err := unix.Mkdirat(int(refsFD.Fd()), "heads", 0700); err != nil {
		return err
	}
	for _, item := range []struct{ name, value string }{{"HEAD", ignoreSandboxHEAD}, {"config", ignoreSandboxConfig}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		fd, err := unix.Openat(int(gitFD.Fd()), item.name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), "ignore-sandbox-file")
		n, writeErr := file.WriteString(item.value)
		if writeErr == nil && n != len(item.value) {
			writeErr = ErrIgnoreUnsupported
		}
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return err
		}
	}
	if err := root.Verify(); err != nil {
		return err
	}
	gitCheck, workCheck, err := openIgnoreSandbox(root)
	if err != nil {
		return err
	}
	if err := errors.Join(gitCheck.Close(), workCheck.Close()); err != nil {
		return err
	}
	if err := authorize(ctx); err != nil {
		return err
	}
	return ctx.Err()
}
