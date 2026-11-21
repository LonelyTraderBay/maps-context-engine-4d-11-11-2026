package golang

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrLoaderUnsupported = errors.New("Go loader platform is not verified")
	ErrCapturedParse     = errors.New("captured Go source could not be parsed")
)

// CapturedOverlay maps synthetic absolute filenames to immutable captured
// inputs. It does not create those paths or prove the containing directory is
// isolated. The caller must establish that separately before running a loader.
type CapturedOverlay struct {
	ctx            context.Context
	snapshot       *snapshot.Snapshot
	manifest       model.CaptureManifest
	epoch          int64
	scanner        *policy.Scanner
	authorizeScope func(context.Context, string, string, string, int64) error
	authorizePath  func(context.Context, string, string, string, int64) error
	files          map[string]string
}

type CapturedOverlayInput struct {
	Snapshot         *snapshot.Snapshot
	Profile          *model.GoProfile
	SyntheticRoot    string
	PolicyEpoch      int64
	Scanner          *policy.Scanner
	AuthorizeScope   func(context.Context, string, string, string, int64) error
	AuthorizePath    func(context.Context, string, string, string, int64) error
	MaxManifestBytes int
	MaxFileBytes     int
	MaxOverlayBytes  int64
}

func NewCapturedOverlay(ctx context.Context, input CapturedOverlayInput) (*CapturedOverlay, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return nil, ErrLoaderUnsupported
	}
	if ctx == nil || input.Snapshot == nil || input.Profile == nil || input.Scanner == nil || input.AuthorizeScope == nil || input.AuthorizePath == nil ||
		!filepath.IsAbs(input.SyntheticRoot) || filepath.Clean(input.SyntheticRoot) != input.SyntheticRoot || input.PolicyEpoch < 1 || input.PolicyEpoch > 9007199254740991 ||
		input.MaxManifestBytes < 1 || input.MaxManifestBytes > 64*1024*1024 || input.MaxFileBytes < 1 || input.MaxFileBytes > 2*1024*1024 || input.MaxOverlayBytes < 1 || input.MaxOverlayBytes > 64*1024*1024 {
		return nil, ErrLoaderInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrLoaderInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manifest := input.Snapshot.Manifest()
	profile, err := model.ProfileDigest(input.Profile, input.MaxManifestBytes)
	if err != nil || model.VerifyCaptureDigest(manifest, input.MaxManifestBytes) != nil || manifest.ProfileID != profile || manifest.ProviderDigest != input.Profile.ProviderDigest {
		return nil, ErrInputMismatch
	}
	if err := input.AuthorizeScope(ctx, manifest.WorktreeID, profile, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	paths := make(map[string]string, len(manifest.Files))
	pathKeys := make(map[string]bool, len(manifest.Files))
	var total int64
	for _, item := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.Status != "captured" || item.ContentHash == nil || item.ByteLength == nil || policy.CheckPath(item.Path) != nil {
			return nil, ErrInputMismatch
		}
		if err := input.AuthorizePath(ctx, manifest.WorktreeID, item.Path, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
			return nil, loaderAuthorityError(ctx)
		}
		file, ok := input.Snapshot.File(item.Path)
		if !ok || file.ContentHash() != *item.ContentHash || int64(file.Len()) != *item.ByteLength {
			return nil, ErrInputMismatch
		}
		if file.Len() > input.MaxFileBytes {
			return nil, ErrSyntaxLimit
		}
		absolute := filepath.Join(input.SyntheticRoot, filepath.FromSlash(item.Path))
		relative, err := filepath.Rel(input.SyntheticRoot, absolute)
		if err != nil || filepath.ToSlash(relative) != item.Path || !filepath.IsLocal(relative) {
			return nil, ErrInputMismatch
		}
		key := absolute
		if runtime.GOOS == "windows" {
			for _, part := range strings.Split(item.Path, "/") {
				if !filepath.IsLocal(part) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
					return nil, ErrInputMismatch
				}
			}
			key = strings.ToUpper(key)
		}
		if pathKeys[key] {
			return nil, ErrInputMismatch
		}
		pathKeys[key] = true
		size := int64(len(absolute)) + int64(file.Len())
		if size > input.MaxOverlayBytes-total {
			return nil, ErrSyntaxLimit
		}
		total += size
		if err := input.Scanner.CheckContent([]byte(item.Path)); err != nil {
			return nil, err
		}
		if err := input.Scanner.CheckContent(file.Bytes()); err != nil {
			return nil, err
		}
		paths[absolute] = item.Path
	}
	for _, item := range manifest.Files {
		if err := input.AuthorizePath(ctx, manifest.WorktreeID, item.Path, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
			return nil, loaderAuthorityError(ctx)
		}
	}
	if err := input.AuthorizeScope(ctx, manifest.WorktreeID, profile, manifest.PolicyDigest, input.PolicyEpoch); err != nil {
		return nil, loaderAuthorityError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &CapturedOverlay{ctx: ctx, snapshot: input.Snapshot, manifest: manifest, epoch: input.PolicyEpoch, scanner: input.Scanner, authorizeScope: input.AuthorizeScope, authorizePath: input.AuthorizePath, files: paths}, nil
}

