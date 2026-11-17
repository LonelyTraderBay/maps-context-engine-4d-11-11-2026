package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type ApprovedGlobalIgnore struct {
	Root         *paths.Root
	RelativePath string
	Authorize    AuthorizeCapture
}

// LocalInventoryRequest requires a verified, explicitly authorized config
// snapshot. The gate must be bound to the actual repository metadata.
type LocalInventoryRequest struct {
	Root                *paths.Root
	Config              *VerifiedGitConfig
	IndexBudget         GitIndexBudget
	WalkBudget          WalkBudget
	IgnoreCaptureBudget IgnoreCaptureBudget
	IgnoreBudget        IgnoreBudget
	MaxFileMapEntries   int
	Scanner             *policy.Scanner
	Sandbox             *paths.Root
	GitBinary           string
	AuthorizeRoot       func(context.Context) error
	AuthorizePath       WalkAuthorization
	AuthorizeMetadata   AuthorizeCapture
	AuthorizeSandbox    func(context.Context) error
	Exclude             WalkExclusion
	objectHash          string
	ignoreOptions       IgnoreOptions
	global              *ApprovedGlobalIgnore
}

type localInventoryObservation struct {
	index   IndexObservation
	walk    CandidateWalk
	sources []IgnoreSource
	ignored []bool
	files   FileMap
}

type inventoryBinding struct {
	readIndex    func(context.Context) (IndexObservation, error)
	openMetadata func() (*paths.Root, func() error, func() error, error)
	verify       func() error
}

// InventoryLocal performs two bounded observations and returns only when
// index, candidates, ignore inputs and the resulting map match. It is not an
// atomic filesystem snapshot: the caller must capture bytes and revalidate
// full membership and policy again before publication or edit preparation.
// Linked worktrees use a separate binding and are not accepted by this entry.
func InventoryLocal(ctx context.Context, request LocalInventoryRequest) (FileMap, error) {
	if err := request.prepareConfig(ctx); err != nil {
		return FileMap{}, err
	}
	binding := inventoryBinding{
		readIndex: func(ctx context.Context) (IndexObservation, error) {
			return ReadLocalGitIndex(ctx, request.Root, request.objectHash, request.IndexBudget, request.AuthorizeRoot)
		},
		openMetadata: func() (*paths.Root, func() error, func() error, error) {
			metadata, err := runtime.OpenLocalGitMetadata(request.Root)
			if err != nil {
				return nil, nil, nil, errors.Join(runtime.ErrGitMetadataUnverified, err)
			}
			return metadata, metadata.Close, metadata.Verify, nil
		},
		verify: func() error {
			return errors.Join(request.Root.Verify(), request.Config.verifyLocalBinding(request.Root))
		},
	}
	return stableInventory(ctx, request, binding)
}

// InventoryLinked requires the same worktree/common roots used to establish a
// reciprocal linked-worktree metadata binding. The common root is borrowed.
func InventoryLinked(ctx context.Context, request LocalInventoryRequest, binding *runtime.LinkedGitMetadata, common *paths.Root) (FileMap, error) {
	if binding == nil || common == nil || request.Root == nil {
		return FileMap{}, runtime.ErrGitMetadataUnverified
	}
	if err := request.prepareConfig(ctx); err != nil {
		return FileMap{}, err
	}
	verify := func() error {
		return errors.Join(binding.VerifyRoots(request.Root, common), request.Config.verifyLinkedBinding(binding, common))
	}
	source := inventoryBinding{
		readIndex: func(ctx context.Context) (IndexObservation, error) {
			return ReadLinkedGitIndex(ctx, binding, request.objectHash, request.IndexBudget, request.AuthorizeRoot)
		},
		openMetadata: func() (*paths.Root, func() error, func() error, error) {
			if err := verify(); err != nil {
				return nil, nil, nil, err
			}
			return common, func() error { return nil }, verify, nil
		},
		verify: verify,
	}
	return stableInventory(ctx, request, source)
}

func (request *LocalInventoryRequest) prepareConfig(ctx context.Context) error {
	if request.Config == nil {
		return ErrGitConfigUnsupported
	}
	if err := request.Config.Verify(ctx); err != nil {
		return err
	}
	request.objectHash = request.Config.objectHash
	request.ignoreOptions = request.Config.ignoreOptions
	global := request.Config.global
	global.Authorize = request.Config.authorizeGlobal
	request.global = &global
	return nil
}

