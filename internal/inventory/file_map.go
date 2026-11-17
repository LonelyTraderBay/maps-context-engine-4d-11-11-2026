package inventory

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

type FileMapEntry struct {
	Path            string
	Origin          string // tracked, untracked
	State           string // candidate, missing, unmerged, sparse, boundary, excluded, unreadable, type_changed
	Directory       bool
	Stages          []uint8
	SkipWorktree    bool
	AssumeUnchanged bool
	IntentToAdd     bool
}

type FileMap struct {
	Entries          []FileMapEntry
	InputDigest      string // opaque digest of verified Git/config/ignore inputs, not source bytes
	DeniedCount      int
	InvalidPathCount int
	IgnoredCount     int
}

// ExcludeIndexBoundaries augments the caller's exclusions with gitlinks and
// sparse-index directories before walking, so their contents are not traversed.
// The index must come from a verified ParseGitIndex observation.
func ExcludeIndexBoundaries(index GitIndex, exclude WalkExclusion) (WalkExclusion, error) {
	if exclude == nil || len(index.Entries) > 100000 {
		return nil, ErrWalkBudget
	}
	boundaries := make(map[string]bool)
	for _, entry := range index.Entries {
		if entry.Mode != 0160000 && entry.Mode != 0040000 {
			continue
		}
		directory := entry.Mode == 0040000
		if !validIndexPath(entry.Path, directory) || directory && (!entry.SkipWorktree || !index.SparseDirectories || entry.Stage != 0) {
			return nil, ErrIndexCorrupt
		}
		name := strings.TrimSuffix(string(entry.Path), "/")
		if model.ValidateRelativePath(name, false) != nil {
			continue
		}
		boundaries[name] = true
	}
	return func(name string, directory bool) bool { return boundaries[name] || exclude(name, directory) }, nil
}

