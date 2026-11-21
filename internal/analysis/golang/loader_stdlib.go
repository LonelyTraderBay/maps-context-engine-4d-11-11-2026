package golang

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"golang.org/x/mod/module"
)

// StdlibAuthority independently proves that the registered source tree,
// complete permitted package closure, and toolchain are authorized and bound
// to dependencyDigest. It must authorize every path the Go tool and loader
// may read before ParseFile is called. It is a trust boundary, not a hash
// implementation or an operating-system sandbox.
type StdlibAuthority func(ctx context.Context, sourceRoot, toolchainDigest, dependencyDigest string, packages []string) (bool, error)

type TrustedStdlibInput struct {
	SourceRoot       *paths.Root // borrowed; must remain open throughout the load
	GoRoot           string
	Profile          *model.GoProfile
	Snapshot         *snapshot.Snapshot
	DependencyDigest string
	AllowedPackages  []string
	Scanner          *policy.Scanner
	AuthorizePath    func(context.Context, string) error // path relative to SourceRoot, never absolute
	VerifyAuthority  StdlibAuthority
	MaxManifestBytes int
	MaxFileBytes     int
	MaxFiles         int
	MaxSourceBytes   int64
}

// TrustedStdlib bounds parsing to an independently approved toolchain source
// root. Observation and callbacks cannot provide atomicity against writers.
type TrustedStdlib struct {
	root             *paths.Root
	rootPath         string
	goRoot           string
	profileID        string
	snapshotDigest   string
	toolchainDigest  string
	dependencyDigest string
	packages         []string
	allowed          map[string]bool
	scanner          *policy.Scanner
	authorizePath    func(context.Context, string) error
	verifyAuthority  StdlibAuthority
	maxManifestBytes int
	maxFileBytes     int
	maxFiles         int
	maxSourceBytes   int64
	mu               sync.Mutex
	used             bool
	fileCount        int
	sourceBytes      int64
	verified         map[string]bool
	parseSlots       chan struct{}
}

func NewTrustedStdlib(ctx context.Context, in TrustedStdlibInput) (*TrustedStdlib, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return nil, ErrLoaderUnsupported
	}
	if ctx == nil || in.SourceRoot == nil || in.Profile == nil || in.Snapshot == nil || in.Scanner == nil || in.AuthorizePath == nil || in.VerifyAuthority == nil ||
		!filepath.IsAbs(in.GoRoot) || filepath.Clean(in.GoRoot) != in.GoRoot || in.MaxManifestBytes < 1 || in.MaxManifestBytes > 64*1024*1024 ||
		in.MaxFileBytes < 1 || in.MaxFileBytes > 2*1024*1024 || in.MaxFiles < 1 || in.MaxFiles > 8192 || in.MaxSourceBytes < 1 || in.MaxSourceBytes > 64*1024*1024 ||
		len(in.AllowedPackages) < 1 || len(in.AllowedPackages) > 1024 {
		return nil, ErrLoaderInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrLoaderInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manifest := in.Snapshot.Manifest()
	profileID, err := model.ProfileDigest(in.Profile, in.MaxManifestBytes)
	if err != nil || model.VerifyCaptureDigest(manifest, in.MaxManifestBytes) != nil || manifest.ProfileID != profileID || manifest.ProviderDigest != in.Profile.ProviderDigest {
		return nil, ErrInputMismatch
	}
	digestPresent := false
	for _, digest := range manifest.DependencyInputDigests {
		digestPresent = digestPresent || digest == in.DependencyDigest
	}
	rootPath, err := in.SourceRoot.CanonicalPath()
	if err != nil || rootPath != filepath.Join(in.GoRoot, "src") || !digestPresent {
		return nil, ErrInputMismatch
	}
	packages := append([]string(nil), in.AllowedPackages...)
	sort.Strings(packages)
	allowed := make(map[string]bool, len(packages))
	for _, name := range packages {
		if module.CheckImportPath(name) != nil || allowed[name] || in.Scanner.CheckContent([]byte(name)) != nil {
			return nil, ErrLoaderInput
		}
		allowed[name] = true
	}
	stdlib := &TrustedStdlib{root: in.SourceRoot, rootPath: rootPath, goRoot: in.GoRoot, profileID: profileID, snapshotDigest: manifest.SnapshotDigest,
		toolchainDigest: in.Profile.ToolchainDigest, dependencyDigest: in.DependencyDigest, packages: packages, allowed: allowed, scanner: in.Scanner,
		authorizePath: in.AuthorizePath, verifyAuthority: in.VerifyAuthority, maxManifestBytes: in.MaxManifestBytes,
		maxFileBytes: in.MaxFileBytes, maxFiles: in.MaxFiles, maxSourceBytes: in.MaxSourceBytes, verified: map[string]bool{}, parseSlots: make(chan struct{}, 4)}
	verified, err := stdlib.Check(ctx, in.Profile, in.Snapshot, in.GoRoot)
	if err != nil {
		return nil, err
	}
	if !verified {
		return nil, ErrLoaderAuthority
	}
	return stdlib, nil
}

