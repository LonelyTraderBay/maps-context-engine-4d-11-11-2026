//go:build linux

package golang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestLoaderToolchainProofRejectsChangedBinary(t *testing.T) {
	goRoot := t.TempDir()
	bin := filepath.Join(goRoot, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(bin, "go")
	contents := []byte("fixture toolchain identity; not executed")
	if err := os.WriteFile(filename, contents, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := preflightContext(t)
	sum := sha256.Sum256(contents)
	tool, err := runtimeadapter.OpenGoExecutable(ctx, runtimeadapter.GoExecutableInput{BinRoot: root, ExpectedSHA256: hex.EncodeToString(sum[:]), MaxBytes: 1024, Authorize: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer tool.Close()
	t.Setenv("PATH", bin)
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": "package x\n"})
	input.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
	input.VerifyToolchain = LoaderToolchainVerifier(tool, goRoot, func(_ context.Context, profile *model.GoProfile) (bool, error) {
		return profile.ToolchainDigest == input.Profile.ToolchainDigest, nil
	})
	result, err := PreflightLoader(ctx, input)
	if err != nil || result.Status != "eligible_for_guarded_load" {
		t.Fatalf("expected fixture proof: %+v %v", result, err)
	}
	if err := os.WriteFile(filename, []byte("changed identity"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err = PreflightLoader(ctx, input)
	if err != nil || result.Status != "partial" || !containsReason(result.Reasons, "TOOLCHAIN_UNVERIFIED") {
		t.Fatalf("changed binary accepted: %+v %v", result, err)
	}
}
