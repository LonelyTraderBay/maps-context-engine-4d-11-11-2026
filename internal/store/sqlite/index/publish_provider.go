package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

type PublishProviderRequest struct {
	Capture             CaptureWrite
	Stream              ProviderStageStream
	AuthorizeGeneration GenerationAuthorizer
	Revalidate          func(context.Context, *snapshot.Snapshot) error
	VerifyProvider      func(context.Context, *snapshot.Snapshot, model.ProviderDone) error
	MaxFacts            int
	MaxFrameBytes       int
}

// PublishProvider validates staged facts and a sealed provider stream before
// atomically switching the active pointer. VerifyProvider is a required trust
// boundary: it must establish that the provider actually consumed the frozen
// inputs. Search documents are independently checked against captured bytes.
func (s *Store) PublishProvider(ctx context.Context, request PublishProviderRequest) (resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || request.Stream.shared == nil || capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || capture.Writer == nil || capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 || capture.MaxSourceBytes < 1 || request.AuthorizeGeneration == nil || request.Revalidate == nil || request.VerifyProvider == nil || request.MaxFacts < 1 || request.MaxFacts > 1000000 || request.MaxFrameBytes < 65536 || request.MaxFrameBytes > 16*1024*1024 {
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
	if !registered || shared.done == nil {
		return ErrGenerationConflict
	}
	if shared.uncertain {
		return ErrCommitUnknown
	}
	manifest := capture.Snapshot.Manifest()
	if shared.jobID != capture.JobID || shared.generationID != capture.GenerationID || shared.digest != manifest.SnapshotDigest || manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil {
		return ErrCaptureMismatch
	}
	rawDone, err := json.Marshal(shared.done)
	if err != nil {
		return err
	}
	if len(rawDone) > request.MaxFrameBytes {
		return ErrCaptureLimit
	}
	done, err := model.DecodeProviderDoneJSON(rawDone, request.MaxFrameBytes)
	if err != nil {
		return ErrPublishIntegrity
	}
	if done.JobID != capture.JobID || done.VerifyInputDigest(manifest.SnapshotDigest) != nil {
		return ErrCaptureMismatch
	}
	state, coverageStatus := "READY_PARTIAL", "partial"
	if done.Status == "complete_in_supported_scope" {
		state, coverageStatus = "READY", "supported_scope_complete"
		for _, item := range manifest.Files {
			if item.Status != "captured" {
				return ErrPublishIntegrity
			}
		}
	} else if done.Status != "partial" {
		return ErrPublishIntegrity
	}
	coverage := model.Coverage{Status: coverageStatus, Capabilities: done.Capabilities, Gaps: done.Gaps, ScopeDescription: "workspace"}
	summary, err := json.Marshal(coverage)
	if err != nil {
		return err
	}
	if len(summary) > request.MaxFrameBytes {
		return ErrCaptureLimit
	}
	if _, err := model.DecodeCoverageJSON(summary, request.MaxFrameBytes); err != nil {
		return ErrPublishIntegrity
	}
	verify := func(ctx context.Context, captured *snapshot.Snapshot) error {
		copyDone, err := model.DecodeProviderDoneJSON(rawDone, request.MaxFrameBytes)
		if err != nil {
			return ErrPublishIntegrity
		}
		return request.VerifyProvider(ctx, captured, copyDone)
	}
	plan := publishPlan{Capture: capture, AuthorizeGeneration: request.AuthorizeGeneration, State: state, AllowParseErrors: state == "READY_PARTIAL",
		Revalidate: func(ctx context.Context, captured *snapshot.Snapshot) error {
			if err := verify(ctx, captured); err != nil {
				return err
			}
			if err := request.Revalidate(ctx, captured); err != nil {
				return err
			}
			return verify(ctx, captured)
		},
		ValidateExtra: func(ctx context.Context, conn publicationQueryer, current model.CaptureManifest) error {
			if err := checkProviderRowBudget(ctx, conn, capture.GenerationID, request.MaxFacts); err != nil {
				return err
			}
			if err := validateStoredProviderFacts(ctx, conn, s.identity.WorktreeID, capture, current, request.MaxFacts); err != nil {
				return err
			}
			if err := checkFactRanges(ctx, conn, s.identity.WorktreeID, capture, current, request.MaxFacts); err != nil {
				return err
			}
			maxModules := request.MaxFacts
			if maxModules > 100000 {
				maxModules = 100000
			}
			if err := checkModuleAcyclic(ctx, conn, capture.GenerationID, maxModules, capture.Scanner); err != nil {
				return err
			}
			return checkProviderCompleteness(ctx, conn, capture.GenerationID, state, len(done.Gaps))
		},
		PersistExtra: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO capability_results(generation_id,capability_key,scope_key,status,summary_json) VALUES(?,'provider_coverage','workspace',?,?)`, capture.GenerationID, coverageStatus, string(summary))
			return err
		},
	}
	err = s.publish(ctx, plan)
	if errors.Is(err, ErrCommitUnknown) || errors.Is(err, ErrAfterCommit) {
		shared.uncertain = true
	}
	return err
}

func checkProviderRowBudget(ctx context.Context, db publicationQueryer, generation string, max int) error {
	var count int64
	err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM packages WHERE generation_id=?)+(SELECT count(*) FROM symbols WHERE generation_id=?)+(SELECT count(*) FROM relations WHERE generation_id=?)+(SELECT count(*) FROM diagnostics WHERE generation_id=?)+(SELECT count(*) FROM modules WHERE generation_id=?)+(SELECT count(*) FROM module_members WHERE generation_id=?)`, generation, generation, generation, generation, generation, generation).Scan(&count)
	if err != nil {
		return err
	}
	if count > int64(max) {
		return ErrCaptureLimit
	}
	return nil
}

func checkProviderCompleteness(ctx context.Context, db publicationQueryer, generation, state string, gapCount int) error {
	query := `SELECT 1 FROM capability_results WHERE generation_id=? UNION ALL SELECT 1 FROM files f WHERE f.generation_id=? AND f.status='parse_error' AND NOT EXISTS(SELECT 1 FROM diagnostics d WHERE d.generation_id=f.generation_id AND d.path=f.path AND d.code=f.reason_code AND d.severity='error')`
	args := []any{generation, generation}
	if gapCount == 0 {
		query += ` UNION ALL SELECT 1 FROM relations WHERE generation_id=? AND evidence_kind='unresolved' UNION ALL SELECT 1 FROM diagnostics WHERE generation_id=? AND severity='error'`
		args = append(args, generation, generation)
	}
	if state == "READY" {
		query += ` UNION ALL SELECT 1 FROM diagnostics WHERE generation_id=? AND severity='error' UNION ALL SELECT 1 FROM files WHERE generation_id=? AND status='parse_error' UNION ALL SELECT 1 FROM packages WHERE generation_id=? AND semantic_status!='complete_in_profile'`
		args = append(args, generation, generation, generation)
	}
	query += ` LIMIT 1`
	var found int
	err := db.QueryRowContext(ctx, query, args...).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrPublishIntegrity
}