// Overlay returns fresh copies; changing one does not alter the capture or
// permit ParseFile to accept bytes that differ from the original snapshot.
func (o *CapturedOverlay) Overlay() (map[string][]byte, error) {
	if o == nil || o.ctx == nil || o.snapshot == nil {
		return nil, ErrLoaderInput
	}
	if err := o.authorizeScope(o.ctx, o.manifest.WorktreeID, o.manifest.ProfileID, o.manifest.PolicyDigest, o.epoch); err != nil {
		return nil, loaderAuthorityError(o.ctx)
	}
	result := make(map[string][]byte, len(o.files))
	for absolute, relative := range o.files {
		if err := o.ctx.Err(); err != nil {
			return nil, err
		}
		if err := o.authorizePath(o.ctx, o.manifest.WorktreeID, relative, o.manifest.PolicyDigest, o.epoch); err != nil {
			return nil, loaderAuthorityError(o.ctx)
		}
		file, ok := o.snapshot.File(relative)
		if !ok {
			return nil, ErrInputMismatch
		}
		result[absolute] = file.Bytes()
	}
	for _, relative := range o.files {
		if err := o.authorizePath(o.ctx, o.manifest.WorktreeID, relative, o.manifest.PolicyDigest, o.epoch); err != nil {
			return nil, loaderAuthorityError(o.ctx)
		}
	}
	if err := o.authorizeScope(o.ctx, o.manifest.WorktreeID, o.manifest.ProfileID, o.manifest.PolicyDigest, o.epoch); err != nil {
		return nil, loaderAuthorityError(o.ctx)
	}
	if err := o.ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ParseFile is safe for concurrent packages.Load callbacks when the supplied
// authorization functions are themselves concurrency-safe. It rejects any
// path or bytes not present in the capture and suppresses raw parser errors.
func (o *CapturedOverlay) ParseFile(fset *token.FileSet, filename string, source []byte) (*ast.File, error) {
	if o == nil || o.ctx == nil || fset == nil || o.snapshot == nil {
		return nil, ErrLoaderInput
	}
	if err := o.ctx.Err(); err != nil {
		return nil, err
	}
	relative, ok := o.files[filename]
	if !ok || !strings.HasSuffix(relative, ".go") {
		return nil, ErrInputMismatch
	}
	if err := o.authorizeScope(o.ctx, o.manifest.WorktreeID, o.manifest.ProfileID, o.manifest.PolicyDigest, o.epoch); err != nil {
		return nil, loaderAuthorityError(o.ctx)
	}
	if err := o.authorizePath(o.ctx, o.manifest.WorktreeID, relative, o.manifest.PolicyDigest, o.epoch); err != nil {
		return nil, loaderAuthorityError(o.ctx)
	}
	file, ok := o.snapshot.File(relative)
	if !ok || !bytes.Equal(source, file.Bytes()) {
		return nil, ErrInputMismatch
	}
	if err := o.scanner.CheckContent(source); err != nil {
		return nil, err
	}
	parsed, err := parser.ParseFile(fset, filename, source, parser.AllErrors|parser.ParseComments)
	if ctxErr := o.ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, ErrCapturedParse
	}
	if err := o.authorizePath(o.ctx, o.manifest.WorktreeID, relative, o.manifest.PolicyDigest, o.epoch); err != nil {
		return nil, loaderAuthorityError(o.ctx)
	}
	if err := o.authorizeScope(o.ctx, o.manifest.WorktreeID, o.manifest.ProfileID, o.manifest.PolicyDigest, o.epoch); err != nil {
		return nil, loaderAuthorityError(o.ctx)
	}
	if err := o.ctx.Err(); err != nil {
		return nil, err
	}
	return parsed, nil
}
