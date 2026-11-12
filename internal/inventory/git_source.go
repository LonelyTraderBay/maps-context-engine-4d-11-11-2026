package inventory

import (
	"bytes"
	"context"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var ErrIndexChanged = errors.New("Git index changed during inventory")

type GitIndexBudget struct {
	MaxBytes            int
	MaxEntries          int
	MaxPathBytes        int
	MaxDecodedPathBytes int
}

// IndexObservation is an authorized snapshot of Git index metadata only. A
// missing index is distinct from an empty committed index. Its paths remain
// untrusted until per-path policy and authorization have been applied.
type IndexObservation struct {
	Index        GitIndex
	IndexMissing bool
}

type indexReader interface {
	ReadIndexContext(context.Context, int) ([]byte, error)
	Verify() error
}

type localIndexReader struct{ worktree, metadata *paths.Root }

func (r localIndexReader) Verify() error {
	return errors.Join(r.worktree.Verify(), r.metadata.Verify())
}
func (r localIndexReader) ReadIndexContext(ctx context.Context, maxBytes int) ([]byte, error) {
	if err := r.Verify(); err != nil {
		return nil, err
	}
	data, err := runtime.ReadGitIndexContext(ctx, r.metadata, maxBytes)
	if err != nil {
		return nil, err
	}
	if err := r.Verify(); err != nil {
		return nil, err
	}
	return data, nil
}

// ReadLocalGitIndex only supports a real .git directory anchored below the
// authorized worktree. It does not execute Git or read source contents.
func ReadLocalGitIndex(ctx context.Context, worktree *paths.Root, objectHash string, budget GitIndexBudget, authorize func(context.Context) error) (observation IndexObservation, resultErr error) {
	if ctx == nil || worktree == nil || authorize == nil {
		return IndexObservation{}, runtime.ErrGitMetadataUnverified
	}
	if err := authorize(ctx); err != nil {
		return IndexObservation{}, err
	}
	if err := worktree.Verify(); err != nil {
		return IndexObservation{}, err
	}
	metadata, err := runtime.OpenLocalGitMetadata(worktree)
	if err != nil {
		return IndexObservation{}, errors.Join(runtime.ErrGitMetadataUnverified, err)
	}
	defer func() {
		if err := metadata.Close(); err != nil {
			observation = IndexObservation{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	return observeGitIndex(ctx, localIndexReader{worktree: worktree, metadata: metadata}, objectHash, budget, authorize)
}

// ReadLinkedGitIndex requires the reciprocal worktree/common metadata binding
// to have been established with explicitly authorized roots.
func ReadLinkedGitIndex(ctx context.Context, binding *runtime.LinkedGitMetadata, objectHash string, budget GitIndexBudget, authorize func(context.Context) error) (IndexObservation, error) {
	if ctx == nil || binding == nil || authorize == nil {
		return IndexObservation{}, runtime.ErrGitMetadataUnverified
	}
	if err := authorize(ctx); err != nil {
		return IndexObservation{}, err
	}
	return observeGitIndex(ctx, binding, objectHash, budget, authorize)
}

func observeGitIndex(ctx context.Context, reader indexReader, objectHash string, budget GitIndexBudget, authorize func(context.Context) error) (IndexObservation, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		return IndexObservation{}, ErrIndexBudget
	}
	if err := ctx.Err(); err != nil {
		return IndexObservation{}, err
	}
	if budget.MaxBytes < 1 || budget.MaxBytes > 64*1024*1024 {
		return IndexObservation{}, ErrIndexBudget
	}
	first, firstErr := reader.ReadIndexContext(ctx, budget.MaxBytes)
	missing := errors.Is(firstErr, os.ErrNotExist)
	if firstErr != nil && !missing {
		return IndexObservation{}, firstErr
	}
	var parsed GitIndex
	if !missing {
		var err error
		parsed, err = ParseGitIndex(first, objectHash, budget.MaxEntries, budget.MaxPathBytes, budget.MaxDecodedPathBytes)
		if err != nil {
			return IndexObservation{}, err
		}
	} else {
		// Validate budgets and object format even when the repository has no index.
		if budget.MaxEntries < 1 || budget.MaxEntries > 100000 || budget.MaxPathBytes < 1 || budget.MaxPathBytes > 65536 || budget.MaxDecodedPathBytes < 1 || budget.MaxDecodedPathBytes > 64*1024*1024 {
			return IndexObservation{}, ErrIndexBudget
		}
		if objectHash != "sha1" && objectHash != "sha256" {
			return IndexObservation{}, ErrIndexUnsupported
		}
		parsed.Entries = []IndexEntry{}
	}
	second, secondErr := reader.ReadIndexContext(ctx, budget.MaxBytes)
	if missing != errors.Is(secondErr, os.ErrNotExist) || !missing && secondErr != nil || !missing && !bytes.Equal(first, second) {
		return IndexObservation{}, errors.Join(ErrIndexChanged, secondErr)
	}
	if err := reader.Verify(); err != nil {
		return IndexObservation{}, err
	}
	if err := authorize(ctx); err != nil {
		return IndexObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return IndexObservation{}, err
	}
	return IndexObservation{Index: parsed, IndexMissing: missing}, nil
}
