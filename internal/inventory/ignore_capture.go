package inventory

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

type IgnoreCaptureBudget struct {
	MaxFiles      int
	MaxFileBytes  int
	MaxTotalBytes int
}

// CaptureDirectoryIgnoreSources reads only authorized regular .gitignore
// files already discovered by a completed candidate walk. It does not resolve
// global or Git metadata exclusions or prove complete workspace membership.
func CaptureDirectoryIgnoreSources(ctx context.Context, root *paths.Root, walk CandidateWalk, scanner *policy.Scanner, budget IgnoreCaptureBudget, authorize AuthorizeCapture) ([]IgnoreSource, error) {
	if ctx == nil || root == nil || scanner == nil || authorize == nil || budget.MaxFiles < 1 || budget.MaxFiles > 4096 || budget.MaxFileBytes < 1 || budget.MaxFileBytes > 4*1024*1024 || budget.MaxTotalBytes < 1 || budget.MaxTotalBytes > 4*1024*1024 || len(walk.Candidates) > 1000000 {
		return nil, ErrIgnoreBudget
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrIgnoreBudget
	}
	if walk.IgnoreRulesUnknown {
		return nil, ErrIgnoreUnsupported
	}
	var sources []IgnoreSource
	var capturedPaths []string
	seen := make(map[string]bool)
	total := 0
	for _, candidate := range walk.Candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if path.Base(candidate.Path) != ".gitignore" {
			continue
		}
		if seen[candidate.Path] {
			return nil, ErrIgnoreUnsupported
		}
		seen[candidate.Path] = true
		if candidate.Kind == "symlink" {
			continue
		} // Git does not follow .gitignore symlinks.
		if candidate.Kind != "regular" {
			return nil, ErrIgnoreUnsupported
		}
		if len(sources) >= budget.MaxFiles {
			return nil, ErrIgnoreBudget
		}
		file, err := CaptureFile(ctx, root, scanner, candidate.Path, budget.MaxFileBytes, authorize)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrCaptureChanged) {
				return nil, errors.Join(ErrWalkChanged, err)
			}
			return nil, err
		}
		if file.Len() > budget.MaxTotalBytes-total {
			return nil, ErrIgnoreBudget
		}
		total += file.Len()
		base := path.Dir(candidate.Path)
		if base == "." {
			base = ""
		}
		sources = append(sources, IgnoreSource{Kind: "directory", Base: base, Rules: file.Bytes()})
		capturedPaths = append(capturedPaths, candidate.Path)
	}
	for i, name := range capturedPaths {
		current, err := CaptureFile(ctx, root, scanner, name, budget.MaxFileBytes, authorize)
		if err != nil {
			return nil, errors.Join(ErrWalkChanged, err)
		}
		if !bytes.Equal(current.Bytes(), sources[i].Rules) {
			return nil, ErrWalkChanged
		}
	}
	if err := root.Verify(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Base < sources[j].Base })
	if sources == nil {
		sources = []IgnoreSource{}
	}
	return sources, nil
}

// CaptureInfoExclude reads $GIT_COMMON_DIR/info/exclude under an explicitly
// authorized metadata root. A missing file is checked twice; global excludes
// and repository configuration are separate inputs, not assumed absent.
func CaptureInfoExclude(ctx context.Context, metadata *paths.Root, scanner *policy.Scanner, maxBytes int, authorize AuthorizeCapture) (*IgnoreSource, error) {
	return captureOptionalIgnore(ctx, metadata, "info/exclude", "info", scanner, maxBytes, authorize)
}

// CaptureApprovedGlobalIgnore accepts only a caller-selected relative path
// within an explicitly authorized root. It does not discover or trust Git's
// configuration, expand home paths, or silently authorize an external file.
func CaptureApprovedGlobalIgnore(ctx context.Context, root *paths.Root, relative string, scanner *policy.Scanner, maxBytes int, authorize AuthorizeCapture) (*IgnoreSource, error) {
	return captureOptionalIgnore(ctx, root, relative, "global", scanner, maxBytes, authorize)
}

func captureOptionalIgnore(ctx context.Context, root *paths.Root, name, kind string, scanner *policy.Scanner, maxBytes int, authorize AuthorizeCapture) (*IgnoreSource, error) {
	if ctx == nil || root == nil || scanner == nil || authorize == nil || maxBytes < 1 || maxBytes > 4*1024*1024 {
		return nil, ErrIgnoreBudget
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrIgnoreBudget
	}
	file, err := CaptureFile(ctx, root, scanner, name, maxBytes, authorize)
	if errors.Is(err, os.ErrNotExist) {
		if err := authorize(ctx, name); err != nil {
			return nil, err
		}
		current, checkErr := root.OpenRegular(name)
		if checkErr == nil {
			return nil, errors.Join(ErrWalkChanged, current.Close())
		}
		if !errors.Is(checkErr, os.ErrNotExist) {
			return nil, errors.Join(ErrWalkChanged, checkErr)
		}
		if err := root.Verify(); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	current, err := CaptureFile(ctx, root, scanner, name, maxBytes, authorize)
	if err != nil {
		return nil, errors.Join(ErrWalkChanged, err)
	}
	if !bytes.Equal(file.Bytes(), current.Bytes()) {
		return nil, ErrWalkChanged
	}
	if err := root.Verify(); err != nil {
		return nil, err
	}
	return &IgnoreSource{Kind: kind, Rules: file.Bytes()}, nil
}
