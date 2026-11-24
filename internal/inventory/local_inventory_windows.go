package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type WindowsInventoryRequest struct {
	Root, Metadata *paths.Root
	LinkedGit      string
	Scanner        *policy.Scanner
	MaxEntries     int
	AuthorizeRoot  func(context.Context) error
	AuthorizePath  WalkAuthorization
	Exclude        WalkExclusion
	ReadGlobal     func(context.Context, int) ([]byte, bool, error)
}

// InventoryWindowsLocal observes the pinned local Git metadata and the NTFS
// candidate tree twice. Global rules come from the protected profile's
// managed rule input. Ambient Git configuration is outside this profile.
func InventoryWindowsLocal(ctx context.Context, r WindowsInventoryRequest) (result FileMap, resultErr error) {
	if ctx == nil || r.Root == nil || r.Metadata == nil || r.Scanner == nil || r.AuthorizeRoot == nil ||
		r.AuthorizePath == nil || r.Exclude == nil || r.ReadGlobal == nil || r.MaxEntries < 1 || r.MaxEntries > 100000 {
		return FileMap{}, ErrWalkBudget
	}
	readIndex := func(ctx context.Context, limit int) ([]byte, error) {
		return readObservedCapture(ctx, r.Metadata, "index", limit)
	}
	if r.LinkedGit != "" {
		if err := r.AuthorizeRoot(ctx); err != nil {
			return FileMap{}, err
		}
		binding, err := runtime.BindLinkedGitMetadata(r.Root, r.Metadata, r.LinkedGit)
		if err != nil {
			return FileMap{}, err
		}
		defer func() {
			if err := binding.Close(); err != nil {
				result = FileMap{}
				resultErr = errors.Join(resultErr, err)
			}
		}()
		readIndex = binding.ReadIndexContext
	}
	first, err := observeWindowsInventory(ctx, r, readIndex)
	if err != nil {
		return FileMap{}, err
	}
	second, err := observeWindowsInventory(ctx, r, readIndex)
	if err != nil {
		return FileMap{}, err
	}
	if first.InputDigest != second.InputDigest {
		return FileMap{}, ErrWalkChanged
	}
	return second, nil
}

func observeWindowsInventory(ctx context.Context, r WindowsInventoryRequest, readIndex func(context.Context, int) ([]byte, error)) (FileMap, error) {
	if err := r.AuthorizeRoot(ctx); err != nil {
		return FileMap{}, err
	}
	index, indexDigest, ignoreCase, err := readWindowsTrackedIndex(ctx, r.Metadata, readIndex, r.MaxEntries)
	if err != nil {
		return FileMap{}, err
	}
	exclude, err := ExcludeIndexBoundaries(index.Index, r.Exclude)
	if err != nil {
		return FileMap{}, err
	}
	walk, err := WalkCandidates(ctx, r.Root,
		WalkBudget{MaxVisited: min(r.MaxEntries*4, 100000), MaxDirectories: min(r.MaxEntries, 100000),
			MaxDepth: 64, MaxPathBytes: 4096, MaxTotalPathBytes: 32 << 20},
		r.AuthorizeRoot, r.AuthorizePath, exclude)
	if err != nil {
		return FileMap{}, err
	}
	if err := verifyWindowsInventorySpelling(index.Index, walk); err != nil {
		return FileMap{}, err
	}
	sources, err := CaptureDirectoryIgnoreSources(ctx, r.Root, walk, r.Scanner,
		IgnoreCaptureBudget{MaxFiles: 1024, MaxFileBytes: 128 << 10, MaxTotalBytes: 1 << 20},
		func(ctx context.Context, name string) error {
			allowed, err := authorizeMapPath(ctx, name, false, r.AuthorizePath)
			if err == nil && !allowed {
				return ErrWalkAuthorityChanged
			}
			return err
		})
	if err != nil {
		return FileMap{}, err
	}
	info, err := CaptureInfoExclude(ctx, r.Metadata, r.Scanner, 128<<10, func(ctx context.Context, _ string) error {
		return r.AuthorizeRoot(ctx)
	})
	if err != nil {
		return FileMap{}, err
	}
	if info != nil {
		sources = append(sources, *info)
	}
	global, present, err := r.ReadGlobal(ctx, 128<<10)
	if err != nil {
		return FileMap{}, err
	}
	if present {
		sources = append(sources, IgnoreSource{Kind: "global", Rules: global})
	}
	ignored, err := evaluateWindowsIgnores(ctx, sources, walk, r.Scanner, ignoreCase)
	if err != nil {
		return FileMap{}, err
	}
	files, err := BuildFileMap(ctx, r.Root, index, walk, ignored, r.MaxEntries, r.AuthorizeRoot, r.AuthorizePath)
	if err != nil {
		return FileMap{}, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("MCE4D-WINDOWS-INVENTORY1\x00"))
	if err := json.NewEncoder(hash).Encode([]any{indexDigest, index.IndexMissing, sources, walk, files}); err != nil {
		return FileMap{}, err
	}
	files.InputDigest = hex.EncodeToString(hash.Sum(nil))
	return files, r.AuthorizeRoot(ctx)
}

func verifyWindowsInventorySpelling(index GitIndex, walk CandidateWalk) error {
	spelling := map[string]string{}
	check := func(name string) bool {
		folded := strings.ToUpper(name)
		if previous, ok := spelling[folded]; ok && previous != name {
			return false
		}
		spelling[folded] = name
		return true
	}
	for _, entry := range index.Entries {
		if !check(strings.TrimSuffix(string(entry.Path), "/")) {
			return ErrWalkChanged
		}
	}
	for _, entry := range walk.Candidates {
		if !check(entry.Path) {
			return ErrWalkChanged
		}
	}
	return nil
}
