package providers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

const maxManifest = 16 << 20

func verifyCapture(ctx context.Context, root *paths.Root, job ports.ProviderJob) error {
	name, err := root.CanonicalPath()
	if err != nil || name != job.Start.SnapshotRoot {
		return ErrAuthority
	}
	manifestName := filepath.Base(job.Start.ManifestPath)
	manifest, err := root.OpenRegular(manifestName)
	if err != nil {
		return ErrAuthority
	}
	raw, readErr := io.ReadAll(io.LimitReader(manifest, maxManifest+1))
	closeErr := manifest.Close()
	if readErr != nil || closeErr != nil || len(raw) > maxManifest || hexDigest(raw) != job.Start.ManifestDigest {
		return ErrAuthority
	}
	captured := job.Capture.Manifest()
	parsed, err := model.DecodeCaptureManifestJSON(raw, maxManifest)
	if err != nil || model.VerifyCaptureDigest(parsed, maxManifest) != nil ||
		parsed.SnapshotDigest != captured.SnapshotDigest || parsed.WorktreeID != captured.WorktreeID ||
		parsed.ProfileID != job.Start.ProfileID ||
		parsed.ProviderDigest != job.Start.ProviderDigest {
		return ErrAuthority
	}
	expected := map[string]bool{manifestName: true}
	for _, item := range parsed.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.Status != "captured" {
			continue
		}
		if expected[item.Path] {
			return ErrAuthority
		}
		expected[item.Path] = true
		source, err := root.OpenRegular(item.Path)
		if err != nil {
			return ErrAuthority
		}
		digest := sha256.New()
		size, readErr := io.Copy(digest, io.LimitReader(source, *item.ByteLength+1))
		closeErr := source.Close()
		if readErr != nil || closeErr != nil || size != *item.ByteLength ||
			hex.EncodeToString(digest.Sum(nil)) != *item.ContentHash {
			return ErrAuthority
		}
	}
	return verifyMembers(ctx, root, name, expected)
}

func verifyMembers(ctx context.Context, root *paths.Root, name string, expected map[string]bool) error {
	directories := map[string]bool{".": true}
	for file := range expected {
		for dir := path.Dir(file); !directories[dir]; dir = path.Dir(dir) {
			directories[dir] = true
		}
	}
	err := fs.WalkDir(os.DirFS(name), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return ErrAuthority
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if !directories[path] {
				return ErrAuthority
			}
			return nil
		}
		if !entry.Type().IsRegular() || !expected[path] {
			return ErrAuthority
		}
		delete(expected, path)
		return nil
	})
	if err != nil {
		return err
	}
	if len(expected) != 0 || root.Verify() != nil {
		return ErrAuthority
	}
	return nil
}

func hexDigest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func readProviderStream(ctx context.Context, reader *bufio.Reader, job ports.ProviderJob, sink ports.ProviderSink) (done model.ProviderDone, resultErr error) {
	facts := factScope{capture: job.Capture, packages: map[string]bool{}, symbols: map[string]bool{},
		relations: map[string]bool{}, diagnostics: map[string]bool{}}
	manifest := job.Capture.Manifest()
	stream, err := model.BeginProviderStream(job.Start.JobID, manifest.SnapshotDigest)
	if err != nil {
		return done, ErrProtocol
	}
	limit := int(job.Start.MaxFrameBytes)
	remaining := maxStream
	for {
		data, err := readFrame(reader, limit)
		if err != nil {
			return done, err
		}
		remaining -= len(data)
		if remaining < 0 {
			return done, ErrBudget
		}
		message, err := model.DecodeProviderMessageJSON(data, limit)
		if err != nil {
			return done, ErrProtocol
		}
		stream, err = model.AcceptProviderOutput(stream, message, limit)
		if err != nil {
			return done, ErrProtocol
		}
		if err := job.Recheck(ctx); err != nil {
			return done, ErrAuthority
		}
		switch value := message.(type) {
		case *model.ProviderBatch:
			if err := facts.batch(ctx, value); err != nil {
				return done, err
			}
			if err := sink(ctx, *value); err != nil {
				return done, err
			}
		case *model.ProviderDone:
			if err := checkDone(job, manifest.WorktreeID, value); err != nil {
				return done, err
			}
			return *value, nil
		}
	}
}
