package snapshot

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var ErrSourcePrecondition = errors.New("source precondition does not match the pinned capture")

type ExpectedSource struct {
	Ref       model.SourceRef
	ServiceID *string
}

// ValidateCapturedSource checks identity, hash and byte-range boundaries in a
// pinned immutable snapshot. The caller must separately verify that the
// generation, current authority and live workspace are still valid.
func ValidateCapturedSource(scope model.ScopeVector, ref model.SourceRef, serviceID *string, captured *Snapshot, maxBytes int) error {
	return ValidateCapturedSources(context.Background(), scope, []ExpectedSource{{Ref: ref, ServiceID: serviceID}}, captured, maxBytes)
}

// ValidateCapturedSources verifies the capture once for a bounded set of
// references. It does not grant current authority or prove live freshness.
func ValidateCapturedSources(ctx context.Context, scope model.ScopeVector, sources []ExpectedSource, captured *Snapshot, maxBytes int) error {
	if ctx == nil || captured == nil || len(sources) < 1 || len(sources) > 256 || maxBytes < 1024 || maxBytes > 64*1024*1024 {
		return ErrSourcePrecondition
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifest := captured.Manifest()
	if model.VerifyCaptureDigest(manifest, maxBytes) != nil {
		return ErrSourcePrecondition
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		ref := source.Ref
		member, err := model.MatchPinnedSource(scope, ref, source.ServiceID, maxBytes)
		if err != nil || manifest.WorktreeID != member.WorktreeID || manifest.ProfileID != member.ProfileID || manifest.PolicyDigest != member.PolicyDigest || manifest.SnapshotDigest != member.SnapshotDigest {
			return ErrSourcePrecondition
		}
		file, ok := captured.File(ref.Path)
		if !ok || file.ContentHash() != ref.ContentHash || ref.Range.StartByte < 0 || ref.Range.EndByte < ref.Range.StartByte || ref.Range.EndByte > int64(file.Len()) || file.CheckRange(int(ref.Range.StartByte), int(ref.Range.EndByte)) != nil {
			return ErrSourcePrecondition
		}
	}
	return ctx.Err()
}
