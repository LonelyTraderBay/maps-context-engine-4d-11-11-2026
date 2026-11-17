//go:build linux

package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

type nativeInventory struct {
	scope                                   *nativeScope
	request                                 inventory.LocalInventoryRequest
	home, system, sandbox, worktreeMetadata *paths.Root
	linked                                  *runtime.LinkedGitMetadata
}

func (n *nativeScope) approvedGit(ctx context.Context) (string, error) {
	var selected string
	for _, executable := range n.binding.Policy.AllowedExecutables {
		if filepath.Base(executable.Path) != "git" || !filepath.IsAbs(executable.Path) || filepath.Clean(executable.Path) != executable.Path {
			continue
		}
		if selected != "" {
			return "", app.ErrCoreUnsupported
		}
		// A hash alone cannot close a path-replacement race. The whole binary
		// path must be controlled by the trusted administrator as well.
		for path := executable.Path; ; path = filepath.Dir(path) {
			var st unix.Stat_t
			if unix.Lstat(path, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&unix.S_IFMT == unix.S_IFLNK {
				return "", ErrAuthority
			}
			if path == "/" {
				break
			}
		}
		root, err := paths.OpenRoot(filepath.Dir(executable.Path))
		if err != nil {
			return "", err
		}
		file, err := root.OpenRegular(filepath.Base(executable.Path))
		if err != nil {
			_ = root.Close()
			return "", err
		}
		hash := sha256.New()
		count, readErr := io.Copy(hash, io.LimitReader(file, (64<<20)+1))
		err = errors.Join(readErr, file.Close(), root.Close(), ctx.Err())
		if err != nil || count > 64<<20 || hex.EncodeToString(hash.Sum(nil)) != executable.SHA256 {
			return "", ErrAuthority
		}
		selected = executable.Path
	}
	if selected == "" {
		return "", app.ErrCoreUnsupported
	}
	return selected, nil
}

func openNativeInventory(ctx context.Context, n *nativeScope, create bool) (_ *nativeInventory, resultErr error) {
	if n.binding.Profile.AnalysisMode != "syntax" && n.binding.Profile.AnalysisMode != "typed_when_available" || len(n.binding.Profile.DependencyRoots) != 0 || len(n.binding.Policy.DependencyRoots) != 0 {
		return nil, app.ErrCoreUnsupported
	}
	git, err := n.approvedGit(ctx)
	if err != nil {
		return nil, err
	}
	i := &nativeInventory{scope: n}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, i.Close())
		}
	}()
	root, _, _, err := n.stores.CacheResources(ctx)
	if err != nil {
		return nil, err
	}
	i.sandbox, err = root.OpenDirectory("scratch/inventory")
	if errors.Is(err, os.ErrNotExist) && create {
		reservation, err := n.stores.ReserveCache(ctx, 64<<10)
		if err != nil {
			return nil, err
		}
		makeErr := func() (resultErr error) {
			defer func() { resultErr = errors.Join(resultErr, reservation.Close()) }()
			parent, err := root.OpenDirectory("scratch")
			if err != nil {
				return err
			}
			defer parent.Close()
			fd, err := parent.DirectoryHandle()
			if err != nil {
				return err
			}
			defer fd.Close()
			if err := unix.Mkdirat(int(fd.Fd()), "inventory", 0700); err != nil {
				return err
			}
			i.sandbox, err = root.OpenDirectory("scratch/inventory")
			if err != nil {
				return err
			}
			return inventory.PrepareIgnoreSandbox(ctx, i.sandbox, n.stores.Check)
		}()
		if makeErr != nil {
			return nil, makeErr
		}
	} else if err != nil {
		return nil, err
	}
	i.home, err = paths.OpenRoot(filepath.Join(n.installation, "home"))
	if err != nil {
		return nil, err
	}
	i.system, err = paths.OpenRoot("/etc")
	if err != nil {
		return nil, err
	}
	metadata := n.common
	if n.linked != "" {
		i.linked, err = runtime.BindLinkedGitMetadata(n.target, n.common, n.linked)
		if err != nil {
			return nil, err
		}
		i.worktreeMetadata, err = n.common.OpenDirectory(n.linked)
		if err != nil {
			return nil, err
		}
		metadata = i.worktreeMetadata
	}
	authorize := func(ctx context.Context, _ string) error { return n.boundary(ctx) }
	source := func(root *paths.Root, name string) inventory.GitConfigSource {
		return inventory.GitConfigSource{Root: root, RelativePath: name, Authorize: authorize}
	}
	scanner, err := policy.NewScanner(64 << 20)
	if err != nil {
		return nil, err
	}
	plan := inventory.GitConfigPlan{System: source(i.system, "gitconfig"), XDG: source(i.home, ".config/git/config"), Global: source(i.home, ".gitconfig"), Repository: source(n.common, "config"), Worktree: func() *inventory.GitConfigSource { v := source(metadata, "config.worktree"); return &v }(),
		VerifyDiscovery: func(ctx context.Context) error {
			return errors.Join(n.recheckBinding(ctx), i.home.Verify(), i.system.Verify())
		},
		ResolveGlobalIgnore: func(ctx context.Context, value *string) (*inventory.ApprovedGlobalIgnore, error) {
			if value != nil && *value != ".config/git/ignore" && *value != filepath.Join(n.installation, "home/.config/git/ignore") {
				return nil, inventory.ErrGitConfigUnsupported
			}
			if err := n.boundary(ctx); err != nil {
				return nil, err
			}
			return &inventory.ApprovedGlobalIgnore{Root: i.home, RelativePath: ".config/git/ignore", Authorize: authorize}, nil
		}, ValidateWorktree: func(ctx context.Context, fields inventory.GitConfigFields) error {
			if fields.Worktree != nil || fields.Bare != nil && *fields.Bare {
				return inventory.ErrGitConfigUnsupported
			}
			return n.boundary(ctx)
		}}
	config, err := inventory.InspectGitIgnoreConfig(ctx, git, plan, scanner, inventory.GitConfigBudget{MaxFileBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxOutputBytes: 4 << 20})
	if err != nil {
		return nil, err
	}
	count := int(minInt64(n.effective.Core.MaxIndexedFiles, 100000))
	i.request = inventory.LocalInventoryRequest{Root: n.target, Config: config, Scanner: scanner, Sandbox: i.sandbox, GitBinary: git,
		IndexBudget:         inventory.GitIndexBudget{MaxBytes: 64 << 20, MaxEntries: count, MaxPathBytes: 4096, MaxDecodedPathBytes: 64 << 20},
		WalkBudget:          inventory.WalkBudget{MaxVisited: min(count*4, 100000), MaxDirectories: min(count, 100000), MaxDepth: 64, MaxPathBytes: 4096, MaxTotalPathBytes: 64 << 20},
		IgnoreCaptureBudget: inventory.IgnoreCaptureBudget{MaxFiles: 4096, MaxFileBytes: 1 << 20, MaxTotalBytes: 4 << 20},
		IgnoreBudget:        inventory.IgnoreBudget{MaxSources: 4096, MaxRules: 100000, MaxRuleBytes: 4 << 20, MaxQueries: min(count*4, 100000), MaxQueryBytes: 64 << 20, MaxOutputBytes: 64 << 20},
		MaxFileMapEntries:   count, AuthorizeRoot: n.recheckBinding, AuthorizeMetadata: authorize, AuthorizeSandbox: n.stores.Check,
		AuthorizePath: func(ctx context.Context, name string, directory bool) (bool, error) {
			if err := n.boundary(ctx); err != nil {
				return false, err
			}
			return policy.CheckConfiguredPath(strings.TrimSuffix(name, "/"), n.binding.Policy.DeniedGlobs) == nil, nil
		},
		Exclude: func(name string, directory bool) bool {
			return policy.CheckConfiguredPath(strings.TrimSuffix(name, "/"), n.binding.Policy.DeniedGlobs) != nil || !directory && !nativeTextPath(name)
		},
	}
	return i, nil
}

func nativeTextPath(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".mod", ".sum", ".md", ".txt", ".json", ".yaml", ".yml", ".toml", ".xml", ".html", ".css", ".js", ".jsx", ".ts", ".tsx", ".sql", ".sh":
		return true
	}
	switch strings.ToLower(filepath.Base(name)) {
	case "readme", "license", "makefile", "dockerfile", ".gitignore", ".gitattributes", ".editorconfig":
		return true
	}
	return false
}

func (i *nativeInventory) Observe(ctx context.Context) (inventory.FileMap, error) {
	if i.linked != nil {
		return inventory.InventoryLinked(ctx, i.request, i.linked, i.scope.common)
	}
	return inventory.InventoryLocal(ctx, i.request)
}

func (i *nativeInventory) Close() error {
	var err error
	if i.linked != nil {
		err = errors.Join(err, i.linked.Close())
	}
	for _, root := range []*paths.Root{i.worktreeMetadata, i.home, i.system, i.sandbox} {
		if root != nil {
			err = errors.Join(err, root.Close())
		}
	}
	return err
}
