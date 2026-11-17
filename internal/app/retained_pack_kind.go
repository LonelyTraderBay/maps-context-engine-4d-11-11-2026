package app

import (
	"bytes"
	"context"
	"crypto/sha256"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// IdentifyRetainedPack authenticates bounded stored bytes only to select the
// matching restore path. It does not return archive contents or replace the
// restore owner's complete token, authority and source checks.
func IdentifyRetainedPack(ctx context.Context, store SymbolPackStorage, codec *contextpack.CursorCodec, token, principal, actor string, guard contextpack.CursorGuard, maxBytes int) (string, error) {
	if ctx == nil || store == nil || codec == nil || guard == nil || actor == "" || len(actor) > 4096 || maxBytes < 1024 || maxBytes > maxPackEnvelope || int64(maxBytes) > store.MaxResultBytes() {
		return "", ErrValidationInput
	}
	claims, err := codec.VerifyPrincipal(ctx, token, "pack", principal, guard)
	if err != nil {
		return "", err
	}
	scope := claims.Binding.Scope
	if scope.Kind != "single" || len(scope.Members) != 1 || len(scope.UnavailableMembers) != 0 || scope.Members[0].WorktreeID != store.WorktreeID() || claims.Binding.ResultID == "" {
		return "", ErrValidationAuthority
	}
	var kind string
	var digest [sha256.Size]byte
	seen := false
	raw, err := store.Read(ctx, claims.Binding.ResultID, func(ctx context.Context, raw []byte) error {
		current := sha256.Sum256(raw)
		if seen && current != digest {
			return ErrValidationAuthority
		}
		candidate := ""
		envelope, envelopeErr := decodePackEnvelope(raw, maxBytes)
		if envelopeErr == nil {
			info, err := codec.InspectPackArchive(envelope.Archive, maxBytes)
			if err != nil || info.ResultID != claims.Binding.ResultID || !bytes.Equal(info.Metadata, envelope.Request) {
				return ErrValidationAuthority
			}
			if info.Expired {
				return contextpack.ErrPackExpired
			}
			request, err := model.DecodePackRequestJSON(envelope.Request, 65536)
			if err != nil {
				return ErrValidationInput
			}
			requestDigest, err := model.RequestDigest("context_pack", actor, "PackRequest", request, 65536, 4)
			if err != nil || requestDigest != claims.Binding.RequestDigest {
				return ErrValidationAuthority
			}
			candidate = "symbol"
		} else {
			opened, err := codec.OpenPlanMaterialization(ctx, raw, token, principal, maxBytes, guard)
			if err != nil {
				return err
			}
			if err := opened.Recheck(ctx, guard); err != nil {
				return err
			}
			candidate = "plan"
		}
		if seen && candidate != kind {
			return ErrValidationAuthority
		}
		seen, digest, kind = true, current, candidate
		return codec.Recheck(ctx, claims, guard)
	})
	if err != nil {
		return "", err
	}
	if !seen || sha256.Sum256(raw) != digest {
		return "", ErrValidationAuthority
	}
	if err := codec.Recheck(ctx, claims, guard); err != nil {
		return "", err
	}
	return kind, ctx.Err()
}
