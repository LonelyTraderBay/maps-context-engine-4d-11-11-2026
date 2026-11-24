// Package indexer coordinates source observations for index publication.
package indexer

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrSnapshotChanged      = errors.New("workspace membership or captured source changed")
	ErrSnapshotUnverifiable = errors.New("capture cannot be fully revalidated")
)

type RevalidateRequest struct {
	Inventory inventory.LocalInventoryRequest
	Expected  inventory.FileMap
	Snapshot  *snapshot.Snapshot
	// MatchManifestInventory uses the first live file map after matching its
	// input digest to the pinned capture. Only the read-only verifier uses it.
	MatchManifestInventory bool
	MaxManifestBytes       int
	MaxFileBytes           int
	MaxSourceBytes         int64
	// AuthorizeSourceRoot is an optional live root proof for each file
	// read in an observation. Inventory and publication use AuthorizeRoot.
	AuthorizeSourceRoot func(context.Context) error
	VerifyAuthority     func(context.Context, model.CaptureManifest) error
	VerifyDependencies  func(context.Context, []string) error
}

// RevalidateLocal re-inventories an authorized local worktree, hashes every
// captured file through secure handles, then inventories again. It establishes
// observed stability only for this bounded interval, not an atomic filesystem
// snapshot. The coordinator must hold the worktree writer lease throughout.
func RevalidateLocal(ctx context.Context, request RevalidateRequest) error {
	if request.MatchManifestInventory {
		return ErrSnapshotUnverifiable
	}
	request.AuthorizeSourceRoot = nil
	return ObserveCapturedLocal(ctx, request)
}

// ObserveCapturedLocal performs only source observations. A query owner must
// pin and independently recheck its generation and authority at both ends. It
// does not acquire the index writer, publish a generation, or freeze the live
// filesystem; a concurrent watch may continue indexing while it runs.
func ObserveCapturedLocal(ctx context.Context, request RevalidateRequest) error {
	return revalidate(ctx, request, func(ctx context.Context) (inventory.FileMap, error) {
		return inventory.InventoryLocal(ctx, request.Inventory)
	})
}

// RevalidateLinked applies the same checks to a verified linked-worktree root.
func RevalidateLinked(ctx context.Context, request RevalidateRequest, binding *runtime.LinkedGitMetadata, common *paths.Root) error {
	if request.MatchManifestInventory {
		return ErrSnapshotUnverifiable
	}
	request.AuthorizeSourceRoot = nil
	return ObserveCapturedLinked(ctx, request, binding, common)
}

// ObserveCapturedLinked is the read-only observation for a verified linked root.
func ObserveCapturedLinked(ctx context.Context, request RevalidateRequest, binding *runtime.LinkedGitMetadata, common *paths.Root) error {
	if binding == nil || common == nil {
		return ErrSnapshotUnverifiable
	}
	return revalidate(ctx, request, func(ctx context.Context) (inventory.FileMap, error) {
		return inventory.InventoryLinked(ctx, request.Inventory, binding, common)
	})
}