func stableInventory(ctx context.Context, request LocalInventoryRequest, binding inventoryBinding) (FileMap, error) {
	if ctx == nil || request.Root == nil || request.Scanner == nil || request.Sandbox == nil || request.Config == nil || request.AuthorizeRoot == nil || request.AuthorizePath == nil || request.AuthorizeMetadata == nil || request.AuthorizeSandbox == nil || request.Exclude == nil {
		return FileMap{}, ErrWalkBudget
	}
	if _, ok := ctx.Deadline(); !ok {
		return FileMap{}, ErrWalkBudget
	}
	if request.global == nil || request.global.Root == nil || request.global.Authorize == nil {
		return FileMap{}, ErrIgnoreUnsupported
	}
	if err := request.AuthorizeRoot(ctx); err != nil {
		return FileMap{}, err
	}
	if err := binding.verify(); err != nil {
		return FileMap{}, err
	}
	first, err := observeInventory(ctx, request, binding)
	if err != nil {
		return FileMap{}, err
	}
	second, err := observeInventory(ctx, request, binding)
	if err != nil {
		return FileMap{}, err
	}
	if !reflect.DeepEqual(first, second) {
		return FileMap{}, ErrWalkChanged
	}
	if err := request.Config.Verify(ctx); err != nil {
		return FileMap{}, err
	}
	if err := request.AuthorizeRoot(ctx); err != nil {
		return FileMap{}, err
	}
	if err := request.Root.Verify(); err != nil {
		return FileMap{}, err
	}
	if err := binding.verify(); err != nil {
		return FileMap{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileMap{}, err
	}
	return second.files, nil
}

func observeInventory(ctx context.Context, request LocalInventoryRequest, binding inventoryBinding) (result localInventoryObservation, resultErr error) {
	if err := request.Config.Verify(ctx); err != nil {
		return result, err
	}
	index, err := binding.readIndex(ctx)
	if err != nil {
		return result, err
	}
	exclude, err := ExcludeIndexBoundaries(index.Index, request.Exclude)
	if err != nil {
		return result, err
	}
	walk, err := WalkCandidates(ctx, request.Root, request.WalkBudget, request.AuthorizeRoot, request.AuthorizePath, exclude)
	if err != nil {
		return result, err
	}
	authorizeSource := func(ctx context.Context, name string) error {
		allowed, err := authorizeMapPath(ctx, name, false, request.AuthorizePath)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrWalkAuthorityChanged
		}
		return nil
	}
	sources, err := CaptureDirectoryIgnoreSources(ctx, request.Root, walk, request.Scanner, request.IgnoreCaptureBudget, authorizeSource)
	if err != nil {
		return result, err
	}
	metadata, closeMetadata, verifyMetadata, err := binding.openMetadata()
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, closeMetadata()) }()
	info, err := CaptureInfoExclude(ctx, metadata, request.Scanner, request.IgnoreCaptureBudget.MaxFileBytes, request.AuthorizeMetadata)
	if err != nil {
		return result, err
	}
	if info != nil {
		sources = append(sources, *info)
	}
	if request.global != nil {
		global, err := CaptureApprovedGlobalIgnore(ctx, request.global.Root, request.global.RelativePath, request.Scanner, request.IgnoreCaptureBudget.MaxFileBytes, request.global.Authorize)
		if err != nil {
			return result, err
		}
		if global != nil {
			sources = append(sources, *global)
		}
	}
	queries := make([]IgnoreQuery, len(walk.Candidates))
	for i, item := range walk.Candidates {
		queries[i] = IgnoreQuery{Path: item.Path, IsDirectory: item.Directory}
	}
	ignored, err := EvaluateIgnoreRules(ctx, request.Sandbox, request.GitBinary, sources, queries, request.Scanner, request.ignoreOptions, request.IgnoreBudget, request.AuthorizeSandbox)
	if err != nil {
		return result, err
	}
	files, err := BuildFileMap(ctx, request.Root, index, walk, ignored, request.MaxFileMapEntries, request.AuthorizeRoot, request.AuthorizePath)
	if err != nil {
		return result, err
	}
	if err := request.Config.Verify(ctx); err != nil {
		return result, err
	}
	if err := verifyMetadata(); err != nil {
		return result, err
	}
	// Bind input changes even when they happen to produce the same membership
	// (for example, an ignore comment or unrelated Git-index entry flags).
	type configInput struct {
		Root, Path string
		Present    bool
		Digest     [32]byte
		Length     int
	}
	inputs := make([]configInput, 0, len(request.Config.observations))
	for _, observed := range request.Config.observations {
		root, err := observed.source.Root.CanonicalPath()
		if err != nil {
			return result, err
		}
		inputs = append(inputs, configInput{root, observed.source.RelativePath, observed.present, observed.digest, observed.length})
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("MCE4D-INVENTORY-INPUTS1\x00"))
	if err := json.NewEncoder(hash).Encode([]any{inputs, index, sources, files}); err != nil {
		return result, err
	}
	files.InputDigest = hex.EncodeToString(hash.Sum(nil))
	return localInventoryObservation{index: index, walk: walk, sources: sources, ignored: ignored, files: files}, nil
}
