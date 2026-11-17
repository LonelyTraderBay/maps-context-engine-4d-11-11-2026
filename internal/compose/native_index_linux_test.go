//go:build linux

package compose

import (
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestNativeSyntaxUsesConfiguredNearestCapturedModule(t *testing.T) {
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: "11111111-1111-4111-8111-111111111111", ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	files := []snapshot.File{}
	for _, input := range []struct{ path, body string }{{"go.mod", "module example.invalid/root\n\ngo 1.27\n"}, {"nested/go.mod", "module example.invalid/nested\n\ngo 1.27\n"}, {"broken/go.mod", "not a module declaration\n"}} {
		file, err := snapshot.NewFile(input.path, []byte(input.body), 4096)
		if err != nil {
			t.Fatal(err)
		}
		hash, length := file.ContentHash(), int64(file.Len())
		files = append(files, file)
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: input.path, Status: "captured", ContentHash: &hash, ByteLength: &length})
	}
	captured, err := snapshot.Freeze(manifest, files, 65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	profile := &model.GoProfile{ModuleRoots: []string{"."}}
	modules := nativeSyntaxModules(captured, profile)
	if root, importPath, ok := nativeSyntaxModuleFor(modules, "sub/pkg"); !ok || root != "." || importPath != "example.invalid/root" {
		t.Fatal(root, importPath, ok)
	}
	for _, directory := range []string{"nested/sub", "broken/sub"} {
		if _, _, ok := nativeSyntaxModuleFor(modules, directory); ok {
			t.Fatal("assigned parent import identity across a module boundary", directory)
		}
	}
	profile.ModuleRoots = append(profile.ModuleRoots, "nested", "broken")
	modules = nativeSyntaxModules(captured, profile)
	if root, importPath, ok := nativeSyntaxModuleFor(modules, "nested/sub"); !ok || root != "nested" || importPath != "example.invalid/nested" {
		t.Fatal(root, importPath, ok)
	}
	if _, _, ok := nativeSyntaxModuleFor(modules, "broken/sub"); ok {
		t.Fatal("invented an import identity for an invalid manifest")
	}
}
