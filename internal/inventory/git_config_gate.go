package inventory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

var ErrGitConfigChanged = errors.New("effective Git configuration changed")

type GitConfigSource struct {
	Root         *paths.Root
	RelativePath string
	Authorize    AuthorizeCapture
}

// GitConfigPlan names each applicable configuration location explicitly.
// Discovery and path-resolution callbacks must verify environment, actor,
// approved roots and repository identity; they must not follow config includes.
type GitConfigPlan struct {
	System              GitConfigSource
	XDG                 GitConfigSource
	Global              GitConfigSource
	Repository          GitConfigSource
	Worktree            *GitConfigSource
	VerifyDiscovery     func(context.Context) error
	ResolveGlobalIgnore func(context.Context, *string) (*ApprovedGlobalIgnore, error)
	ValidateWorktree    func(context.Context, GitConfigFields) error
}

type GitConfigBudget struct {
	MaxFileBytes   int
	MaxTotalBytes  int
	MaxOutputBytes int
}

type configObservation struct {
	source  GitConfigSource
	present bool
	digest  [32]byte
	length  int
}

// VerifiedGitConfig owns no root handles. Its caller must keep all supplied
// roots open and use Verify before each inventory/response boundary.
type VerifiedGitConfig struct {
	objectHash    string
	ignoreOptions IgnoreOptions
	global        ApprovedGlobalIgnore
	plan          GitConfigPlan
	budget        GitConfigBudget
	scanner       *policy.Scanner
	observations  []configObservation
	effective     GitConfigFields
}

func InspectGitIgnoreConfig(ctx context.Context, gitBinary string, plan GitConfigPlan, scanner *policy.Scanner, budget GitConfigBudget) (*VerifiedGitConfig, error) {
	if ctx == nil || scanner == nil || plan.VerifyDiscovery == nil || plan.ResolveGlobalIgnore == nil || plan.ValidateWorktree == nil || budget.MaxFileBytes < 1 || budget.MaxFileBytes > 4*1024*1024 || budget.MaxTotalBytes < 1 || budget.MaxTotalBytes > 4*1024*1024 || budget.MaxOutputBytes < 1 || budget.MaxOutputBytes > 8*1024*1024 {
		return nil, ErrGitConfigUnsupported
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrGitConfigUnsupported
	}
	if plan.Worktree != nil {
		copy := *plan.Worktree
		plan.Worktree = &copy
	}
	if err := plan.VerifyDiscovery(ctx); err != nil {
		return nil, err
	}
	sources := []GitConfigSource{plan.System, plan.XDG, plan.Global, plan.Repository}
	var observations []configObservation
	var effective GitConfigFields
	total := 0
	for i, source := range sources {
		observation, raw, err := captureConfigSource(ctx, source, scanner, budget.MaxFileBytes)
		if err != nil {
			return nil, err
		}
		if i == 3 && !observation.present {
			return nil, ErrGitConfigUnsupported
		}
		if len(raw) > budget.MaxTotalBytes-total {
			return nil, ErrIgnoreBudget
		}
		total += len(raw)
		observations = append(observations, observation)
		if !observation.present {
			continue
		}
		fields, err := ParseGitConfigFields(ctx, gitBinary, raw, scanner, budget.MaxFileBytes, budget.MaxOutputBytes, func(ctx context.Context) error { return source.Authorize(ctx, source.RelativePath) })
		if err != nil {
			return nil, err
		}
		if i != 3 && (fields.ObjectFormat != nil || fields.WorktreeConfig != nil) {
			return nil, ErrGitConfigUnsupported
		}
		mergeGitConfig(&effective, fields)
	}
	if effective.WorktreeConfig != nil && *effective.WorktreeConfig {
		if plan.Worktree == nil {
			return nil, ErrGitConfigUnsupported
		}
		source := *plan.Worktree
		observation, raw, err := captureConfigSource(ctx, source, scanner, budget.MaxFileBytes)
		if err != nil {
			return nil, err
		}
		if len(raw) > budget.MaxTotalBytes-total {
			return nil, ErrIgnoreBudget
		}
		observations = append(observations, observation)
		if observation.present {
			fields, err := ParseGitConfigFields(ctx, gitBinary, raw, scanner, budget.MaxFileBytes, budget.MaxOutputBytes, func(ctx context.Context) error { return source.Authorize(ctx, source.RelativePath) })
			if err != nil {
				return nil, err
			}
			if fields.ObjectFormat != nil || fields.WorktreeConfig != nil {
				return nil, ErrGitConfigUnsupported
			}
			mergeGitConfig(&effective, fields)
		}
	}
	if err := plan.ValidateWorktree(ctx, cloneGitConfigFields(effective)); err != nil {
		return nil, err
	}
	global, err := plan.ResolveGlobalIgnore(ctx, copyString(effective.ExcludesFile))
	if err != nil {
		return nil, err
	}
	if global == nil || global.Root == nil || global.Authorize == nil || global.RelativePath == "" {
		return nil, ErrGitConfigUnsupported
	}
	if err := plan.VerifyDiscovery(ctx); err != nil {
		return nil, err
	}
	objectHash := "sha1"
	if effective.ObjectFormat != nil {
		objectHash = *effective.ObjectFormat
	}
	options := IgnoreOptions{}
	if effective.IgnoreCase != nil {
		options.IgnoreCase = *effective.IgnoreCase
	}
	gate := &VerifiedGitConfig{objectHash: objectHash, ignoreOptions: options, global: *global, plan: plan, budget: budget, scanner: scanner, observations: observations, effective: effective}
	if err := gate.Verify(ctx); err != nil {
		return nil, err
	}
	return gate, nil
}

