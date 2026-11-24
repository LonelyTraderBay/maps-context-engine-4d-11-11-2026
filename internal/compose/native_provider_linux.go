package compose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	pythonprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/python-provider"
	tsprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/ts-provider"
	"golang.org/x/sys/unix"
)

const nativeProviderFrame = 1 << 20

type nativeLanguageProvider struct {
	id, executable, digest string
	worker                 installedLanguageProvider
}

type installedLanguageProvider interface {
	ports.LanguageProvider
	VerifyInstallation(context.Context) error
}

func nativeTypeScriptInstallation(installation, scratch string) tsprovider.Installation {
	base := filepath.Join(installation, "providers", "typescript")
	return tsprovider.Installation{Node: filepath.Join(base, "node"), ScratchParent: scratch,
		CompilerRoot: filepath.Join(base, "compiler"), CompilerArchive: filepath.Join(base, "typescript-5.9.3.tgz"),
		CompilerManifest: filepath.Join(base, "compiler-files.json")}
}

func openTypeScriptProvider(ctx context.Context, installation string, profile *model.TSProfile, scratch string) (*providers.NodeProvider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	materials := nativeTypeScriptInstallation(installation, scratch)
	for dir := filepath.Dir(materials.Node); ; dir = filepath.Dir(dir) {
		var st unix.Stat_t
		if unix.Lstat(dir, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&unix.S_IFMT != unix.S_IFDIR {
			return nil, app.ErrCoreUnsupported
		}
		if dir == "/" {
			break
		}
	}
	// Installation files have to stay under the administrative identity
	// between fingerprint verification and loading the pinned compiler.
	entries := 0
	err := filepath.WalkDir(filepath.Dir(materials.Node), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		var st unix.Stat_t
		if entries > 512 || unix.Lstat(path, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 ||
			!entry.IsDir() && !entry.Type().IsRegular() {
			return app.ErrCoreUnsupported
		}
		return nil
	})
	if err != nil {
		return nil, errors.Join(app.ErrCoreUnsupported, err)
	}
	provider, installed, err := tsprovider.New(materials, profile.TsconfigPaths, profile.AnalysisMode)
	if err != nil || profile.NodeDigest != installed.NodeDigest || profile.CompilerDigest != installed.CompilerDigest || profile.ProviderDigest != installed.ProviderDigest {
		return nil, app.ErrCoreUnsupported
	}
	return provider, nil
}

func openNativeAddonProvider(ctx context.Context, installation, scratch string, profile model.EffectiveProfile) (*nativeLanguageProvider, error) {
	switch p := profile.(type) {
	case *model.TSProfile:
		worker, err := openTypeScriptProvider(ctx, installation, p, scratch)
		if err != nil {
			return nil, err
		}
		return &nativeLanguageProvider{id: tsprovider.ProviderID, executable: nativeTypeScriptInstallation(installation, scratch).Node, digest: p.NodeDigest, worker: worker}, nil
	case *model.PythonProfile:
		materials := pythonprovider.Installation{Interpreter: "/usr/bin/python3.14", Stdlib: "/usr/lib/python3.14", ScratchParent: scratch}
		var worker *providers.PythonProvider
		var installed *model.PythonProfile
		var err error
		switch p.AnalysisMode {
		case "syntax":
			worker, installed, err = pythonprovider.New(materials, p.ModuleRoots)
		case "typed_when_available":
			worker, installed, err = pythonprovider.NewTyped(materials, p.ModuleRoots)
		default:
			return nil, app.ErrCoreUnsupported
		}
		if err != nil || installed.PythonVersion != p.PythonVersion || installed.InterpreterDigest != p.InterpreterDigest || installed.StdlibDigest != p.StdlibDigest || installed.ProviderDigest != p.ProviderDigest {
			return nil, app.ErrCoreUnsupported
		}
		return &nativeLanguageProvider{id: pythonprovider.ProviderID, executable: materials.Interpreter, digest: p.InterpreterDigest, worker: worker}, nil
	default:
		return nil, app.ErrCoreUnsupported
	}
}

func (n *nativeScope) languageProvider(ctx context.Context) (*nativeLanguageProvider, error) {
	if n.binding.Profile != nil {
		return nil, nil
	}
	root, _, _, err := n.stores.CacheResources(ctx)
	if err != nil {
		return nil, err
	}
	state, err := root.CanonicalPath()
	if err != nil {
		return nil, err
	}
	provider, err := openNativeAddonProvider(ctx, n.installation, filepath.Join(state, "scratch"), n.binding.AddonProfile)
	if err != nil {
		return nil, err
	}
	for _, executable := range n.binding.Policy.AllowedExecutables {
		if executable.Path == provider.executable && executable.SHA256 == provider.digest {
			return provider, nil
		}
	}
	return nil, app.ErrCoreUnsupported
}

func (n *nativeScope) capturePath(name string) bool {
	if ts, ok := n.binding.AddonProfile.(*model.TSProfile); ok {
		for _, config := range ts.TsconfigPaths {
			if config == name {
				return true
			}
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".ts", ".tsx", ".cts", ".mts", ".js", ".jsx", ".cjs", ".mjs", ".json":
			return true
		default:
			return false
		}
	}
	if _, python := n.binding.AddonProfile.(*model.PythonProfile); python {
		for _, component := range strings.Split(name, "/") {
			switch strings.ToLower(component) {
			case ".venv", "venv", "site-packages", "__pycache__", "setup.py", "sitecustomize.py", "usercustomize.py":
				return false
			}
		}
		return strings.EqualFold(filepath.Ext(name), ".py")
	}
	return nativeTextPath(name)
}

func (n *nativeScope) stageLanguageProvider(ctx context.Context, store *index.Store, write index.CaptureWrite, stream index.ProviderStageStream, provider *nativeLanguageProvider) (_ model.ProviderDone, resultErr error) {
	manifest := write.Snapshot.Manifest()
	raw, err := json.Marshal(manifest)
	if err != nil || len(raw) > 16<<20 {
		return model.ProviderDone{}, app.ErrCoreLimit
	}
	needed := int64(len(raw))
	count := 0
	for _, file := range manifest.Files {
		if file.Status == "captured" {
			count++
			needed += *file.ByteLength
		}
	}
	if count > 1024 || needed > 64<<20 {
		return model.ProviderDone{}, app.ErrCoreLimit
	}
	state, _, protected, err := n.stores.CacheResources(ctx)
	if err != nil {
		return model.ProviderDone{}, err
	}
	parent, err := state.OpenDirectory("scratch")
	if err != nil {
		return model.ProviderDone{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	if err := errors.Join(privateNativeDirectory(parent), runtime.CheckCleanupRoot(parent, protected)); err != nil {
		return model.ProviderDone{}, err
	}
	reservation, err := n.stores.ReserveCache(ctx, needed)
	if err != nil {
		return model.ProviderDone{}, errors.Join(app.ErrCoreLimit, err)
	}
	defer func() {
		if reservation != nil {
			resultErr = errors.Join(resultErr, reservation.Close())
		}
	}()
	name := "provider-" + write.GenerationID
	fd, err := parent.DirectoryHandle()
	if err != nil {
		return model.ProviderDone{}, err
	}
	if err := errors.Join(unix.Mkdirat(int(fd.Fd()), name, 0700), fd.Close()); err != nil {
		return model.ProviderDone{}, err
	}
	root, err := parent.OpenDirectory(name)
	if err != nil {
		return model.ProviderDone{}, err
	}
	check := func(ctx context.Context) error {
		return errors.Join(n.recheckBinding(ctx), privateNativeDirectory(root), runtime.CheckCleanupRoot(root, protected))
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, removeNativeJobScratch(cleanup, root, parent, name, func() error {
			return errors.Join(n.stores.Check(cleanup), privateNativeDirectory(root), runtime.CheckCleanupRoot(root, protected))
		}), root.Close())
	}()
	if err := check(ctx); err != nil {
		return model.ProviderDone{}, err
	}
	path, err := root.CanonicalPath()
	if err != nil {
		return model.ProviderDone{}, err
	}
	output, err := os.OpenRoot(path)
	if err != nil {
		return model.ProviderDone{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, output.Close()) }()
	save := func(file string, body []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(file)
		if err := output.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return err
		}
		f, err := output.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(body)
		return errors.Join(err, f.Close())
	}
	for _, member := range manifest.Files {
		if file, ok := write.Snapshot.File(member.Path); ok {
			if err := save(member.Path, file.Bytes()); err != nil {
				return model.ProviderDone{}, err
			}
		}
	}
	manifestName := "mce4d-provider-" + write.GenerationID + ".json"
	if err := save(manifestName, raw); err != nil {
		return model.ProviderDone{}, err
	}
	// SQLite staging takes the same cache growth gate. Release the temporary
	// reservation after checking actual usage, before admitting batches.
	if err := reservation.Check(ctx); err != nil {
		return model.ProviderDone{}, err
	}
	err = reservation.Close()
	reservation = nil
	if err != nil {
		return model.ProviderDone{}, err
	}
	deadline, _ := ctx.Deadline()
	job := ports.ProviderJob{Capture: providers.SnapshotInput(write.Snapshot), Recheck: check, Start: model.ProviderStart{
		ProviderProtocol: 1, Type: "analyze_start", ProviderID: provider.id, ProviderDigest: manifest.ProviderDigest,
		JobID: write.JobID, GenerationID: write.GenerationID, ProfileID: manifest.ProfileID, Profile: n.binding.AddonProfile,
		SnapshotRoot: path, ManifestPath: filepath.Join(path, manifestName), ManifestDigest: fmt.Sprintf("%x", sha256.Sum256(raw)),
		MaxFrameBytes: nativeProviderFrame, DeadlineAt: deadline.UTC().Format(time.RFC3339Nano),
	}}
	done, err := provider.worker.Analyze(ctx, job, func(ctx context.Context, batch model.ProviderBatch) error {
		_, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: write, Stream: stream, Batch: batch, MaxFrameBytes: nativeProviderFrame})
		return err
	})
	switch {
	case errors.Is(err, providers.ErrAuthority):
		return model.ProviderDone{}, errors.Join(app.ErrCoreUnsupported, err)
	case errors.Is(err, providers.ErrBudget):
		return model.ProviderDone{}, errors.Join(app.ErrCoreLimit, err)
	default:
		return done, err
	}
}
