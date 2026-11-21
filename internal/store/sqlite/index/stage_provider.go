package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type ProviderStageStream struct {
	shared *providerStageState
}

type providerStageState struct {
	gate         chan struct{}
	state        model.ProviderStreamState
	jobID        string
	generationID string
	digest       string
	uncertain    bool
	done         *model.ProviderDone
	reservation  ports.LeaseGuard
}

// BeginProviderStage binds one bounded stream to an existing BUILDING job.
func (s *Store) BeginProviderStage(ctx context.Context, capture CaptureWrite, authorize GenerationAuthorizer) (output ProviderStageStream, resultErr error) {
	if s == nil || ctx == nil || capture.Writer == nil || capture.Snapshot == nil || authorize == nil || !validUUID(capture.JobID) || !validUUID(capture.GenerationID) || capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 {
		return output, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return output, ErrInvalidStore
	}
	streamLease, ok := capture.Writer.(ports.IndexStreamLease)
	if !ok {
		return output, ErrInvalidStore
	}
	reservation, err := streamLease.HoldIndexStream(ctx, s.identity.WorktreeID, capture.JobID)
	if errors.Is(err, ports.ErrIndexStreamBusy) {
		return output, ErrGenerationConflict
	}
	if err != nil {
		return output, err
	}
	if reservation == nil {
		return output, ErrInvalidStore
	}
	keepReservation := false
	defer func() {
		if !keepReservation {
			resultErr = errors.Join(resultErr, reservation.Close())
		}
	}()
	guard, err := capture.Writer.HoldWriter(ctx, s.identity.WorktreeID, capture.JobID)
	if err != nil {
		return output, err
	}
	if guard == nil {
		return output, ErrInvalidStore
	}
	defer func() {
		if err := guard.Close(); err != nil {
			if output.shared != nil {
				s.streamsMu.Lock()
				if s.streams[capture.GenerationID] == output.shared {
					delete(s.streams, capture.GenerationID)
				}
				s.streamsMu.Unlock()
				output = ProviderStageStream{}
				keepReservation = false
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return output, err
	}
	outcome, err := s.PublicationOutcomeByJob(ctx, PublicationOutcomeRequest{GenerationID: capture.GenerationID, JobID: capture.JobID, PolicyEpoch: capture.PolicyEpoch, Snapshot: capture.Snapshot, Authorize: authorize, MaxManifestBytes: capture.MaxManifestBytes})
	if err != nil {
		return output, err
	}
	if outcome.Status != "building" {
		return output, ErrGenerationConflict
	}
	manifest := capture.Snapshot.Manifest()
	state, err := model.BeginProviderStream(capture.JobID, manifest.SnapshotDigest)
	if err != nil {
		return output, err
	}
	shared := &providerStageState{gate: make(chan struct{}, 1), state: state, jobID: capture.JobID, generationID: capture.GenerationID, digest: manifest.SnapshotDigest, reservation: reservation}
	shared.gate <- struct{}{}
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if s.streams == nil {
		s.streams = make(map[string]*providerStageState)
	}
	if _, ok := s.streams[capture.GenerationID]; ok {
		return output, ErrGenerationConflict
	}
	if len(s.streams) >= 256 {
		return output, ErrCaptureLimit
	}
	s.streams[capture.GenerationID] = shared
	keepReservation = true
	return ProviderStageStream{shared: shared}, nil
}

// ReleaseProviderStage frees the bounded registry entry only after the same
// job's generation is observed terminal. A BUILDING or unavailable outcome
// remains reserved so an uncertain stream cannot be silently restarted.
func (s *Store) ReleaseProviderStage(ctx context.Context, capture CaptureWrite, stream ProviderStageStream, authorize GenerationAuthorizer) error {
	if s == nil || ctx == nil || stream.shared == nil || authorize == nil {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	shared := stream.shared
	select {
	case <-shared.gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { shared.gate <- struct{}{} }()
	if shared.jobID != capture.JobID || shared.generationID != capture.GenerationID || capture.Snapshot == nil || shared.digest != capture.Snapshot.Manifest().SnapshotDigest {
		return ErrCaptureMismatch
	}
	outcome, err := s.PublicationOutcomeByJob(ctx, PublicationOutcomeRequest{GenerationID: capture.GenerationID, JobID: capture.JobID, PolicyEpoch: capture.PolicyEpoch, Snapshot: capture.Snapshot, Authorize: authorize, MaxManifestBytes: capture.MaxManifestBytes})
	if err != nil {
		return err
	}
	switch outcome.Status {
	case "published_active", "published_historical", "retired", "failed", "cancelled":
	default:
		return ErrGenerationConflict
	}
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if s.streams[shared.generationID] != shared {
		return ErrGenerationConflict
	}
	if err := shared.reservation.Close(); err != nil {
		shared.uncertain = true
		return err
	}
	delete(s.streams, shared.generationID)
	return nil
}

type StageProviderRequest struct {
	Capture       CaptureWrite
	Stream        ProviderStageStream
	Batch         model.ProviderBatch
	MaxFrameBytes int
}

// StageProviderBatch validates a single ordered provider frame against the
// captured inputs, then stages it atomically under the job's writer lease.
// The caller owns the stream state and must reconcile an uncertain commit;
// successful staging is not proof that the provider analyzed the right input.
func (s *Store) StageProviderBatch(ctx context.Context, request StageProviderRequest) (next ProviderStageStream, resultErr error) {
	capture := request.Capture
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) || capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || capture.Writer == nil ||
		!validUUID(capture.GenerationID) || !validUUID(capture.JobID) || capture.PolicyEpoch < 1 || capture.PolicyEpoch > 9007199254740991 || capture.MaxManifestBytes < 1 || capture.MaxManifestBytes > 64*1024*1024 || capture.MaxSourceBytes < 1 || request.MaxFrameBytes < 65536 || request.MaxFrameBytes > 16*1024*1024 || request.Stream.shared == nil {
		return next, ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return next, ErrInvalidStore
	}
	shared := request.Stream.shared
	select {
	case <-shared.gate:
	case <-ctx.Done():
		return next, ctx.Err()
	}
	defer func() { shared.gate <- struct{}{} }()
	s.streamsMu.Lock()
	registered := s.streams[shared.generationID] == shared
	s.streamsMu.Unlock()
	if !registered {
		return next, ErrGenerationConflict
	}
	if shared.uncertain {
		return next, ErrCommitUnknown
	}
	manifest := capture.Snapshot.Manifest()
	if manifest.WorktreeID != s.identity.WorktreeID || model.VerifyCaptureDigest(manifest, capture.MaxManifestBytes) != nil || shared.jobID != capture.JobID || shared.generationID != capture.GenerationID || shared.digest != manifest.SnapshotDigest {
		return next, ErrCaptureMismatch
	}
	if !providerBatchWithinBudget(request.Batch, request.MaxFrameBytes) {
		return next, ErrCaptureLimit
	}
	raw, err := json.Marshal(request.Batch)
	if err != nil {
		return next, err
	}
	batch, err := model.DecodeProviderBatchJSON(raw, request.MaxFrameBytes)
	if err != nil || batch.JobID != capture.JobID {
		return next, ErrCaptureMismatch
	}
	validated, err := model.AcceptProviderOutput(shared.state, &batch, request.MaxFrameBytes)
	if err != nil {
		return next, ErrCaptureMismatch
	}
	paths, err := validateProviderBatch(ctx, s.identity.WorktreeID, capture, manifest, batch)
	if err != nil {
		return next, err
	}
	committed := false
	guard, err := capture.Writer.HoldWriter(ctx, s.identity.WorktreeID, capture.JobID)
	if err != nil {
		return next, err
	}
	if guard == nil {
		return next, ErrInvalidStore
	}
	defer func() {
		if err := guard.Close(); err != nil {
			shared.uncertain = true
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return next, err
	}
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return next, err
	}
	conn, err := borrowConfigured(ctx, s.writer, false)
	if err != nil {
		return next, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, false); err != nil {
			shared.uncertain = true
			if committed {
				err = errors.Join(ErrAfterCommit, err)
			}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return next, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return next, err
	}
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				shared.uncertain = true
				resultErr = errors.Join(resultErr, ErrCommitUnknown, err)
			}
		}
	}()
	_, err = s.inspectPinned(ctx, tx)
	if err != nil {
		return next, err
	}
	if err := verifyBuildingGeneration(ctx, tx, capture, manifest); err != nil {
		return next, err
	}
	for _, item := range batch.Packages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO packages(generation_id,package_key,import_path,module_key,variant,input_digest,semantic_status) VALUES(?,?,?,?,?,?,?)`, capture.GenerationID, item.PackageKey, item.ImportPath, item.ModuleKey, item.Variant, item.InputDigest, item.SemanticStatus); err != nil {
			return next, err
		}
	}
	for _, item := range batch.Symbols {
		var start, end any
		if item.Range != nil {
			start = item.Range.StartByte
			end = item.Range.EndByte
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO symbols(generation_id,symbol_id,path,package_key,kind,name,qualified_name,scope_key,start_byte,end_byte,signature,exported) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, capture.GenerationID, item.SymbolID, item.Path, item.PackageKey, item.Kind, item.Name, item.QualifiedName, item.ScopeKey, start, end, item.Signature, boolInt(item.Exported)); err != nil {
			return next, err
		}
	}
	for _, item := range batch.Relations {
		if _, err := tx.ExecContext(ctx, `INSERT INTO relations(generation_id,relation_id,source_symbol_id,target_symbol_id,kind,evidence_kind,site_path,start_byte,end_byte,reason,unresolved_label) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, capture.GenerationID, item.RelationID, item.SourceSymbolID, item.TargetSymbolID, item.Kind, item.EvidenceKind, item.SitePath, item.Range.StartByte, item.Range.EndByte, item.Reason, item.UnresolvedLabel); err != nil {
			return next, err
		}
	}
	for _, item := range batch.Diagnostics {
		scope, err := json.Marshal(struct {
			ScopeDescription string `json:"scope_description"`
		}{item.ScopeDescription})
		if err != nil {
			return next, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO diagnostics(generation_id,diagnostic_id,path,package_key,code,severity,sanitized_message,scope_json) VALUES(?,?,?,?,?,?,?,?)`, capture.GenerationID, item.DiagnosticID, item.Path, item.PackageKey, item.Code, item.Severity, item.Message, string(scope)); err != nil {
			return next, err
		}
	}
	for _, path := range paths {
		if err := capture.Authorize(ctx, s.identity.WorktreeID, path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return next, err
		}
	}
	if err := capture.Writer.CheckWriter(ctx, s.identity.WorktreeID, capture.JobID); err != nil {
		return next, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return next, err
	}
	if err := tx.Commit(); err != nil {
		shared.uncertain = true
		return next, errors.Join(ErrCommitUnknown, err)
	}
	committed = true
	shared.state = validated
	return request.Stream, nil
}

// Bound JSON materialization before marshal, including worst-case escaping.
func providerBatchWithinBudget(batch model.ProviderBatch, limit int) bool {
	if len(batch.Packages) > 512 || len(batch.Symbols) > 2048 || len(batch.Relations) > 4096 || len(batch.Diagnostics) > 512 {
		return false
	}
	total := 512 + (len(batch.Packages)+len(batch.Symbols)+len(batch.Relations)+len(batch.Diagnostics))*512
	add := func(value string) bool {
		if len(value) > (limit-total)/6 {
			return false
		}
		total += len(value) * 6
		return true
	}
	if total > limit || !add(batch.JobID) || !add(batch.Type) {
		return false
	}
	for _, x := range batch.Packages {
		for _, v := range []string{x.PackageKey, x.ImportPath, x.ModuleKey, x.Variant, x.InputDigest, x.SemanticStatus} {
			if !add(v) {
				return false
			}
		}
	}
	for _, x := range batch.Symbols {
		for _, v := range []string{x.SymbolID, x.Kind, x.Name, x.QualifiedName, x.ScopeKey, x.Signature} {
			if !add(v) {
				return false
			}
		}
		if x.Path != nil && !add(*x.Path) {
			return false
		}
		if x.PackageKey != nil && !add(*x.PackageKey) {
			return false
		}
	}
	for _, x := range batch.Relations {
		for _, v := range []string{x.RelationID, x.SourceSymbolID, x.Kind, x.EvidenceKind, x.SitePath, x.Reason} {
			if !add(v) {
				return false
			}
		}
		if x.TargetSymbolID != nil && !add(*x.TargetSymbolID) {
			return false
		}
		if x.UnresolvedLabel != nil && !add(*x.UnresolvedLabel) {
			return false
		}
	}
	for _, x := range batch.Diagnostics {
		for _, v := range []string{x.DiagnosticID, x.Code, x.Severity, x.Message, x.ScopeDescription} {
			if !add(v) {
				return false
			}
		}
		if x.Path != nil && !add(*x.Path) {
			return false
		}
		if x.PackageKey != nil && !add(*x.PackageKey) {
			return false
		}
	}
	return true
}

func validateProviderBatch(ctx context.Context, worktree string, capture CaptureWrite, manifest model.CaptureManifest, batch model.ProviderBatch) ([]string, error) {
	known := make(map[string]bool, len(manifest.Files))
	for _, item := range manifest.Files {
		known[item.Path] = true
	}
	return validateProviderBatchWithKnown(ctx, worktree, capture, manifest, known, batch)
}

func validateProviderBatchWithKnown(ctx context.Context, worktree string, capture CaptureWrite, manifest model.CaptureManifest, known map[string]bool, batch model.ProviderBatch) ([]string, error) {
	paths := make(map[string]bool)
	var sourceBytes int64
	verifyPath := func(path string, captured bool, start, end int64) error {
		if !known[path] || policy.CheckPath(path) != nil {
			return ErrCaptureMismatch
		}
		if err := capture.Scanner.CheckContent([]byte(path)); err != nil {
			return err
		}
		if err := capture.Authorize(ctx, worktree, path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
			return err
		}
		if captured {
			file, ok := capture.Snapshot.File(path)
			if !ok || start < 0 || end < start || end > int64(file.Len()) {
				return ErrCaptureMismatch
			}
			if !paths[path] {
				if int64(file.Len()) > capture.MaxSourceBytes-sourceBytes {
					return ErrCaptureLimit
				}
				sourceBytes += int64(file.Len())
			}
			if err := file.CheckRange(int(start), int(end)); err != nil {
				return ErrCaptureMismatch
			}
		}
		paths[path] = true
		return nil
	}
	checkText := func(values ...string) error {
		for _, value := range values {
			if err := capture.Scanner.CheckContent([]byte(value)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, item := range batch.Packages {
		if err := checkText(item.PackageKey, item.ImportPath, item.ModuleKey); err != nil {
			return nil, err
		}
	}
	for _, item := range batch.Symbols {
		if err := checkText(item.SymbolID, item.Kind, item.Name, item.QualifiedName, item.ScopeKey, item.Signature); err != nil {
			return nil, err
		}
		if item.PackageKey != nil {
			if err := checkText(*item.PackageKey); err != nil {
				return nil, err
			}
		}
		if item.Path != nil {
			if item.Range == nil {
				return nil, ErrCaptureMismatch
			}
			if err := verifyPath(*item.Path, true, item.Range.StartByte, item.Range.EndByte); err != nil {
				return nil, err
			}
		}
	}
	for _, item := range batch.Relations {
		if !validRelationEvidence(item) {
			return nil, ErrCaptureMismatch
		}
		if err := checkText(item.RelationID, item.SourceSymbolID, item.Kind, item.EvidenceKind, item.Reason); err != nil {
			return nil, err
		}
		if item.UnresolvedLabel != nil {
			if err := checkText(*item.UnresolvedLabel); err != nil {
				return nil, err
			}
		}
		if item.TargetSymbolID != nil {
			if err := checkText(*item.TargetSymbolID); err != nil {
				return nil, err
			}
		}
		if err := verifyPath(item.SitePath, true, item.Range.StartByte, item.Range.EndByte); err != nil {
			return nil, err
		}
	}
	for _, item := range batch.Diagnostics {
		if err := checkText(item.DiagnosticID, item.Code, item.Message, item.ScopeDescription); err != nil {
			return nil, err
		}
		if item.Path != nil {
			if err := verifyPath(*item.Path, false, 0, 0); err != nil {
				return nil, err
			}
		}
		if item.PackageKey != nil {
			if err := checkText(*item.PackageKey); err != nil {
				return nil, err
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func validRelationEvidence(item model.RelationFact) bool {
	switch item.Kind {
	case "CALLS_DIRECT", "CALLS_INTERFACE":
		if item.EvidenceKind != "type_resolved" {
			return false
		}
	case "MAY_DISPATCH_TO":
		if item.EvidenceKind != "type_possible" {
			return false
		}
	case "DOC_LINK":
		if item.EvidenceKind != "explicit_document" {
			return false
		}
	}
	if item.EvidenceKind == "type_possible" && item.Kind != "MAY_DISPATCH_TO" {
		return false
	}
	if item.EvidenceKind == "explicit_document" && item.Kind != "DOC_LINK" {
		return false
	}
	return true
}