// BuildFileMap merges separately verified index and candidate observations.
// ignored is aligned with walk.Candidates and must come from the complete,
// authorized effective-ignore inputs. The result is preliminary: capture and
// full membership revalidation are required before a generation is published.
func BuildFileMap(ctx context.Context, root *paths.Root, index IndexObservation, walk CandidateWalk, ignored []bool, maxEntries int, authorizeRoot func(context.Context) error, authorizePath WalkAuthorization) (FileMap, error) {
	if ctx == nil || root == nil || authorizeRoot == nil || authorizePath == nil || maxEntries < 1 || maxEntries > 100000 || len(ignored) != len(walk.Candidates) || len(walk.Candidates) > 1000000 || len(index.Index.Entries) > 100000 {
		return FileMap{}, ErrWalkBudget
	}
	if _, ok := ctx.Deadline(); !ok {
		return FileMap{}, ErrWalkBudget
	}
	if walk.IgnoreRulesUnknown {
		return FileMap{}, ErrIgnoreUnsupported
	}
	if err := authorizeRoot(ctx); err != nil {
		return FileMap{}, err
	}
	if err := root.Verify(); err != nil {
		return FileMap{}, err
	}
	if index.IndexMissing && len(index.Index.Entries) != 0 {
		return FileMap{}, ErrIndexCorrupt
	}
	candidates := make(map[string]Candidate, len(walk.Candidates))
	boundaries := make(map[string]bool)
	for i, candidate := range walk.Candidates {
		if candidate.Path == "" || model.ValidateRelativePath(candidate.Path, false) != nil || policy.CheckPath(candidate.Path) != nil {
			return FileMap{}, ErrWalkChanged
		}
		if i > 0 && walk.Candidates[i-1].Path >= candidate.Path {
			return FileMap{}, ErrWalkChanged
		}
		switch candidate.Kind {
		case "regular", "symlink", "hardlink", "nested_root", "excluded", "unsupported", "unreadable":
		default:
			return FileMap{}, ErrWalkChanged
		}
		if (candidate.Kind == "nested_root" || candidate.Kind == "unreadable") && !candidate.Directory || (candidate.Kind == "regular" || candidate.Kind == "symlink" || candidate.Kind == "hardlink" || candidate.Kind == "unsupported") && candidate.Directory {
			return FileMap{}, ErrWalkChanged
		}
		candidates[candidate.Path] = candidate
		if candidate.Kind == "nested_root" || candidate.Kind == "excluded" || candidate.Kind == "unreadable" || candidate.Kind == "symlink" {
			boundaries[candidate.Path] = true
		}
	}
	// Submodules and sparse-index directories are boundaries even when they are
	// missing from the current candidate walk.
	for _, entry := range index.Index.Entries {
		if entry.Mode == 0160000 || entry.Mode == 0040000 {
			name := strings.TrimSuffix(string(entry.Path), "/")
			if model.ValidateRelativePath(name, false) == nil {
				boundaries[name] = true
			}
		}
	}
	result := FileMap{Entries: []FileMapEntry{}, DeniedCount: walk.DeniedCount, InvalidPathCount: walk.InvalidPathCount}
	tracked := make(map[string]bool, len(index.Index.Entries))
	add := func(item FileMapEntry) error {
		if len(result.Entries) >= maxEntries {
			return ErrWalkBudget
		}
		result.Entries = append(result.Entries, item)
		return nil
	}
	for i := 0; i < len(index.Index.Entries); {
		if err := ctx.Err(); err != nil {
			return FileMap{}, err
		}
		entry := index.Index.Entries[i]
		if !validIndexMode(entry.Mode) || !validIndexPath(entry.Path, entry.Mode == 0040000) || entry.Mode == 0040000 && (!entry.SkipWorktree || !index.Index.SparseDirectories || entry.Stage != 0) {
			return FileMap{}, ErrIndexCorrupt
		}
		raw := string(entry.Path)
		j := i + 1
		for j < len(index.Index.Entries) && string(index.Index.Entries[j].Path) == raw {
			j++
		}
		if i > 0 && string(index.Index.Entries[i-1].Path) >= raw {
			return FileMap{}, ErrIndexCorrupt
		}
		name := strings.TrimSuffix(raw, "/")
		if tracked[name] {
			return FileMap{}, ErrIndexCorrupt
		}
		tracked[name] = true
		if model.ValidateRelativePath(name, false) != nil {
			result.InvalidPathCount++
			i = j
			continue
		}
		isDir := entry.Mode == 0160000 || entry.Mode == 0040000
		allowed, err := authorizeMapPath(ctx, name, isDir, authorizePath)
		if err != nil {
			return FileMap{}, err
		}
		if !allowed {
			result.DeniedCount++
			i = j
			continue
		}
		if hasParentBoundary(name, boundaries) {
			i = j
			continue
		}
		item := FileMapEntry{Path: name, Origin: "tracked", Directory: isDir, Stages: []uint8{}}
		for k := i; k < j; k++ {
			part := index.Index.Entries[k]
			if !validIndexMode(part.Mode) || part.Stage > 3 || k > i && (part.Stage <= index.Index.Entries[k-1].Stage || index.Index.Entries[k-1].Stage == 0) {
				return FileMap{}, ErrIndexCorrupt
			}
			item.Stages = append(item.Stages, part.Stage)
			item.SkipWorktree = item.SkipWorktree || part.SkipWorktree
			item.AssumeUnchanged = item.AssumeUnchanged || part.AssumeUnchanged
			item.IntentToAdd = item.IntentToAdd || part.IntentToAdd
		}
		switch {
		case entry.Mode == 0160000:
			item.State = "boundary"
		case entry.Mode == 0040000:
			item.State = "sparse"
		case entry.Stage != 0:
			item.State = "unmerged"
		default:
			candidate, ok := candidates[name]
			if ok {
				item.State = candidateState(candidate.Kind)
				item.Directory = candidate.Directory
			} else {
				state, err := observeMissingTracked(root, name, item.SkipWorktree)
				if err != nil {
					return FileMap{}, err
				}
				item.State = state
			}
		}
		if err := add(item); err != nil {
			return FileMap{}, err
		}
		i = j
	}
	for i, candidate := range walk.Candidates {
		if err := ctx.Err(); err != nil {
			return FileMap{}, err
		}
		if tracked[candidate.Path] || hasParentBoundary(candidate.Path, boundaries) {
			continue
		}
		isDir := candidate.Directory
		allowed, err := authorizeMapPath(ctx, candidate.Path, isDir, authorizePath)
		if err != nil {
			return FileMap{}, err
		}
		if !allowed {
			result.DeniedCount++
			continue
		}
		if ignored[i] {
			result.IgnoredCount++
			continue
		}
		if err := add(FileMapEntry{Path: candidate.Path, Origin: "untracked", State: candidateState(candidate.Kind), Directory: isDir, Stages: []uint8{}}); err != nil {
			return FileMap{}, err
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	for _, item := range result.Entries {
		allowed, err := authorizeMapPath(ctx, item.Path, item.Directory, authorizePath)
		if err != nil {
			return FileMap{}, err
		}
		if !allowed {
			return FileMap{}, ErrWalkAuthorityChanged
		}
	}
	if err := root.Verify(); err != nil {
		return FileMap{}, err
	}
	if err := authorizeRoot(ctx); err != nil {
		return FileMap{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileMap{}, err
	}
	return result, nil
}

func authorizeMapPath(ctx context.Context, name string, directory bool, authorize WalkAuthorization) (bool, error) {
	if policy.CheckPath(name) != nil {
		return false, nil
	}
	for offset := 0; ; {
		next := strings.IndexByte(name[offset:], '/')
		if next < 0 {
			return authorize(ctx, name, directory)
		}
		offset += next
		allowed, err := authorize(ctx, name[:offset], true)
		if err != nil || !allowed {
			return allowed, err
		}
		offset++
	}
}

func hasParentBoundary(name string, boundaries map[string]bool) bool {
	for i := 0; i < len(name); i++ {
		if name[i] == '/' && boundaries[name[:i]] {
			return true
		}
	}
	return false
}

func candidateState(kind string) string {
	switch kind {
	case "regular":
		return "candidate"
	case "nested_root":
		return "boundary"
	case "unreadable":
		return "unreadable"
	default:
		return "excluded"
	}
}

func observeMissingTracked(root *paths.Root, name string, skip bool) (string, error) {
	file, err := root.OpenRegular(name)
	if err == nil {
		return "", errors.Join(ErrWalkChanged, file.Close())
	}
	if errors.Is(err, os.ErrNotExist) {
		if skip {
			return "sparse", nil
		}
		return "missing", nil
	}
	if errors.Is(err, paths.ErrNotRegular) {
		return "type_changed", nil
	}
	if errors.Is(err, paths.ErrHardLinked) {
		return "excluded", nil
	}
	if errors.Is(err, os.ErrPermission) {
		return "unreadable", nil
	}
	return "", errors.Join(ErrWalkChanged, err)
}
