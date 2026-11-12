//go:build linux

package golang

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/sys/unix"
)

// LoaderScratchVerifier checks the current contents of an already authorized
// and exclusively owned scratch/input directory. It never creates or removes
// paths. Ownership/authorization and absence of same-user writers remain the
// caller's responsibility; observations cannot make a filesystem atomic.
func LoaderScratchVerifier(scratch *paths.Root, authorize func(context.Context) error) func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
	return func(ctx context.Context, scratchPath, workspacePath string, captured *snapshot.Snapshot) (ok bool, resultErr error) {
		if ctx == nil || scratch == nil || authorize == nil || captured == nil {
			return false, ErrLoaderInput
		}
		if _, ok := ctx.Deadline(); !ok {
			return false, ErrLoaderInput
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := authorize(ctx); err != nil {
			return false, loaderAuthorityError(ctx)
		}
		actualScratch, err := scratch.CanonicalPath()
		if err != nil || actualScratch != scratchPath || filepath.Join(actualScratch, "input") != workspacePath {
			return false, nil
		}
		if err := checkLoaderDirectory(scratch); err != nil {
			return false, nil
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
		if err := checkLoaderDirectory(workspace); err != nil {
			return false, nil
		}
		manifest := captured.Manifest()
		if len(manifest.Files) > 128 {
			return false, nil
		}
		// Inventory intentionally skips .git. A loader sandbox must reject it,
		// including in each captured package directory, without reading it.
		dirs := map[string]bool{".": true}
		for _, item := range manifest.Files {
			depth := 0
			for parent := path.Dir(item.Path); parent != "."; parent = path.Dir(parent) {
				depth++
				if depth > 64 || len(dirs) >= 256 && !dirs[parent] {
					return false, nil
				}
				dirs[parent] = true
			}
		}
		dirnames := make([]string, 0, len(dirs))
		for name := range dirs {
			dirnames = append(dirnames, name)
		}
		sort.Strings(dirnames)
		for _, name := range dirnames {
			current := workspace
			if name != "." {
				current, err = workspace.OpenDirectory(name)
				if err != nil {
					return false, nil
				}
			}
			present, checkErr := loaderGitPresent(current)
			if name != "." {
				checkErr = errors.Join(checkErr, current.Close())
			}
			if checkErr != nil || present {
				return false, nil
			}
		}
		walk, err := inventory.WalkCandidates(ctx, workspace, inventory.WalkBudget{MaxVisited: 1024, MaxDirectories: 256, MaxDepth: 64, MaxPathBytes: 16384, MaxTotalPathBytes: 1024 * 1024}, authorize,
			func(ctx context.Context, _ string, _ bool) (bool, error) {
				if err := authorize(ctx); err != nil {
					return false, loaderAuthorityError(ctx)
				}
				return true, nil
			},
			func(string, bool) bool { return false })
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		}
		if walk.DeniedCount != 0 || walk.InvalidPathCount != 0 || walk.IgnoreRulesUnknown || len(walk.Candidates) != len(manifest.Files) {
			return false, nil
		}
		for i, item := range manifest.Files {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			candidate := walk.Candidates[i]
			if item.Status != "captured" || item.ContentHash == nil || item.ByteLength == nil || candidate.Path != item.Path || candidate.Kind != "regular" || candidate.Directory {
				return false, nil
			}
			file, exists := captured.File(item.Path)
			if !exists || file.Len() > 64*1024 || file.ContentHash() != *item.ContentHash || int64(file.Len()) != *item.ByteLength {
				return false, nil
			}
			current, err := workspace.OpenRegular(item.Path)
			if err != nil {
				return false, nil
			}
			matches, verifyErr := loaderScratchFileMatches(ctx, current, file.Bytes())
			closeErr := current.Close()
			if verifyErr != nil || closeErr != nil || !matches {
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				return false, nil
			}
		}
		if err := workspace.Verify(); err != nil {
			return false, nil
		}
		if err := scratch.Verify(); err != nil {
			return false, nil
		}
		if err := authorize(ctx); err != nil {
			return false, loaderAuthorityError(ctx)
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return true, nil
	}
}

func loaderGitPresent(root *paths.Root) (present bool, resultErr error) {
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	var st unix.Stat_t
	err = unix.Fstatat(int(anchor.Fd()), ".git", &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func checkLoaderDirectory(root *paths.Root) (resultErr error) {
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	fd, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), "loader-scratch-directory")
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return ErrLoaderAuthority
	}
	for _, attribute := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		n, err := unix.Fgetxattr(fd, attribute, nil)
		if err != nil && !errors.Is(err, unix.ENODATA) {
			return ErrLoaderAuthority
		}
		if n > 0 {
			return ErrLoaderAuthority
		}
	}
	return root.Verify()
}

func loaderScratchFileMatches(ctx context.Context, file *os.File, expected []byte) (bool, error) {
	before, err := file.Stat()
	if err != nil || before.Size() != int64(len(expected)) || paths.VerifyRegularFile(file) != nil {
		return false, err
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	if err != nil {
		return false, err
	}
	after, err := file.Stat()
	if err != nil || paths.VerifyRegularFile(file) != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return os.SameFile(before, after) && before.Size() == after.Size() && before.Mode() == after.Mode() && before.ModTime() == after.ModTime() && bytes.Equal(data, expected), nil
}
