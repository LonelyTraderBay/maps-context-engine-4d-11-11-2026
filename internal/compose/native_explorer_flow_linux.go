//go:build linux

package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"

	golanganalysis "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/golang"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/sql"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/flowview"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

func (n *nativeScope) projectStaticFlow(ctx context.Context, observed ScopeObservation, graph *index.PublishedGraph,
	root model.SymbolRef, options flowview.Options) (_ flowview.Result, resultErr error) {
	if graph == nil || graph.Snapshot == nil || len(observed.Vector.Members) != 1 {
		return flowview.Result{}, flowview.ErrInput
	}
	available, directory, err := flowPackages(graph, root.SymbolID)
	if err != nil {
		return flowview.Result{}, err
	}
	i, err := openNativeInventory(ctx, n, false)
	if err != nil {
		return flowview.Result{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, i.Close()) }()
	files, err := i.Observe(ctx)
	if err != nil {
		return flowview.Result{}, err
	}
	captured := nativeCapture{snapshot: graph.Snapshot, files: files, blocked: map[string]string{}}
	manifest := graph.Snapshot.Manifest()
	if len(manifest.DependencyInputDigests) != 1 || manifest.DependencyInputDigests[0] != files.InputDigest {
		return flowview.Result{}, ErrScopeChanged
	}
	member := observed.Vector.Members[0]
	typed, err := i.loadTypedIndex(ctx, captured, available, &nativeFlowLoad{directory: directory, member: member})
	if err != nil {
		return flowview.Result{}, err
	}
	if typed == nil || typed.program == nil {
		return flowview.Result{}, flowview.ErrUnverified
	}
	defer func() { resultErr = errors.Join(resultErr, typed.proof.Close()) }()
	options.SQL, err = capturedFlowSQL(graph, member)
	if err != nil {
		return flowview.Result{}, err
	}
	result, err := typed.program.Project(ctx, root, options)
	if err != nil {
		return flowview.Result{}, err
	}
	if err := i.observeCaptured(ctx, captured, graph.Snapshot, false); err != nil {
		return flowview.Result{}, err
	}
	return result, nil
}

func flowPackages(graph *index.PublishedGraph, root string) (map[string]golanganalysis.TypedPackageIdentity, string, error) {
	packages := make(map[string]golanganalysis.TypedPackageIdentity)
	for _, pkg := range graph.Packages {
		if pkg.Variant == "production" {
			packages[pkg.PackageKey] = golanganalysis.TypedPackageIdentity{ImportPath: pkg.ImportPath, PackageKey: pkg.PackageKey, ModuleKey: pkg.ModuleKey}
		}
	}
	available := make(map[string]golanganalysis.TypedPackageIdentity)
	directory := ""
	for _, symbol := range graph.Symbols {
		if symbol.Path == nil || symbol.PackageKey == nil || packages[*symbol.PackageKey].PackageKey == "" {
			continue
		}
		identity := packages[*symbol.PackageKey]
		dir := path.Dir(*symbol.Path)
		if earlier, exists := available[dir]; exists && earlier != identity {
			return nil, "", flowview.ErrUnverified
		}
		available[dir] = identity
		if symbol.SymbolID == root && (symbol.Kind == "function" || symbol.Kind == "method") {
			directory = dir
		}
	}
	if directory == "" {
		return nil, "", flowview.ErrUnverified
	}
	return available, directory, nil
}

func capturedFlowSQL(graph *index.PublishedGraph, member model.ScopeMember) (*flowview.SQLInput, error) {
	file, ok := graph.Snapshot.File("mce4d-flow.json")
	if !ok {
		return nil, nil
	}
	data := file.Bytes()
	if _, err := model.DecodeIdentityJSON(data, 65536); err != nil {
		return nil, flowview.ErrUnverified
	}
	var config struct {
		SchemaVersion int         `json:"schema_version"`
		Profile       sql.Profile `json:"sql_profile"`
		Schemas       []string    `json:"schema_paths"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&config) != nil || config.SchemaVersion != 1 || len(config.Schemas) > 31 {
		return nil, flowview.ErrUnverified
	}
	forPath := func(name string) (model.SourceRef, bool) {
		file, ok := graph.Snapshot.File(name)
		return model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
			Path: name, ContentHash: file.ContentHash(), Range: model.ByteRange{EndByte: int64(file.Len())}}, ok
	}
	configRef, _ := forPath("mce4d-flow.json")
	result := &flowview.SQLInput{Profile: config.Profile, Config: &configRef, Schemas: []model.SourceRef{}}
	seen := make(map[string]bool)
	for _, name := range config.Schemas {
		ref, ok := forPath(name)
		if !ok || seen[name] || path.Ext(name) != ".sql" {
			return nil, flowview.ErrUnverified
		}
		seen[name] = true
		result.Schemas = append(result.Schemas, ref)
	}
	return result, nil
}
