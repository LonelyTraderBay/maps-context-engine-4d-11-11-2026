package inventory

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

// WalkCandidates lists confined Windows directory entries without reading
// source bytes. Each walk uses a fresh directory enumeration handle.
func WalkCandidates(ctx context.Context, root *paths.Root, budget WalkBudget, authorizeRoot func(context.Context) error,
	authorizePath WalkAuthorization, exclude WalkExclusion) (CandidateWalk, error) {
	if ctx == nil || root == nil || authorizeRoot == nil || authorizePath == nil || exclude == nil ||
		budget.MaxVisited < 1 || budget.MaxVisited > 1000000 || budget.MaxDirectories < 1 || budget.MaxDirectories > 100000 ||
		budget.MaxDepth < 1 || budget.MaxDepth > 128 || budget.MaxPathBytes < 1 || budget.MaxPathBytes > 65536 ||
		budget.MaxTotalPathBytes < 1 || budget.MaxTotalPathBytes > 64<<20 {
		return CandidateWalk{}, ErrWalkBudget
	}
	if _, ok := ctx.Deadline(); !ok {
		return CandidateWalk{}, ErrWalkBudget
	}
	if err := authorizeRoot(ctx); err != nil {
		return CandidateWalk{}, err
	}
	name, err := root.CanonicalPath()
	if err != nil {
		return CandidateWalk{}, err
	}
	scan, err := paths.OpenRoot(name)
	if err != nil {
		return CandidateWalk{}, err
	}
	w := windowsCandidateWalker{ctx: ctx, root: root, authorize: authorizePath, exclude: exclude, budget: budget,
		result: CandidateWalk{Candidates: []Candidate{}}}
	if err := errors.Join(w.walk(scan, "", 0), scan.Close(), root.Verify(), authorizeRoot(ctx)); err != nil {
		return CandidateWalk{}, err
	}
	sort.Slice(w.result.Candidates, func(i, j int) bool { return w.result.Candidates[i].Path < w.result.Candidates[j].Path })
	return w.result, nil
}

type windowsCandidateWalker struct {
	ctx                             context.Context
	root                            *paths.Root
	budget                          WalkBudget
	authorize                       WalkAuthorization
	exclude                         WalkExclusion
	result                          CandidateWalk
	visited, directories, pathBytes int
}

func (w *windowsCandidateWalker) walk(dir *paths.Root, prefix string, depth int) (resultErr error) {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if depth > w.budget.MaxDepth || w.directories >= w.budget.MaxDirectories {
		return ErrWalkBudget
	}
	w.directories++
	handle, err := dir.DirectoryHandle()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, handle.Close()) }()
	before, err := handle.Stat()
	if err != nil {
		return err
	}
	var entries []os.DirEntry
	seen := map[string]bool{}
	for {
		page, pageErr := handle.ReadDir(128)
		if pageErr != nil && !errors.Is(pageErr, io.EOF) {
			return pageErr
		}
		for _, entry := range page {
			if err := w.ctx.Err(); err != nil {
				return err
			}
			w.visited++
			if w.visited > w.budget.MaxVisited {
				return ErrWalkBudget
			}
			folded := strings.ToUpper(entry.Name())
			if seen[folded] {
				return ErrWalkChanged
			}
			seen[folded] = true
			if folded == ".GIT" {
				if prefix != "" {
					w.add(prefix, "nested_root", true)
					return stableWindowsDirectory(dir, handle, before)
				}
				continue
			}
			entries = append(entries, entry)
		}
		if pageErr != nil {
			break
		}
	}
	for _, entry := range entries {
		if err := w.entry(entry, prefix, depth); err != nil {
			return err
		}
	}
	return stableWindowsDirectory(dir, handle, before)
}

func (w *windowsCandidateWalker) entry(entry os.DirEntry, prefix string, depth int) error {
	name := entry.Name()
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
		return nil
	}
	allowed, err := authorizeMapPath(w.ctx, path, entry.IsDir(), w.authorize)
	if err != nil {
		return err
	}
	if !allowed || policy.CheckPath(path) != nil {
		w.result.DeniedCount++
		w.result.IgnoreRulesUnknown = w.result.IgnoreRulesUnknown || strings.EqualFold(name, ".gitignore")
		return nil
	}
	if w.exclude(path, entry.IsDir()) {
		w.add(path, "excluded", entry.IsDir())
		return nil
	}
	if entry.Type()&os.ModeSymlink != 0 {
		// Windows reparse objects also include junctions. Exclude the path
		// from the walk and from any tracked descendants.
		w.add(path, "symlink", false)
		return nil
	}
	if entry.IsDir() {
		child, err := w.root.OpenDirectory(path)
		switch {
		case errors.Is(err, paths.ErrInvalidPath), errors.Is(err, paths.ErrUnsupported):
			w.add(path, "excluded", true)
			return nil
		case errors.Is(err, os.ErrPermission):
			w.add(path, "unreadable", true)
			return nil
		case err != nil:
			return errors.Join(ErrWalkChanged, err)
		}
		return errors.Join(w.walk(child, path, depth+1), child.Close())
	}
	file, err := w.root.OpenRegular(path)
	switch {
	case errors.Is(err, paths.ErrHardLinked):
		w.add(path, "hardlink", false)
	case errors.Is(err, paths.ErrInvalidPath), errors.Is(err, paths.ErrNotRegular), errors.Is(err, os.ErrPermission):
		w.add(path, "excluded", false)
	case err != nil:
		return errors.Join(ErrWalkChanged, err)
	default:
		w.add(path, "regular", false)
		return file.Close()
	}
	return nil
}

func (w *windowsCandidateWalker) add(path, kind string, dir bool) {
	w.result.Candidates = append(w.result.Candidates, Candidate{Path: path, Kind: kind, Directory: dir})
}

func stableWindowsDirectory(dir *paths.Root, handle *os.File, before os.FileInfo) error {
	after, err := handle.Stat()
	if err != nil || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		return errors.Join(ErrWalkChanged, err)
	}
	return dir.Verify()
}
