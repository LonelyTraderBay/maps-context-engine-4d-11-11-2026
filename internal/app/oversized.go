package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// oversizedEvidenceError can only be minted inside this package after an
// immutable retained result confirms the item and its source descriptors.
// Its checks are repeated by the service immediately before delivery.
type oversizedEvidenceError struct {
	scope                       model.ScopeVector
	itemID                      string
	sources                     []model.SourceRef
	principal, binding, request string
	token, cursor               string
	first                       bool
	maxOutput                   int64
	recheck                     func(context.Context, *oversizedEvidenceError) error
	release                     func()
}

func (*oversizedEvidenceError) Error() string { return contextpack.ErrItemTooLarge.Error() }
func (*oversizedEvidenceError) Unwrap() error { return contextpack.ErrItemTooLarge }

func oversizedID(value string) bool {
	value = strings.TrimPrefix(value, "structure-")
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}

func newOversizedEvidence(ctx context.Context, item *contextpack.OversizedItemError, scope model.ScopeVector, principal, binding, request, token, cursor string, first bool, maxOutput, policyMax int64, check func(context.Context) error) error {
	if ctx == nil || item == nil || check == nil || !oversizedID(item.ItemID) || len(item.Sources) > 16 || len(principal) != 64 || len(binding) != 64 || len(request) != 64 || token == "" || first != (cursor == "") {
		return ErrValidationInput
	}
	if maxOutput == 0 {
		maxOutput = 24576
	}
	if policyMax < 4096 || policyMax > 131072 || maxOutput < 4096 || maxOutput > 131072 {
		return ErrValidationInput
	}
	if maxOutput > policyMax {
		maxOutput = policyMax
	}
	raw, err := json.Marshal(scope)
	if err != nil || len(raw) > 32768 {
		return ErrValidationInput
	}
	copyScope, err := model.DecodeScopeVectorJSON(raw, 32768)
	if err != nil || model.VerifyScopeVectorDigest(copyScope, 32768) != nil {
		return ErrValidationInput
	}
	sources := append([]model.SourceRef{}, item.Sources...)
	for _, source := range sources {
		raw, err := json.Marshal(source)
		if err != nil || len(raw) > 32768 {
			return ErrValidationInput
		}
		if _, err := model.DecodeSourceRefJSON(raw, 32768); err != nil {
			return ErrValidationInput
		}
		if _, err := model.MatchPinnedSource(copyScope, source, nil, 32768); err != nil {
			return ErrValidationAuthority
		}
	}
	if err := check(ctx); err != nil {
		return packContextError(ctx, err)
	}
	expectedScope, err := json.Marshal(copyScope)
	if err != nil {
		return ErrValidationInput
	}
	expectedSources := append([]model.SourceRef{}, sources...)
	expectedID := item.ItemID
	proof := &oversizedEvidenceError{scope: copyScope, itemID: expectedID, sources: sources, principal: principal, binding: binding, request: request, token: token, cursor: cursor, first: first, maxOutput: maxOutput}
	proof.recheck = func(ctx context.Context, current *oversizedEvidenceError) error {
		if current == nil || current.itemID != expectedID || !reflect.DeepEqual(current.sources, expectedSources) || current.principal != principal || current.binding != binding || current.request != request || current.token != token || current.cursor != cursor || current.first != first || current.maxOutput != maxOutput {
			return ErrValidationAuthority
		}
		raw, err := json.Marshal(current.scope)
		if err != nil || !bytes.Equal(raw, expectedScope) {
			return ErrValidationAuthority
		}
		return check(ctx)
	}
	return proof
}

// GuardOversizedError adds a cache/owner check to an existing sealed proof; it
// cannot turn an arbitrary backend error into a source-bearing descriptor.
// A release callback may discard a first-page materialization after delivery.
func GuardOversizedError(err error, guard func(context.Context) error, release func()) (error, bool) {
	var proof *oversizedEvidenceError
	if guard == nil || !errors.As(err, &proof) || proof == nil {
		return err, false
	}
	copyProof := *proof
	previous := proof.recheck
	copyProof.recheck = func(ctx context.Context, current *oversizedEvidenceError) error {
		if err := previous(ctx, current); err != nil {
			return err
		}
		return guard(ctx)
	}
	if release != nil {
		var once sync.Once
		previousRelease := proof.release
		copyProof.release = func() {
			once.Do(func() {
				if previousRelease != nil {
					previousRelease()
				}
				release()
			})
		}
	}
	return &copyProof, true
}

func (p *FreshnessPack) describeOversized(ctx context.Context, cause error, cursor, principal string, guard contextpack.CursorGuard, options contextpack.PackPageOptions, first bool) error {
	var item *contextpack.OversizedItemError
	if !errors.As(cause, &item) || item == nil {
		return packContextError(ctx, cause)
	}
	copyItem := &contextpack.OversizedItemError{ItemID: item.ItemID, Sources: append([]model.SourceRef{}, item.Sources...)}
	check := func(ctx context.Context) error {
		if err := p.recheck(ctx, p.token, cursor, principal, guard); err != nil {
			return err
		}
		if err := p.pack.VerifyOversizedItem(ctx, cursor, principal, guard, copyItem); err != nil {
			return err
		}
		return p.recheck(ctx, p.token, cursor, principal, guard)
	}
	return newOversizedEvidence(ctx, copyItem, p.validation.Scope, p.principal, p.binding, p.request, p.token, cursor, first, options.MaxOutputBytes, p.slice.PolicyMaxOutput(), check)
}

func (p *FreshnessPlan) describeOversized(ctx context.Context, cause error, cursor, principal string, guard contextpack.CursorGuard, options contextpack.PackPageOptions, first bool) error {
	var item *contextpack.OversizedItemError
	if !errors.As(cause, &item) || item == nil {
		return packContextError(ctx, cause)
	}
	copyItem := &contextpack.OversizedItemError{ItemID: item.ItemID, Sources: append([]model.SourceRef{}, item.Sources...)}
	check := func(ctx context.Context) error {
		if err := p.recheck(ctx, cursor, principal, guard); err != nil {
			return err
		}
		if err := p.pager.VerifyOversizedItem(ctx, cursor, principal, guard, copyItem); err != nil {
			return err
		}
		return p.recheck(ctx, cursor, principal, guard)
	}
	return newOversizedEvidence(ctx, copyItem, p.packBinding.Scope, p.principal, p.packBinding.BindingDigest, p.packBinding.RequestDigest, p.token, cursor, first, options.MaxOutputBytes, p.plan.PolicyMaxOutput(), check)
}
