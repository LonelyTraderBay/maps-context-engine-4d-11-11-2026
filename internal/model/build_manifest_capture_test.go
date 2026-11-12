package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestBuildManifestVerifiesCapturedAnchors(t *testing.T) {
	id := "01234567-89ab-cdef-0123-456789abcdef"
	h := strings.Repeat("a", 64)
	provider := strings.Repeat("b", 64)
	source := []byte("a😀\n")
	content := fmt.Sprintf("%x", sha256.Sum256(source))
	anchor := SourceAnchorIdentity{Path: "a.tsx", ContentHash: content, Kind: "element_site", Range: ByteRange{StartByte: 1, EndByte: 5}, ProviderDigest: provider}
	anchorID, err := SourceAnchorDigest(anchor, source, 8192)
	if err != nil {
		t.Fatal(err)
	}
	manifest := BuildManifest{SchemaVersion: 1, WorktreeID: id, ProfileID: h, ProviderVersion: "v1", BuildConfigDigest: h, InputManifestDigest: h, Mode: "development", ModuleCoverage: "partial", Sources: []ManifestSource{{Path: "a.tsx", ContentHash: content}}, Anchors: []AnchorEntry{{AnchorID: anchorID, Kind: anchor.Kind, Path: anchor.Path, ContentHash: content, Range: anchor.Range}}, ProviderDigest: provider}
	manifest.BuildMapID, err = ComputeBuildMapDigest(manifest, 16384)
	if err != nil {
		t.Fatal(err)
	}
	captured := map[string][]byte{"a.tsx": source}
	if err := VerifyBuildManifestCaptured(manifest, captured, 16384, 1024); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBuildManifestCaptured(manifest, map[string][]byte{"a.tsx": []byte("other")}, 16384, 1024); err == nil {
		t.Fatal("accepted wrong bytes")
	}
	if err := VerifyBuildManifestCaptured(manifest, captured, 16384, 1); err == nil {
		t.Fatal("ignored source budget")
	}
	manifest.Anchors[0].Range.StartByte = 2
	manifest.BuildMapID, err = ComputeBuildMapDigest(manifest, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBuildManifestCaptured(manifest, captured, 16384, 1024); err == nil {
		t.Fatal("accepted a split rune")
	}
	manifest.Anchors[0].Range.StartByte = 1
	manifest.Anchors[0].AnchorID = strings.Repeat("c", 64)
	manifest.BuildMapID, err = ComputeBuildMapDigest(manifest, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBuildManifestCaptured(manifest, captured, 16384, 1024); err == nil {
		t.Fatal("accepted fake anchor id")
	}
}
