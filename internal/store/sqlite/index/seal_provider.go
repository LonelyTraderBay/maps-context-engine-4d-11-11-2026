package index

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

type SealProviderRequest struct {
	Capture             CaptureWrite
	Stream              ProviderStageStream
	Done                model.ProviderDone
	AuthorizeGeneration GenerationAuthorizer
	MaxFrameBytes       int
}

// SealProviderStage accepts one terminal frame only after all previously
// accepted batches, validates source references, and binds the terminal claim
// to the captured digest. It does not prove analyzer completeness or publish.
func (s *Store) SealProviderStage(ctx context.Context, request SealProviderRequest) (resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || request.Stream.shared == nil || capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || capture.Writer == nil || request.AuthorizeGeneration == nil || request.MaxFrameBytes < 65536 || request.MaxFrameBytes > 16*1024*1024 || capture.MaxSourceBytes < 1 || capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	shared := request.Stream.shared
	select {
	case <-shared.gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { shared.gate <- struct{}{} }()
	s.streamsMu.Lock()
	registered := s.streams[shared.generationID] == shared
	s.streamsMu.Unlock()
	if !registered || shared.uncertain || shared.done != nil {
		return ErrGenerationConflict
	}
	manifest := capture.Snapshot.Manifest()
	if shared.jobID != capture.JobID || shared.generationID != capture.GenerationID || shared.digest != manifest.SnapshotDigest || manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil {
		return ErrCaptureMismatch
	}
	if request.Done.Error != nil {
		return ErrCaptureMismatch
	}
	if !providerDoneWithinBudget(request.Done, request.MaxFrameBytes) {
		return ErrCaptureLimit
	}
	raw, err := json.Marshal(request.Done)
	if err != nil {
		return err
	}
	done, err := model.DecodeProviderDoneJSON(raw, request.MaxFrameBytes)
	if err != nil || done.JobID != capture.JobID {
		return ErrCaptureMismatch
	}
	next, err := model.AcceptProviderOutput(shared.state, &done, request.MaxFrameBytes)
	if err != nil {
		return ErrCaptureMismatch
	}
	if done.Status != "complete_in_supported_scope" && done.Status != "partial" || done.Error != nil || len(done.Capabilities) == 0 {
		return ErrCaptureMismatch
	}
	partial := len(done.Gaps) > 0
	seen := map[string]bool{}
	for _, capability := range done.Capabilities {
		if seen[capability.Name] {
			return ErrCaptureMismatch
		}
		seen[capability.Name] = true
		for _, value := range []string{capability.Name, capability.ProviderID, capability.ProviderVersion, capability.Status} {
			if err := capture.Scanner.CheckContent([]byte(value)); err != nil {
				return err
			}
		}
		if capability.Status != "supported_scope_complete" {
			partial = true
		}
	}
	if done.Status == "complete_in_supported_scope" && partial || done.Status == "partial" && (!partial || len(done.Gaps) == 0) {
		return ErrCaptureMismatch
	}
	paths := map[string]bool{}
	var total int64
	for _, gap := range done.Gaps {
		for _, value := range []string{gap.Code, gap.Reason} {
			if err := capture.Scanner.CheckContent([]byte(value)); err != nil {
				return err
			}
		}
		for _, ref := range gap.SourceRefs {
			if ref.WorktreeID != manifest.WorktreeID || ref.ProfileID != manifest.ProfileID || ref.GenerationID != capture.GenerationID || policy.CheckPath(ref.Path) != nil {
				return ErrCaptureMismatch
			}
			if err := capture.Scanner.CheckContent([]byte(ref.Path)); err != nil {
				return err
			}
			if err := capture.Authorize(ctx, s.identity.WorktreeID, ref.Path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
				return err
			}
			file, ok := capture.Snapshot.File(ref.Path)
			if !ok || file.ContentHash() != ref.ContentHash || ref.Range.StartByte < 0 || ref.Range.EndByte < ref.Range.StartByte || ref.Range.EndByte > int64(file.Len()) {
				return ErrCaptureMismatch
			}
			if !paths[ref.Path] {
				if int64(file.Len()) > capture.MaxSourceBytes-total {
					return ErrCaptureLimit
				}
				total += int64(file.Len())
			}
			if err := file.CheckRange(int(ref.Range.StartByte), int(ref.Range.EndByte)); err != nil {
				return ErrCaptureMismatch
			}
			paths[ref.Path] = true
		}
	}
	guard, err := capture.Writer.HoldWriter(ctx, s.identity.WorktreeID, capture.JobID)
	if err != nil {
		return err
	}
	if guard == nil {
		return ErrInvalidStore
	}
	defer func() {
		if err := guard.Close(); err != nil {
			shared.uncertain = true
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return err
	}
	outcome, err := s.PublicationOutcomeByJob(ctx, PublicationOutcomeRequest{GenerationID: capture.GenerationID, JobID: capture.JobID, PolicyEpoch: capture.PolicyEpoch, Snapshot: capture.Snapshot, Authorize: request.AuthorizeGeneration, MaxManifestBytes: capture.MaxManifestBytes})
	if err != nil {
		return err
	}
	if outcome.Status != "building" {
		return ErrGenerationConflict
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		if err := capture.Authorize(ctx, s.identity.WorktreeID, path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
	}
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	shared.state = next
	shared.done = &done
	return nil
}

func providerDoneWithinBudget(done model.ProviderDone, limit int) bool {
	if done.Error != nil || len(done.Capabilities) > 128 || len(done.Gaps) > 64 {
		return false
	}
	total := 512 + (len(done.Capabilities)+len(done.Gaps))*512
	add := func(value string) bool {
		if len(value) > (limit-total)/6 {
			return false
		}
		total += len(value) * 6
		return true
	}
	if total > limit || !add(done.JobID) || !add(done.Type) || !add(done.Status) || !add(done.InputDigest) {
		return false
	}
	for _, c := range done.Capabilities {
		for _, v := range []string{c.Name, c.ProviderID, c.ProviderVersion, c.Status} {
			if !add(v) {
				return false
			}
		}
	}
	for _, gap := range done.Gaps {
		if len(gap.SourceRefs) > 16 || !add(gap.Code) || !add(gap.Reason) {
			return false
		}
		for _, ref := range gap.SourceRefs {
			if total > limit-512 {
				return false
			}
			total += 512
			for _, v := range []string{ref.WorktreeID, ref.ProfileID, ref.GenerationID, ref.Path, ref.ContentHash} {
				if !add(v) {
					return false
				}
			}
		}
	}
	return true
}
