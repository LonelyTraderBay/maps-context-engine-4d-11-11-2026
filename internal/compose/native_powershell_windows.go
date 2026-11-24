package compose

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	powershellprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/powershell-provider"
)

type windowsPowerShellFacts struct {
	result []powershellprovider.FileResult
}

func (c windowsIndexCapture) analyzePowerShell(ctx context.Context, installation string, capture *snapshot.Snapshot) (*windowsPowerShellFacts, error) {
	executable, approved := nativeRenameExecutable(c.scope.Binding.Policy, "pwsh.exe")
	if !approved || executable.SHA256 != c.powershell.ExecutableDigest ||
		executable.Path != filepath.Join(installation, "control", "powershell", "pwsh.exe") || c.scope.OpenRuntime == nil {
		return nil, app.ErrCoreUnsupported
	}
	home := filepath.Dir(executable.Path)
	binding := c.scope.Binding
	if windowsPathsOverlap(home, binding.CommonDirectory) {
		return nil, app.ErrCoreAuthentication
	}
	for _, target := range binding.Policy.AllowedRoots {
		if windowsPathsOverlap(home, target) {
			return nil, app.ErrCoreAuthentication
		}
	}
	root, err := c.scope.OpenRuntime(ctx, "powershell")
	if err != nil {
		return nil, errors.Join(app.ErrCoreUnsupported, err)
	}
	rows, err := powershellprovider.Analyze(ctx, root, c.powershell, capture, c.scope.Check)
	err = errors.Join(err, root.Close())
	if errors.Is(err, powershellprovider.ErrInstallation) {
		return nil, errors.Join(app.ErrCoreUnsupported, err)
	}
	if err != nil {
		return nil, err
	}
	return &windowsPowerShellFacts{result: rows}, nil
}

func (f *windowsPowerShellFacts) stage(ctx context.Context, store *index.Store, write index.CaptureWrite, stream index.ProviderStageStream,
	done model.ProviderDone) (model.ProviderDone, map[string]string, error) {
	const maxFrameBytes = 1 << 20
	analyzed := make(map[string]string, len(f.result))
	done.Sequence = 1
	done.Capabilities = []model.Capability{{Name: "powershell_syntax", ProviderID: powershellprovider.ID, ProviderVersion: "1", Status: "partial"}}
	done.Gaps = []model.Gap{{Code: "POWERSHELL_DECLARATIONS_ONLY", Reason: "Only top-level function and filter declarations are supported.", SourceRefs: []model.SourceRef{}}}
	for _, row := range f.result {
		path := row.Path
		analyzed[path] = row.ContentHash
		batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: write.JobID, Sequence: done.Sequence,
			Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{}, Relations: []model.RelationFact{}, Diagnostics: []model.DiagnosticFact{}}
		scope := fmt.Sprintf("psscope-%x", sha256.Sum256([]byte(path)))
		unit := fmt.Sprintf("psscript-%x", sha256.Sum256([]byte(path)))
		if len(row.Declarations) != 0 {
			// Each script is its own captured syntax unit. A unit does not
			// infer module membership, exports, imports or invocation edges.
			batch.Packages = append(batch.Packages, model.PackageFact{PackageKey: unit, ImportPath: path,
				ModuleKey: scope, Variant: "other_supported", InputDigest: done.InputDigest, SemanticStatus: "partial"})
		}
		for _, declaration := range row.Declarations {
			key := fmt.Sprintf("%s\x00%d\x00%s", path, declaration.Start, declaration.Name)
			span := model.ByteRange{StartByte: declaration.Start, EndByte: declaration.End}
			batch.Symbols = append(batch.Symbols, model.SymbolFact{
				SymbolID: fmt.Sprintf("pssymbol-%x", sha256.Sum256([]byte(key))), Path: &path, PackageKey: &unit,
				Kind: "function", Name: declaration.Name, QualifiedName: path + "#" + declaration.Name,
				ScopeKey: scope, Range: &span, Signature: declaration.Kind,
			})
		}
		if row.Status != "partial" {
			message := "The syntax is outside the supported PowerShell profile."
			if row.Status == "parse_error" {
				message = "The captured file has a PowerShell parser error."
			}
			batch.Diagnostics = append(batch.Diagnostics, model.DiagnosticFact{
				DiagnosticID: "psdiag-" + scope, Path: &path, Code: row.Reason, Severity: "warning",
				Message: message, ScopeDescription: "Captured Windows PowerShell syntax",
			})
		}
		if len(batch.Symbols)+len(batch.Diagnostics) == 0 {
			continue
		}
		if _, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: write, Stream: stream, Batch: batch, MaxFrameBytes: maxFrameBytes}); err != nil {
			return model.ProviderDone{}, nil, err
		}
		done.Sequence++
	}
	return done, analyzed, nil
}
