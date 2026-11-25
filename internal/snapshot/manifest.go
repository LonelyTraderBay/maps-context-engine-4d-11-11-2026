package snapshot

import (
	"errors"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrManifestMismatch = errors.New("capture manifest does not match captured inputs")

// Snapshot contains a canonical capture manifest and its captured inputs. It
// has no parser/type results and makes no claim about filesystem atomicity.
type Snapshot struct {
	manifest      model.CaptureManifest
	manifestBytes int
	files         map[string]File
}

// Identity holds the bindings verified when the immutable snapshot was frozen.
type Identity struct {
	WorktreeID     string
	ProfileID      string
	PolicyDigest   string
	ProviderDigest string
	SnapshotDigest string
}

// Freeze checks the complete supplied input set, computes capture-v1, and
// copies it before returning. The caller must establish membership, security,
// policy, freshness and dependency closure separately.
func Freeze(input model.CaptureManifest, captured []File, maxManifestBytes int, maxSourceBytes int64) (*Snapshot, error) {
	return freeze(input, captured, maxManifestBytes, maxSourceBytes, false)
}

// FreezeSubset verifies the original manifest and only the supplied blobs.
// File reports false for other paths. Use it for bounded snapshot reads.
func FreezeSubset(input model.CaptureManifest, captured []File, maxManifestBytes int, maxSourceBytes int64) (*Snapshot, error) {
	return freeze(input, captured, maxManifestBytes, maxSourceBytes, true)
}

func freeze(input model.CaptureManifest, captured []File, maxManifestBytes int, maxSourceBytes int64, subset bool) (*Snapshot, error) {
	if maxSourceBytes <= 0 {
		return nil, ErrSourceLimit
	}
	if len(captured) > len(input.Files) {
		return nil, ErrManifestMismatch
	}
	digest, manifestBytes, err := model.ComputeCaptureDigestAndSize(input, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	if input.SnapshotDigest != "" && input.SnapshotDigest != digest {
		return nil, ErrManifestMismatch
	}
	var total int64
	for _, source := range captured {
		length := int64(len(source.source))
		if length > maxSourceBytes-total {
			return nil, ErrSourceLimit
		}
		total += length
	}
	manifest := copyManifest(input)
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	sort.Strings(manifest.DependencyInputDigests)
	manifest.SnapshotDigest = digest
	byPath := make(map[string]File, len(captured))
	for _, source := range captured {
		if source.path == "" {
			return nil, ErrManifestMismatch
		}
		if _, exists := byPath[source.path]; exists {
			return nil, ErrManifestMismatch
		}
		copied, err := NewFile(source.path, source.source, len(source.source))
		if err != nil || copied.hash != source.hash {
			return nil, ErrManifestMismatch
		}
		byPath[source.path] = copied
	}
	used := 0
	for _, item := range manifest.Files {
		source, present := byPath[item.Path]
		if item.Status != "captured" {
			if present {
				return nil, ErrManifestMismatch
			}
			continue
		}
		if subset && !present {
			continue
		}
		if !present || item.ContentHash == nil || item.ByteLength == nil || *item.ContentHash != source.hash || *item.ByteLength != int64(len(source.source)) {
			return nil, ErrManifestMismatch
		}
		used++
	}
	if used != len(byPath) {
		return nil, ErrManifestMismatch
	}
	return &Snapshot{manifest: manifest, manifestBytes: manifestBytes, files: byPath}, nil
}

// VerifiedIdentity checks the caller's byte budget without copying or hashing
// the manifest again. Only Freeze can establish this identity.
func (s *Snapshot) VerifiedIdentity(maxBytes int) (Identity, error) {
	if s == nil || s.manifestBytes < 1 || s.manifestBytes > maxBytes {
		return Identity{}, ErrManifestMismatch
	}
	m := s.manifest
	return Identity{m.WorktreeID, m.ProfileID, m.PolicyDigest, m.ProviderDigest, m.SnapshotDigest}, nil
}

// Manifest returns a deep copy, including the nullable scalar fields.
func (s *Snapshot) Manifest() model.CaptureManifest {
	if s == nil {
		return model.CaptureManifest{}
	}
	return copyManifest(s.manifest)
}

// File returns an immutable value; its Bytes and Range methods return copies.
func (s *Snapshot) File(path string) (File, bool) {
	if s == nil {
		return File{}, false
	}
	file, ok := s.files[path]
	return file, ok
}

func copyManifest(input model.CaptureManifest) model.CaptureManifest {
	output := input
	if input.Files != nil {
		output.Files = append([]model.ManifestFile{}, input.Files...)
		for i, file := range output.Files {
			if file.ContentHash != nil {
				value := *file.ContentHash
				output.Files[i].ContentHash = &value
			}
			if file.ByteLength != nil {
				value := *file.ByteLength
				output.Files[i].ByteLength = &value
			}
			if file.ReasonCode != nil {
				value := *file.ReasonCode
				output.Files[i].ReasonCode = &value
			}
		}
	}
	if input.DependencyInputDigests != nil {
		output.DependencyInputDigests = append([]string{}, input.DependencyInputDigests...)
	}
	return output
}