func (s *TrustedStdlib) Check(ctx context.Context, profile *model.GoProfile, captured *snapshot.Snapshot, goRoot string) (bool, error) {
	if s == nil || ctx == nil || profile == nil || captured == nil || goRoot != s.goRoot {
		return false, ErrLoaderInput
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	manifest := captured.Manifest()
	id, err := model.ProfileDigest(profile, s.maxManifestBytes)
	if err != nil || id != s.profileID || manifest.SnapshotDigest != s.snapshotDigest || model.VerifyCaptureDigest(manifest, s.maxManifestBytes) != nil || s.root.Verify() != nil {
		return false, nil
	}
	ok, err := s.verifyAuthority(ctx, s.rootPath, s.toolchainDigest, s.dependencyDigest, append([]string(nil), s.packages...))
	if err != nil {
		return false, loaderAuthorityError(ctx)
	}
	if !ok {
		return false, nil
	}
	s.mu.Lock()
	verifiedFiles := make([]string, 0, len(s.verified))
	for filename := range s.verified {
		verifiedFiles = append(verifiedFiles, filename)
	}
	s.mu.Unlock()
	sort.Strings(verifiedFiles)
	for _, filename := range verifiedFiles {
		relative, err := filepath.Rel(s.rootPath, filename)
		if err != nil {
			return false, nil
		}
		if err := s.authorizePath(ctx, filepath.ToSlash(relative)); err != nil {
			return false, loaderAuthorityError(ctx)
		}
	}
	return ctx.Err() == nil && s.root.Verify() == nil, ctx.Err()
}

func (s *TrustedStdlib) AllowsPackage(name string) bool { return s != nil && s.allowed[name] }

// A verification record is tied to one load and cannot satisfy a later load.
func (s *TrustedStdlib) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used {
		return false
	}
	s.used = true
	return true
}

func (s *TrustedStdlib) fileVerified(filename string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verified[filename]
}

// parseFile reopens only the approved root, compares the bytes passed by the
// loader, suppresses parser paths/errors, and budgets all parse attempts.
func (s *TrustedStdlib) parseFile(ctx context.Context, fset *token.FileSet, filename string, source []byte) (*ast.File, error) {
	if s == nil || ctx == nil || fset == nil || !filepath.IsAbs(filename) || filepath.Clean(filename) != filename || len(source) > s.maxFileBytes {
		return nil, ErrInputMismatch
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(s.rootPath, filename)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) ||
		policy.CheckPath(filepath.ToSlash(relative)) != nil || s.scanner.CheckContent([]byte(relative)) != nil || !strings.HasSuffix(relative, ".go") {
		return nil, ErrInputMismatch
	}
	select {
	case s.parseSlots <- struct{}{}:
		defer func() { <-s.parseSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.fileCount >= s.maxFiles || int64(len(source)) > s.maxSourceBytes-s.sourceBytes {
		s.mu.Unlock()
		return nil, ErrSyntaxLimit
	}
	s.fileCount++
	s.sourceBytes += int64(len(source))
	s.mu.Unlock()
	relative = filepath.ToSlash(relative)
	if err := s.authorizePath(ctx, relative); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	if err := s.scanner.CheckContent(source); err != nil && !knownGoToolchainIdentifier(relative, source, err) {
		return nil, err
	}
	file, err := s.root.OpenRegular(relative)
	if err != nil {
		return nil, ErrInputMismatch
	}
	before, err := file.Stat()
	if err != nil || paths.VerifyRegularFile(file) != nil {
		file.Close()
		return nil, ErrInputMismatch
	}
	actual, readErr := io.ReadAll(io.LimitReader(file, int64(s.maxFileBytes)+1))
	after, statErr := file.Stat()
	verifyErr := paths.VerifyRegularFile(file)
	closeErr := file.Close()
	if readErr != nil || statErr != nil || verifyErr != nil || closeErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(actual, source) || s.root.Verify() != nil {
		return nil, ErrInputMismatch
	}
	parsed, err := parser.ParseFile(fset, filename, source, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, ErrCapturedParse
	}
	if err := s.authorizePath(ctx, relative); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.verified[filename] = true
	s.mu.Unlock()
	return parsed, nil
}

// Go 1.27.4's approved x509 source has an assignment between Go
// identifiers that the content scanner classifies as a credential. This
// exception applies inside the separately authorized toolchain parser, at
// this exact path and digest. The parser still checks the opened file
// against these bytes before it publishes any type information.
func knownGoToolchainIdentifier(relative string, source []byte, scanErr error) bool {
	if scanErr != policy.ErrSecretBlocked || relative != "crypto/x509/pkcs8.go" {
		return false
	}
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:]) == "5402255bfdc3b04452b08e238fabe1022642e75d577fc91a296c06bbaca51841"
}
