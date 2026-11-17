package compose

import (
	"context"
	"errors"
	"go/token"
	"reflect"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

// RenameOwner is supplied by the trusted process owner. Check independently
// authenticates the approved provider fingerprint, principal, complete scope
// and toolchain on every use. Open additionally proves the selected build and
// typed package closure, copies only captured inputs into confined scratch,
// and provides a Session.Check that maintains those proofs and current hashes.
// A syntax-only index or a stored provider digest is not such a proof. Close
// owns session scratch lifetime and must be safe after a failed computation.
// Continuation and validation never call Open or start another language server.
type RenameOwner struct {
	Store          *lsp.ResultStore
	ProviderDigest string
	Check          func(context.Context, app.CorePrincipal, model.ScopeVector) error
	Open           func(context.Context, app.CorePrincipal, model.ScopeVector, model.RenameInitialRequest, *snapshot.Snapshot) (lsp.SessionConfig, func() error, error)
}

func (b *CoreBackend) WithRenamePreview(owner RenameOwner) (*CoreBackend, error) {
	if !b.valid() || b.verifyWorkspace == nil || b.buildGate == nil || owner.Store == nil || !hash(owner.ProviderDigest) || owner.Check == nil || owner.Open == nil || owner.Store.MaxResultBytes() > b.limits.RenameResultMaxBytes || !owner.Store.WithinRetentionLimits(b.Reader.WorktreeID, b.limits.RenameResultsMaxBytes, b.limits.MaxRetainedQueryResults, b.limits.RenameResultTtlSeconds) {
		return nil, app.ErrCoreInput
	}
	copy := *b
	copy.renameOwner = &owner
	return &copy, nil
}

func (b *CoreBackend) SupportsCLIRenameRetention() bool {
	return b != nil && b.valid() && b.renameOwner != nil && b.renameOwner.Store.MaxResultBytes() <= b.limits.RenameResultMaxBytes && b.renameOwner.Store.WithinRetentionLimits(b.Reader.WorktreeID, b.limits.RenameResultsMaxBytes, b.limits.MaxRetainedQueryResults, b.limits.RenameResultTtlSeconds)
}

func (b *CoreBackend) renameCursorGuard(p app.CorePrincipal) contextpack.CursorGuard {
	return func(ctx context.Context, binding contextpack.CursorBinding) error {
		if !b.SupportsCLIRenameRetention() || binding.PrincipalDigest != p.Digest || binding.BindingDigest != p.BindingDigest || binding.OrderVersion != "rename-v1" || binding.Purpose != "rename-result" && binding.Purpose != "rename-page" {
			return app.ErrValidationAuthority
		}
		if err := b.Authority.samePrincipal(ctx, p); err != nil {
			return err
		}
		if _, err := b.Reader.Recheck(ctx, p, binding.Scope); err != nil {
			return err
		}
		return b.renameOwner.Check(ctx, p, binding.Scope)
	}
}

// One operation holds one immutable capture. The guards still recheck live
// catalog/policy/record/incarnation and provider authority at each boundary.
type renameOperation struct {
	observed   ScopeObservation
	captured   *snapshot.Snapshot
	validation app.ValidateCapturedInput
	guard      lsp.ResultGuard
}

func (b *CoreBackend) openRenameOperation(ctx context.Context, p app.CorePrincipal, scope model.ScopeVector) (renameOperation, error) {
	if ctx == nil || !b.SupportsCLIRenameRetention() || !boundedSingleScope(scope) {
		return renameOperation{}, app.ErrCoreUnsupported
	}
	observation, err := b.Reader.Recheck(ctx, p, scope)
	if err != nil {
		return renameOperation{}, coreStoreError(err)
	}
	if err = b.renameOwner.Check(ctx, p, scope); err != nil {
		return renameOperation{}, coreStoreError(err)
	}
	member := scope.Members[0]
	maxFile := minInt64(observation.Binding.Policy.MaxFileBytes, b.limits.MaxFileBytes, 64<<10)
	maxSource := minInt64(observation.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot, 2<<20)
	if maxFile < 1 || maxSource < 1 {
		return renameOperation{}, app.ErrCoreLimit
	}
	scanner, err := policy.NewScanner(int(maxInt64(maxFile, int64(b.Reader.MaxManifestBytes), packFactLimit)))
	if err != nil {
		return renameOperation{}, err
	}
	authorizeGeneration := func(ctx context.Context, w, profile, digest string, epoch int64) error {
		if w != member.WorktreeID || profile != member.ProfileID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		return b.Authority.samePrincipal(ctx, p)
	}
	authorizePath := func(ctx context.Context, w, name, digest string, epoch int64) error {
		if w != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		if err := b.Authority.samePrincipal(ctx, p); err != nil {
			return err
		}
		return b.Authority.path(ctx, p, observation.Binding.Policy, name)
	}
	graph, err := b.Reader.Index.ReadPublishedGraph(ctx, index.PublishedGraphRequest{Member: member, VectorDigest: scope.VectorDigest, AuthorizeGeneration: authorizeGeneration, AuthorizePath: authorizePath, Scanner: scanner, MaxManifestBytes: b.Reader.MaxManifestBytes, MaxFileBytes: int(maxFile), MaxSourceBytes: maxSource, MaxFactBytes: packFactLimit, MaxFacts: 100000})
	if err != nil {
		return renameOperation{}, coreStoreError(err)
	}
	if len(graph.Snapshot.Manifest().Files) > 128 {
		return renameOperation{}, app.ErrCoreLimit
	}
	input := app.ValidateCapturedInput{Scope: scope, MaxInputBytes: 65536, MaxManifestBytes: b.Reader.MaxManifestBytes,
		CurrentScope: func(ctx context.Context) (model.ScopeVector, error) {
			current, err := b.Reader.Recheck(ctx, p, scope)
			return current.Vector, err
		},
		ResolveCapture: func(ctx context.Context, requested model.ScopeMember) (*snapshot.Snapshot, error) {
			if !reflect.DeepEqual(requested, member) {
				return nil, ErrScopeChanged
			}
			if _, err := b.Reader.Recheck(ctx, p, scope); err != nil {
				return nil, err
			}
			return graph.Snapshot, nil
		},
		AuthorizeSource: func(ctx context.Context, requested model.ScopeMember, ref model.SourceRef) error {
			if !reflect.DeepEqual(requested, member) {
				return ErrScopeChanged
			}
			return authorizePath(ctx, member.WorktreeID, ref.Path, member.PolicyDigest, member.PolicyEpoch)
		},
		VerifyWorkspace: func(ctx context.Context, requested model.ScopeMember, snap *snapshot.Snapshot) (app.WorkspaceObservation, error) {
			return b.verifyWorkspace(ctx, p, requested, snap)
		},
	}
	guard := func(ctx context.Context, result lsp.RenameStoredResult) error {
		if int64(len(result.Payload.Edits)) > b.limits.RenameMaxEdits {
			return app.ErrCoreLimit
		}
		if result.Header.PrincipalDigest != p.Digest || model.ComparePinnedScope(scope, result.Header.Scope, 65536) != nil {
			return app.ErrValidationAuthority
		}
		if err := b.renameOwner.Check(ctx, p, scope); err != nil {
			return err
		}
		_, err := app.ValidateRenameCaptured(ctx, result, b.renameOwner.ProviderDigest, input, int(b.renameOwner.Store.MaxResultBytes()))
		return err
	}
	return renameOperation{observation, graph.Snapshot, input, guard}, nil
}

func (b *CoreBackend) RenamePreview(ctx context.Context, p app.CorePrincipal, request model.Request, options app.CoreCallOptions) (blocks [][]byte, resultErr error) {
	if ctx == nil || !b.SupportsCLIRenameRetention() {
		return nil, app.ErrCoreUnsupported
	}
	release, err := b.acquireCLIPackWork(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	cursorGuard := b.renameCursorGuard(p)
	var scope model.ScopeVector
	switch r := request.(type) {
	case *model.RenameInitialRequest:
		if r == nil || !token.IsIdentifier(r.NewName) {
			return nil, app.ErrCoreInput
		}
		observed, err := b.resolveRequestScope(ctx, p, r.Scope)
		if err != nil {
			return nil, coreStoreError(err)
		}
		scope = observed.Vector
	case *model.RenameContinueRequest:
		if r == nil {
			return nil, app.ErrCoreInput
		}
		claims, err := b.Cursor.VerifyPrincipal(ctx, r.ResultToken, "rename-result", p.Digest, cursorGuard)
		if err != nil {
			return nil, coreStoreError(err)
		}
		scope = claims.Binding.Scope
	default:
		return nil, app.ErrCoreInput
	}
	op, err := b.openRenameOperation(ctx, p, scope)
	if err != nil {
		return nil, err
	}
	cap := minInt64(options.MaxOutputBytes, op.observed.Binding.Policy.MaxOutputBytes)
	effectiveMax := minInt64(b.limits.MaxOutputBytes, op.observed.Binding.Policy.MaxOutputBytes)
	wire := lsp.RenameWireOptions{RequestID: options.RequestID, MaxOutputBytes: cap, EffectiveMaxOutput: effectiveMax, DeliveryMode: options.DeliveryMode, VerifyStructuredHost: options.VerifyStructuredHost}
	owner := b.renameOwner
	switch r := request.(type) {
	case *model.RenameContinueRequest:
		bounded := *r
		if bounded.MaxOutputBytes != nil && *bounded.MaxOutputBytes > cap {
			bounded.MaxOutputBytes = &cap
		}
		_, blocks, err = lsp.RenameContinue(ctx, owner.Store, b.Cursor, bounded, p.Digest, cursorGuard, op.guard, wire)
		return blocks, coreStoreError(err)
	case *model.RenameInitialRequest:
		session, closeSession, err := owner.Open(ctx, p, scope, *r, op.captured)
		if err != nil || closeSession == nil || session.Check == nil {
			if closeSession != nil {
				err = errors.Join(err, closeSession())
			}
			if err == nil {
				err = app.ErrCoreBackend
			}
			return nil, coreStoreError(err)
		}
		finished := false
		var finishErr error
		finish := func() error {
			if !finished {
				finished = true
				finishErr = closeSession()
			}
			return finishErr
		}
		defer func() {
			resultErr = errors.Join(resultErr, finish())
			if resultErr != nil {
				blocks = nil
			}
		}()
		check := session.Check
		session.Check = func(ctx context.Context) error {
			if !finished {
				if err := check(ctx); err != nil {
					return err
				}
			} else {
				if finishErr != nil {
					return finishErr
				}
				state, err := b.verifyWorkspace(ctx, p, scope.Members[0], op.captured)
				if err != nil {
					return err
				}
				if state != app.WorkspaceObservedCurrent {
					return app.ErrFreshnessChanged
				}
			}
			if err := owner.Check(ctx, p, scope); err != nil {
				return err
			}
			_, err := b.Reader.Recheck(ctx, p, scope)
			return err
		}
		bounded := *r
		if bounded.MaxOutputBytes != nil && *bounded.MaxOutputBytes > cap {
			bounded.MaxOutputBytes = &cap
		}
		_, blocks, err = lsp.RenameInitial(ctx, lsp.RenameInitialInput{Session: session, Scope: scope, PrincipalID: p.ID, PrincipalDigest: p.Digest, BindingDigest: p.BindingDigest, ProviderDigest: owner.ProviderDigest, Request: bounded, Store: owner.Store, Codec: b.Cursor, CursorGuard: cursorGuard, ResultGuard: op.guard, Wire: wire, FinishSession: func(context.Context) error { return finish() }})
		return blocks, coreStoreError(err)
	}
	return nil, app.ErrCoreInput
}

func (b *CoreBackend) validateRename(ctx context.Context, p app.CorePrincipal, token string, options app.CoreCallOptions) ([][]byte, error) {
	if ctx == nil || !b.SupportsCLIRenameRetention() {
		return nil, app.ErrCoreUnsupported
	}
	release, err := b.acquireCLIPackWork(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	cursorGuard := b.renameCursorGuard(p)
	claims, err := b.Cursor.VerifyPrincipal(ctx, token, "rename-result", p.Digest, cursorGuard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	op, err := b.openRenameOperation(ctx, p, claims.Binding.Scope)
	if err != nil {
		return nil, err
	}
	header, err := b.renameOwner.Store.ValidateResultToken(ctx, b.Cursor, token, p.Digest, cursorGuard, op.guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	stored, err := b.renameOwner.Store.Read(ctx, header.ResultID, p.Digest, header.Scope, op.guard)
	if err != nil {
		return nil, coreStoreError(err)
	}
	validation, err := app.ValidateRenameWorkspace(ctx, stored, b.renameOwner.ProviderDigest, op.validation, int(b.renameOwner.Store.MaxResultBytes()))
	if err != nil {
		return nil, coreStoreError(err)
	}
	if _, err = b.renameOwner.Store.ValidateResultToken(ctx, b.Cursor, token, p.Digest, cursorGuard, op.guard); err != nil {
		return nil, coreStoreError(err)
	}
	options.MaxOutputBytes = minInt64(options.MaxOutputBytes, op.observed.Binding.Policy.MaxOutputBytes)
	response := baseResponse(options, header.Scope)
	response.Freshness, response.Data = validation.Freshness, validation.Data
	return encodeResponse(response)
}

// Reauthenticate the installed result after serialization, and bind every
// page cursor to that exact computation. This never extends expiry or invokes
// the provider. A validate response also repeats the live workspace check.
func (b *CoreBackend) verifyRenameDelivery(ctx context.Context, p app.CorePrincipal, request model.Request, response model.Response) error {
	if !b.SupportsCLIRenameRetention() || response.Scope == nil {
		return app.ErrCoreUnsupported
	}
	release, err := b.acquireCLIPackWork(ctx)
	if err != nil {
		return err
	}
	defer release()
	token, expectedDigest := "", ""
	var previous *string
	validate := false
	switch r := request.(type) {
	case *model.RenameInitialRequest:
		if r == nil {
			return app.ErrCoreInput
		}
		fingerprint := *r
		fingerprint.MaxOutputBytes = nil
		expectedDigest, err = model.RequestDigest("rename_preview", p.ID, "RenameRequest", &fingerprint, 32<<20, 4)
	case *model.RenameContinueRequest:
		if r == nil {
			return app.ErrCoreInput
		}
		token, previous = r.ResultToken, &r.Cursor
	case model.ValidateRequest:
		if r.ResultToken == nil || r.PackToken != nil {
			return app.ErrCoreInput
		}
		token, validate = *r.ResultToken, true
	default:
		return app.ErrCoreInput
	}
	if err != nil {
		return err
	}
	if !validate {
		data, ok := response.Data.(model.RenameData)
		if !ok || token != "" && token != data.ResultToken {
			return app.ErrCoreBackend
		}
		token = data.ResultToken
	}
	guard := b.renameCursorGuard(p)
	claims, err := b.Cursor.VerifyPrincipal(ctx, token, "rename-result", p.Digest, guard)
	if err != nil {
		return coreStoreError(err)
	}
	if expectedDigest != "" && claims.Binding.RequestDigest != expectedDigest || model.ComparePinnedScope(*response.Scope, claims.Binding.Scope, 65536) != nil {
		return contextpack.ErrCursorInvalid
	}
	op, err := b.openRenameOperation(ctx, p, claims.Binding.Scope)
	if err != nil {
		return err
	}
	header, err := b.renameOwner.Store.ValidateResultToken(ctx, b.Cursor, token, p.Digest, guard, op.guard)
	if err != nil {
		return coreStoreError(err)
	}
	if validate {
		stored, err := b.renameOwner.Store.Read(ctx, header.ResultID, p.Digest, header.Scope, op.guard)
		if err != nil {
			return coreStoreError(err)
		}
		checked, err := app.ValidateRenameWorkspace(ctx, stored, b.renameOwner.ProviderDigest, op.validation, int(b.renameOwner.Store.MaxResultBytes()))
		if err != nil {
			return coreStoreError(err)
		}
		data, ok := response.Data.(model.ValidateData)
		if !ok || !reflect.DeepEqual(data, checked.Data) || response.Freshness.Status != checked.Freshness.Status {
			return app.ErrFreshnessChanged
		}
	} else {
		data := response.Data.(model.RenameData)
		if data.ResultID != header.ResultID || data.ResultDigest != header.ResultDigest {
			return app.ErrCoreBackend
		}
		pageBinding := claims.Binding
		pageBinding.Purpose = "rename-page"
		var prior contextpack.SignedCursor
		if previous != nil {
			prior, err = b.Cursor.Verify(ctx, *previous, pageBinding, guard)
			if err != nil {
				return coreStoreError(err)
			}
		}
		if response.Pagination.NextCursor != nil {
			next, err := b.Cursor.Verify(ctx, *response.Pagination.NextCursor, pageBinding, guard)
			if err != nil {
				return coreStoreError(err)
			}
			if previous != nil && !contextpack.CursorAdvances(prior.Position, next.Position) {
				return contextpack.ErrCursorInvalid
			}
		}
	}
	_, err = b.renameOwner.Store.ValidateResultToken(ctx, b.Cursor, token, p.Digest, guard, op.guard)
	return coreStoreError(err)
}
