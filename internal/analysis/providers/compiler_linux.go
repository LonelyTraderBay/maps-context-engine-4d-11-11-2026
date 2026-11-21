//go:build linux

package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

const maxCompilerManifest = 128 << 10

func (p *NodeProvider) verifyCompiler(ctx context.Context) (_ *paths.Root, resultErr error) {
	c := p.config.Compiler
	if c == (CompilerInstallation{}) {
		return nil, nil
	}
	if strings.ContainsAny(c.Root, "*,") || relatedPaths(c.Root, p.config.ScratchParent) {
		return nil, ErrAuthority
	}
	archive, err := openPinned(c.ArchivePath, p.config.CompilerDigest, 8<<20, false)
	if err != nil {
		return nil, err
	}
	if archive.Close() != nil {
		return nil, ErrAuthority
	}
	manifest, err := openPinned(c.ManifestPath, c.ManifestDigest, maxCompilerManifest, false)
	if err != nil {
		return nil, err
	}
	defer manifest.Close()
	if _, err := manifest.Seek(0, io.SeekStart); err != nil {
		return nil, ErrAuthority
	}
	body, err := io.ReadAll(io.LimitReader(manifest, maxCompilerManifest+1))
	if err != nil || hexDigest(body) != c.ManifestDigest || model.ValidateWireJSON(body, maxCompilerManifest) != nil {
		return nil, ErrAuthority
	}
	var files map[string]string
	if json.Unmarshal(body, &files) != nil || len(files) > 256 ||
		files["lib/typescript.js"] == "" || files["LICENSE.txt"] == "" || files["ThirdPartyNoticeText.txt"] == "" {
		return nil, ErrAuthority
	}
	root, err := paths.OpenRoot(c.Root)
	if err != nil {
		return nil, ErrAuthority
	}
	defer func() {
		if resultErr != nil {
			_ = root.Close()
		}
	}()
	expected := make(map[string]bool, len(files))
	var remaining int64 = 32 << 20
	for path, hash := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := root.OpenRegular(path)
		if err != nil {
			return nil, ErrAuthority
		}
		sum := sha256.New()
		size, readErr := io.Copy(sum, io.LimitReader(file, remaining+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || size > remaining || hex.EncodeToString(sum.Sum(nil)) != hash {
			return nil, ErrAuthority
		}
		remaining -= size
		expected[path] = true
	}
	if err := verifyMembers(ctx, root, c.Root, expected); err != nil {
		return nil, err
	}
	return root, nil
}

func relatedPaths(a, b string) bool {
	under := func(root, candidate string) bool {
		rel, err := filepath.Rel(root, candidate)
		return err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return under(a, b) || under(b, a)
}