func revalidate(ctx context.Context, request RevalidateRequest, observe func(context.Context) (inventory.FileMap, error)) error {
	if ctx == nil || request.Inventory.Root == nil || request.Inventory.Scanner == nil || request.Inventory.AuthorizeRoot == nil || request.Inventory.AuthorizePath == nil ||
		request.Snapshot == nil || request.VerifyAuthority == nil || request.VerifyDependencies == nil || observe == nil ||
		request.MaxManifestBytes < 1 || request.MaxManifestBytes > 64*1024*1024 || request.MaxFileBytes < 1 || request.MaxFileBytes > 64*1024*1024 || request.MaxSourceBytes < 1 {
		return ErrSnapshotUnverifiable
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrSnapshotUnverifiable
	}
	manifest := request.Snapshot.Manifest()
	if model.VerifyCaptureDigest(manifest, request.MaxManifestBytes) != nil {
		return ErrSnapshotUnverifiable
	}
	expected := copyFileMap(request.Expected)
	if !request.MatchManifestInventory && len(expected.Entries) != len(manifest.Files) {
		return ErrSnapshotChanged
	}
	if err := request.VerifyAuthority(ctx, request.Snapshot.Manifest()); err != nil {
		return err
	}
	if err := request.VerifyDependencies(ctx, append([]string{}, manifest.DependencyInputDigests...)); err != nil {
		return err
	}
	first, err := observe(ctx)
	if err != nil {
		return err
	}
	if request.MatchManifestInventory {
		if len(manifest.DependencyInputDigests) != 1 || first.InputDigest != manifest.DependencyInputDigests[0] || len(first.Entries) != len(manifest.Files) {
			return ErrSnapshotChanged
		}
		expected = first
	} else if !reflect.DeepEqual(first, expected) {
		return ErrSnapshotChanged
	}
	if err := verifyCapturedFiles(ctx, request, manifest, expected); err != nil {
		return err
	}
	if err := request.VerifyDependencies(ctx, append([]string{}, manifest.DependencyInputDigests...)); err != nil {
		return err
	}
	second, err := observe(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(second, expected) {
		return ErrSnapshotChanged
	}
	if err := verifyCapturedFiles(ctx, request, manifest, expected); err != nil {
		return err
	}
	last, err := observe(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(last, expected) {
		return ErrSnapshotChanged
	}
	if err := request.VerifyDependencies(ctx, append([]string{}, manifest.DependencyInputDigests...)); err != nil {
		return err
	}
	if err := request.VerifyAuthority(ctx, request.Snapshot.Manifest()); err != nil {
		return err
	}
	if err := request.Inventory.Root.Verify(); err != nil {
		return err
	}
	return ctx.Err()
}

func verifyCapturedFiles(ctx context.Context, request RevalidateRequest, manifest model.CaptureManifest, expected inventory.FileMap) error {
	source := request.Inventory
	if request.AuthorizeSourceRoot != nil {
		source.AuthorizeRoot = request.AuthorizeSourceRoot
	}
	var total int64
	for i, item := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := expected.Entries[i]
		if item.Path != entry.Path {
			return ErrSnapshotChanged
		}
		if entry.State == "candidate" && item.Status == "metadata_only" {
			return ErrSnapshotUnverifiable
		}
		if !compatibleCaptureStatus(entry.State, item.Status) {
			return ErrSnapshotChanged
		}
		if item.Status != "captured" {
			continue
		}
		if entry.Directory || item.ContentHash == nil || item.ByteLength == nil || *item.ByteLength < 0 || *item.ByteLength > int64(request.MaxFileBytes) || *item.ByteLength > request.MaxSourceBytes-total {
			return ErrSnapshotUnverifiable
		}
		total += *item.ByteLength
		authorize := func(ctx context.Context, name string) error { return authorizeSource(ctx, source, name) }
		current, err := inventory.CaptureFile(ctx, request.Inventory.Root, request.Inventory.Scanner, item.Path, request.MaxFileBytes, authorize)
		if err != nil {
			return err
		}
		if current.ContentHash() != *item.ContentHash || int64(current.Len()) != *item.ByteLength {
			return ErrSnapshotChanged
		}
	}
	return nil
}

func compatibleCaptureStatus(state, status string) bool {
	switch state {
	case "candidate":
		return status == "captured"
	case "missing":
		return status == "missing"
	case "unreadable":
		return status == "read_error"
	case "unmerged", "sparse", "boundary", "excluded", "type_changed":
		return status == "excluded" || status == "metadata_only"
	default:
		return false
	}
}

func authorizeSource(ctx context.Context, request inventory.LocalInventoryRequest, name string) error {
	if err := request.AuthorizeRoot(ctx); err != nil {
		return err
	}
	for start := 0; ; {
		next := strings.IndexByte(name[start:], '/')
		path, directory := name, false
		if next >= 0 {
			start += next
			path = name[:start]
			directory = true
		}
		allowed, err := request.AuthorizePath(ctx, path, directory)
		if err != nil {
			return err
		}
		if !allowed {
			return inventory.ErrWalkAuthorityChanged
		}
		if next < 0 {
			return nil
		}
		start++
	}
}

func copyFileMap(value inventory.FileMap) inventory.FileMap {
	copy := value
	copy.Entries = append([]inventory.FileMapEntry{}, value.Entries...)
	for i := range copy.Entries {
		copy.Entries[i].Stages = append([]uint8{}, value.Entries[i].Stages...)
	}
	return copy
}
