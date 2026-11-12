//go:build linux

package inventory

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"golang.org/x/sys/unix"
)

type authorizedWalkPath struct {
	path      string
	directory bool
}

// WalkCandidates enumerates names through confined directory handles. It
// never reads file contents or evaluates ignore rules. The caller must
// separately merge the Git index, apply effective ignores, capture allowed
// bytes, and repeat membership checks when freshness matters.
func WalkCandidates(ctx context.Context, root *paths.Root, budget WalkBudget, authorizeRoot func(context.Context) error, authorizePath WalkAuthorization, exclude WalkExclusion) (CandidateWalk, error) {
	if ctx == nil || root == nil || authorizeRoot == nil || authorizePath == nil || exclude == nil ||
		budget.MaxVisited < 1 || budget.MaxVisited > 1000000 || budget.MaxDirectories < 1 || budget.MaxDirectories > 100000 ||
		budget.MaxDepth < 1 || budget.MaxDepth > 128 || budget.MaxPathBytes < 1 || budget.MaxPathBytes > 65536 ||
		budget.MaxTotalPathBytes < 1 || budget.MaxTotalPathBytes > 64*1024*1024 {
		return CandidateWalk{}, ErrWalkBudget
	}
	if _, ok := ctx.Deadline(); !ok {
		return CandidateWalk{}, ErrWalkBudget
	}
	if err := authorizeRoot(ctx); err != nil {
		return CandidateWalk{}, err
	}
	state := candidateWalker{ctx: ctx, root: root, budget: budget, authorize: authorizePath, exclude: exclude, result: CandidateWalk{Candidates: []Candidate{}}}
	if err := state.walk(root, "", 0); err != nil {
		return CandidateWalk{}, err
	}
	if err := root.Verify(); err != nil {
		return CandidateWalk{}, err
	}
	if err := authorizeRoot(ctx); err != nil {
		return CandidateWalk{}, err
	}
	for _, item := range state.authorized {
		allowed, err := authorizePath(ctx, item.path, item.directory)
		if err != nil {
			return CandidateWalk{}, err
		}
		if !allowed {
			return CandidateWalk{}, ErrWalkAuthorityChanged
		}
	}
	if err := ctx.Err(); err != nil {
		return CandidateWalk{}, err
	}
	if err := root.Verify(); err != nil {
		return CandidateWalk{}, err
	}
	sort.Slice(state.result.Candidates, func(i, j int) bool { return state.result.Candidates[i].Path < state.result.Candidates[j].Path })
	return state.result, nil
}

type candidateWalker struct {
	ctx                             context.Context
	root                            *paths.Root
	budget                          WalkBudget
	authorize                       WalkAuthorization
	exclude                         WalkExclusion
	result                          CandidateWalk
	authorized                      []authorizedWalkPath
	visited, directories, pathBytes int
}

func (w *candidateWalker) walk(root *paths.Root, prefix string, depth int) (resultErr error) {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if depth > w.budget.MaxDepth || w.directories >= w.budget.MaxDirectories {
		return ErrWalkBudget
	}
	w.directories++
	if err := root.Verify(); err != nil {
		return err
	}
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, anchor.Close()) }()
	fd, err := unix.Openat(int(anchor.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), "inventory-directory")
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	var before unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &before); err != nil {
		return err
	}
	for {
		entries, err := directory.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, entry := range entries {
			if err := w.ctx.Err(); err != nil {
				return err
			}
			w.visited++
			if w.visited > w.budget.MaxVisited {
				return ErrWalkBudget
			}
			name := entry.Name()
			if name == ".git" {
				continue
			}
			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}
			if len(path) > w.budget.MaxPathBytes || len(path) > w.budget.MaxTotalPathBytes-w.pathBytes {
				return ErrWalkBudget
			}
			w.pathBytes += len(path)
			if model.ValidateRelativePath(path, false) != nil {
				w.result.InvalidPathCount++
				continue
			}
			if err := policy.CheckPath(path); err != nil {
				w.result.DeniedCount++
				continue
			}
			var st unix.Stat_t
			if err := unix.Fstatat(int(directory.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return errors.Join(ErrWalkChanged, err)
			}
			isDir := st.Mode&unix.S_IFMT == unix.S_IFDIR
			allowed, err := w.authorize(w.ctx, path, isDir)
			if err != nil {
				return err
			}
			if !allowed {
				w.result.DeniedCount++
				if name == ".gitignore" {
					w.result.IgnoreRulesUnknown = true
				}
				continue
			}
			w.authorized = append(w.authorized, authorizedWalkPath{path: path, directory: isDir})
			if w.exclude(path, isDir) {
				w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: "excluded", Directory: isDir})
				continue
			}
			switch st.Mode & unix.S_IFMT {
			case unix.S_IFDIR:
				if err := w.walkChild(path, depth+1, st); err != nil {
					return err
				}
			case unix.S_IFREG:
				kind := "regular"
				if st.Nlink != 1 {
					kind = "hardlink"
				}
				w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: kind})
			case unix.S_IFLNK:
				w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: "symlink"})
			default:
				w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: "unsupported"})
			}
		}
		if err != nil {
			break
		}
	}
	if err := root.Verify(); err != nil {
		return err
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &after); err != nil {
		return err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return ErrWalkChanged
	}
	return nil
}

func (w *candidateWalker) walkChild(path string, depth int, expected unix.Stat_t) (resultErr error) {
	if depth > w.budget.MaxDepth {
		return ErrWalkBudget
	}
	child, err := w.root.OpenDirectory(path)
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: "unreadable", Directory: true})
		return nil
	}
	if errors.Is(err, unix.EXDEV) {
		w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: "excluded", Directory: true})
		return nil
	}
	if err != nil {
		return errors.Join(ErrWalkChanged, err)
	}
	defer func() { resultErr = errors.Join(resultErr, child.Close()) }()
	anchor, err := child.DirectoryHandle()
	if err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(anchor.Fd()), &st); err != nil {
		return errors.Join(err, anchor.Close())
	}
	if st.Dev != expected.Dev || st.Ino != expected.Ino || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.Join(ErrWalkChanged, anchor.Close())
	}
	err = unix.Fstatat(int(anchor.Fd()), ".git", &st, unix.AT_SYMLINK_NOFOLLOW)
	closeErr := anchor.Close()
	if closeErr != nil {
		return closeErr
	}
	if err == nil {
		w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: "nested_root", Directory: true})
		return child.Verify()
	}
	if !errors.Is(err, unix.ENOENT) {
		return err
	}
	if err := w.walk(child, path, depth); err != nil {
		return err
	}
	anchor, err = child.DirectoryHandle()
	if err != nil {
		return err
	}
	err = unix.Fstatat(int(anchor.Fd()), ".git", &st, unix.AT_SYMLINK_NOFOLLOW)
	closeErr = anchor.Close()
	if closeErr != nil {
		return closeErr
	}
	if err == nil {
		return ErrWalkChanged
	}
	if !errors.Is(err, unix.ENOENT) {
		return err
	}
	return child.Verify()
}
