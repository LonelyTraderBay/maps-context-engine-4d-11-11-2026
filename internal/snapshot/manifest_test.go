package snapshot

import (
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func manifestFixture(t *testing.T) (model.CaptureManifest, []File) {
	t.Helper()
	a, err := NewFile("a.go", []byte("abc"), 100)
	if err != nil {
		t.Fatal(err)
	}
	z, err := NewFile("z.go", []byte("x\r\n"), 100)
	if err != nil {
		t.Fatal(err)
	}
	ah, zh := a.ContentHash(), z.ContentHash()
	al, zl := int64(a.Len()), int64(z.Len())
	reason := "policy_denied"
	return model.CaptureManifest{
		SchemaVersion: 1, WorktreeID: "01234567-89ab-cdef-0123-456789abcdef",
		ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64),
		Files: []model.ManifestFile{
			{Path: "z.go", Status: "captured", ContentHash: &zh, ByteLength: &zl},
			{Path: "secret.env", Status: "excluded", ReasonCode: &reason},
			{Path: "a.go", Status: "captured", ContentHash: &ah, ByteLength: &al},
		},
		DependencyInputDigests: []string{strings.Repeat("e", 64), strings.Repeat("d", 64), strings.Repeat("d", 64)},
	}, []File{a, z}
}

func TestFreezeMatchesIndependentCanonicalDigestAndOwnsInputs(t *testing.T) {
	manifest, files := manifestFixture(t)
	s, err := Freeze(manifest, files, 32768, 1024)
	if err != nil {
		t.Fatal(err)
	}
	want := "5bbe3712e400cec8e5a0ed5afc442a47cdff9e5e3d8909cc05e6585d42f800f6"
	got := s.Manifest()
	if got.SnapshotDigest != want || got.Files[0].Path != "a.go" || got.Files[2].Path != "z.go" || got.DependencyInputDigests[0] != strings.Repeat("d", 64) {
		t.Fatalf("canonical manifest: %+v", got)
	}
	if err := model.VerifyCaptureDigest(got, 32768); err != nil {
		t.Fatal(err)
	}
	*manifest.Files[0].ContentHash = strings.Repeat("f", 64)
	files[0].source[0] = 'x'
	*got.Files[0].ContentHash = strings.Repeat("f", 64)
	got.DependencyInputDigests[0] = strings.Repeat("f", 64)
	stored := s.Manifest()
	file, ok := s.File("a.go")
	if !ok || string(file.Bytes()) != "abc" || stored.SnapshotDigest != want || *stored.Files[0].ContentHash != file.ContentHash() || stored.DependencyInputDigests[0] != strings.Repeat("d", 64) {
		t.Fatal("snapshot changed through returned or input values")
	}
	if _, ok := s.File("secret.env"); ok {
		t.Fatal("excluded entry produced source bytes")
	}
}

func TestFreezeRejectsIncompleteOrChangedInputs(t *testing.T) {
	manifest, files := manifestFixture(t)
	if _, err := Freeze(manifest, files[:1], 32768, 1024); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("missing captured input accepted: %v", err)
	}
	if _, err := Freeze(manifest, append(files, files[0]), 32768, 1024); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("duplicate captured input accepted: %v", err)
	}
	extra, err := NewFile("secret.env", []byte("not authorized"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Freeze(manifest, append(files, extra), 32768, 1024); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("excluded input accepted: %v", err)
	}
	*manifest.Files[0].ContentHash = strings.Repeat("f", 64)
	if _, err := Freeze(manifest, files, 32768, 1024); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("mismatched content hash accepted: %v", err)
	}
	manifest, files = manifestFixture(t)
	manifest.SnapshotDigest = strings.Repeat("0", 64)
	if _, err := Freeze(manifest, files, 32768, 1024); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("mismatched declared digest accepted: %v", err)
	}
	manifest, files = manifestFixture(t)
	if _, err := Freeze(manifest, files, 32, 1024); err == nil {
		t.Fatal("manifest budget ignored")
	}
	if _, err := Freeze(manifest, files, 32768, 5); !errors.Is(err, ErrSourceLimit) {
		t.Fatalf("aggregate source budget ignored: %v", err)
	}
}

func TestParseOutcomeDoesNotChangeCaptureDigest(t *testing.T) {
	file, err := NewFile("broken.go", []byte("package p\nfunc (\n"), 100)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	input := model.CaptureManifest{
		SchemaVersion: 1, WorktreeID: "01234567-89ab-cdef-0123-456789abcdef",
		ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64),
		Files:                  []model.ManifestFile{{Path: file.Path(), Status: "captured", ContentHash: &hash, ByteLength: &length}},
		DependencyInputDigests: []string{},
	}
	s, err := Freeze(input, []File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	digest := s.Manifest().SnapshotDigest
	if _, err := parser.ParseFile(token.NewFileSet(), file.Path(), file.Bytes(), parser.PackageClauseOnly); err != nil {
		t.Fatalf("package-only parse: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), file.Path(), file.Bytes(), parser.AllErrors); err == nil {
		t.Fatal("fixture should fail a full parse")
	}
	if s.Manifest().SnapshotDigest != digest || model.VerifyCaptureDigest(s.Manifest(), 8192) != nil {
		t.Fatal("parser outcome changed capture identity")
	}
}
