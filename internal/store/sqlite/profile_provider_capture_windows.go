package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type WindowsProviderCapture struct {
	Snapshot                 *snapshot.Snapshot
	Generation, ManifestName string
	ProgramName              string
	Manifest, Program        []byte
	CacheLimit               int64
}

func (o windowsStoreMaintenance) withProviderCapture(ctx context.Context, capture WindowsProviderCapture, check func(context.Context) error,
	use func(*paths.Root, string) error) (resultErr error) {
	if !model.ValidUUID(capture.Generation) || capture.Snapshot == nil || use == nil ||
		capture.ManifestName != "mce4d-provider-"+capture.Generation+".json" ||
		capture.ProgramName != "worker.cjs" && capture.ProgramName != "worker.py" || len(capture.Program) > 1<<20 || len(capture.Manifest) > 1<<20 {
		return ErrProfileInput
	}
	manifest := capture.Snapshot.Manifest()
	if manifest.WorktreeID != o.files.scope {
		return ErrProfileDenied
	}
	bytes := int64(len(capture.Program) + len(capture.Manifest))
	for _, file := range manifest.Files {
		if file.Status == "captured" {
			bytes += *file.ByteLength
		}
	}
	cache, err := reserveWindowsCache(ctx, o.files, o.lease, "", check, capture.CacheLimit, bytes)
	if err != nil {
		return err
	}
	parent := cache.scratch
	defer func() { resultErr = errors.Join(resultErr, cache.Close()) }()
	name := "provider-" + capture.Generation
	if err := createWindowsProfileDirectory(ctx, parent, o.files.acl, name); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, paths.RemoveWindowsOwnedTree(cleanup, parent, name))
	}()
	scratch, err := parent.OpenDirectory(name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, scratch.Close()) }()
	dir, err := scratch.CanonicalPath()
	if err != nil {
		return err
	}
	if err := createWindowsProfileDirectory(ctx, scratch, o.files.acl, "input"); err != nil {
		return err
	}
	input, err := scratch.OpenDirectory("input")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, input.Close()) }()
	if err := writeWindowsScratchFile(ctx, scratch, o.files.acl, []string{capture.ProgramName}, capture.Program); err != nil {
		return err
	}
	if err := writeWindowsScratchFile(ctx, input, o.files.acl, []string{capture.ManifestName}, capture.Manifest); err != nil {
		return err
	}
	for _, item := range manifest.Files {
		if file, ok := capture.Snapshot.File(item.Path); ok {
			if err := writeWindowsScratchFile(ctx, input, o.files.acl, strings.Split(item.Path, "/"), file.Bytes()); err != nil {
				return err
			}
		}
	}
	if err := cache.Check(ctx); err != nil {
		return err
	}
	return errors.Join(use(input, filepath.Join(dir, capture.ProgramName)), cache.Check(ctx))
}