// Verify rechecks the same authorized files and resolution; no unrelated
// configuration values or raw config bytes are retained in the gate.
func (g *VerifiedGitConfig) Verify(ctx context.Context) error {
	if g == nil || ctx == nil || g.plan.VerifyDiscovery == nil {
		return ErrGitConfigUnsupported
	}
	if err := g.plan.VerifyDiscovery(ctx); err != nil {
		return err
	}
	for _, expected := range g.observations {
		observed, _, err := captureConfigSource(ctx, expected.source, g.scanner, g.budget.MaxFileBytes)
		if err != nil {
			return err
		}
		if observed.present != expected.present || observed.digest != expected.digest || observed.length != expected.length {
			return ErrGitConfigChanged
		}
	}
	if err := g.plan.ValidateWorktree(ctx, cloneGitConfigFields(g.effective)); err != nil {
		return err
	}
	global, err := g.resolveGlobal(ctx)
	if err != nil {
		return err
	}
	if err := global.Authorize(ctx, global.RelativePath); err != nil {
		return err
	}
	if err := g.plan.VerifyDiscovery(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

func (g *VerifiedGitConfig) resolveGlobal(ctx context.Context) (*ApprovedGlobalIgnore, error) {
	global, err := g.plan.ResolveGlobalIgnore(ctx, copyString(g.effective.ExcludesFile))
	if err != nil {
		return nil, err
	}
	if global == nil || global.Root != g.global.Root || global.RelativePath != g.global.RelativePath || global.Authorize == nil {
		return nil, ErrGitConfigChanged
	}
	return global, nil
}

func (g *VerifiedGitConfig) authorizeGlobal(ctx context.Context, name string) error {
	global, err := g.resolveGlobal(ctx)
	if err != nil {
		return err
	}
	if name != global.RelativePath {
		return ErrGitConfigChanged
	}
	return global.Authorize(ctx, name)
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneGitConfigFields(value GitConfigFields) GitConfigFields {
	return GitConfigFields{IgnoreCase: copyBool(value.IgnoreCase), ExcludesFile: copyString(value.ExcludesFile), ObjectFormat: copyString(value.ObjectFormat), WorktreeConfig: copyBool(value.WorktreeConfig), Worktree: copyString(value.Worktree), Bare: copyBool(value.Bare)}
}

func sameConfigRoot(source GitConfigSource, root *paths.Root, relative string) error {
	if root == nil || source.Root == nil || source.RelativePath != relative {
		return ErrGitConfigUnsupported
	}
	want, err := root.CanonicalPath()
	if err != nil {
		return err
	}
	got, err := source.Root.CanonicalPath()
	if err != nil {
		return err
	}
	if got != want {
		return ErrGitConfigUnsupported
	}
	return errors.Join(root.Verify(), source.Root.Verify())
}

func (g *VerifiedGitConfig) verifyLocalBinding(worktree *paths.Root) (resultErr error) {
	if g == nil || worktree == nil {
		return ErrGitConfigUnsupported
	}
	metadata, err := runtime.OpenLocalGitMetadata(worktree)
	if err != nil {
		return errors.Join(ErrGitConfigUnsupported, err)
	}
	defer func() { resultErr = errors.Join(resultErr, metadata.Close()) }()
	if err := sameConfigRoot(g.plan.Repository, metadata, "config"); err != nil {
		return err
	}
	if g.effective.WorktreeConfig != nil && *g.effective.WorktreeConfig {
		if g.plan.Worktree == nil {
			return ErrGitConfigUnsupported
		}
		if err := sameConfigRoot(*g.plan.Worktree, metadata, "config.worktree"); err != nil {
			return err
		}
	}
	return nil
}

func (g *VerifiedGitConfig) verifyLinkedBinding(binding *runtime.LinkedGitMetadata, common *paths.Root) error {
	if g == nil || binding == nil {
		return ErrGitConfigUnsupported
	}
	if err := sameConfigRoot(g.plan.Repository, common, "config"); err != nil {
		return err
	}
	if g.effective.WorktreeConfig != nil && *g.effective.WorktreeConfig {
		if g.plan.Worktree == nil || g.plan.Worktree.RelativePath != "config.worktree" {
			return ErrGitConfigUnsupported
		}
		if err := binding.VerifyMetadataRoot(g.plan.Worktree.Root); err != nil {
			return err
		}
	}
	return nil
}

func mergeGitConfig(target *GitConfigFields, source GitConfigFields) {
	if source.IgnoreCase != nil {
		target.IgnoreCase = source.IgnoreCase
	}
	if source.ExcludesFile != nil {
		target.ExcludesFile = source.ExcludesFile
	}
	if source.ObjectFormat != nil {
		target.ObjectFormat = source.ObjectFormat
	}
	if source.WorktreeConfig != nil {
		target.WorktreeConfig = source.WorktreeConfig
	}
	if source.Worktree != nil {
		target.Worktree = source.Worktree
	}
	if source.Bare != nil {
		target.Bare = source.Bare
	}
}

func captureConfigSource(ctx context.Context, source GitConfigSource, scanner *policy.Scanner, maxBytes int) (configObservation, []byte, error) {
	observation := configObservation{source: source}
	if source.Root == nil || source.Authorize == nil || source.RelativePath == "" {
		return observation, nil, ErrGitConfigUnsupported
	}
	file, err := CaptureFile(ctx, source.Root, scanner, source.RelativePath, maxBytes, source.Authorize)
	if errors.Is(err, os.ErrNotExist) {
		if err := source.Authorize(ctx, source.RelativePath); err != nil {
			return observation, nil, err
		}
		current, checkErr := source.Root.OpenRegular(source.RelativePath)
		if checkErr == nil {
			return observation, nil, errors.Join(ErrGitConfigChanged, current.Close())
		}
		if !errors.Is(checkErr, os.ErrNotExist) {
			return observation, nil, errors.Join(ErrGitConfigChanged, checkErr)
		}
		if err := source.Root.Verify(); err != nil {
			return observation, nil, err
		}
		return observation, nil, ctx.Err()
	}
	if err != nil {
		return observation, nil, err
	}
	raw := file.Bytes()
	second, err := CaptureFile(ctx, source.Root, scanner, source.RelativePath, maxBytes, source.Authorize)
	if err != nil {
		return observation, nil, errors.Join(ErrGitConfigChanged, err)
	}
	if !bytes.Equal(raw, second.Bytes()) {
		return observation, nil, ErrGitConfigChanged
	}
	observation.present = true
	observation.length = len(raw)
	observation.digest = sha256.Sum256(raw)
	return observation, raw, nil
}
