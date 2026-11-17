package app

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const maxPackEnvelope = 32 * 1024 * 1024

type packEnvelope struct {
	Version int             `json:"version"`
	Request json.RawMessage `json:"request"`
	Archive []byte          `json:"archive"`
}

// RetainedPackEnvelope contains sensitive selected data. Its owner must enforce
// owner-only storage, quota and expiration; neither field may be logged.
type RetainedPackEnvelope struct {
	Token           string
	PrincipalDigest string
	Bytes           []byte
}

// ArchiveEnvelope binds a validated original request to the already signed
// materialization. It does not publish or persist a token.
func (p *FreshnessPack) ArchiveEnvelope(ctx context.Context, principalID string, requestJSON []byte, ttlSeconds int64, maxBytes int) (RetainedPackEnvelope, error) {
	if p == nil || p.pack == nil || p.codec == nil || p.guard == nil || principalID == "" || len(principalID) > 4096 || len(requestJSON) > 65536 || maxBytes < 1024 || maxBytes > maxPackEnvelope {
		return RetainedPackEnvelope{}, ErrValidationInput
	}
	request, err := model.DecodePackRequestJSON(requestJSON, 65536)
	if err != nil {
		return RetainedPackEnvelope{}, ErrValidationInput
	}
	digest, err := model.RequestDigest("context_pack", principalID, "PackRequest", request, 65536, 4)
	if err != nil || digest != p.request {
		return RetainedPackEnvelope{}, ErrValidationInput
	}
	canonicalRequest, err := json.Marshal(request)
	if err != nil {
		return RetainedPackEnvelope{}, ErrValidationInput
	}
	if err := p.recheck(ctx, p.token, "", p.principal, p.guard); err != nil {
		return RetainedPackEnvelope{}, packContextError(ctx, err)
	}
	archive, err := p.pack.ArchiveForRetentionWithMetadata(ctx, maxBytes, ttlSeconds, canonicalRequest)
	if err != nil {
		return RetainedPackEnvelope{}, err
	}
	raw, err := json.Marshal(packEnvelope{Version: 1, Request: canonicalRequest, Archive: archive})
	if err != nil || len(raw) > maxBytes {
		return RetainedPackEnvelope{}, contextpack.ErrLimit
	}
	if err := p.recheck(ctx, p.token, "", p.principal, p.guard); err != nil {
		return RetainedPackEnvelope{}, packContextError(ctx, err)
	}
	return RetainedPackEnvelope{Token: p.token, PrincipalDigest: p.principal, Bytes: raw}, nil
}

// RestoreFreshnessEnvelope accepts only canonical bounded storage bytes. The
// archive MAC and the request digest are verified by RestoreFreshnessPack;
// caller-supplied callbacks remain responsible for all current authority.
func decodePackEnvelope(raw []byte, maxBytes int) (packEnvelope, error) {
	if len(raw) < 1 || maxBytes < 1024 || maxBytes > maxPackEnvelope || len(raw) > maxBytes {
		return packEnvelope{}, ErrValidationInput
	}
	raw = bytes.Clone(raw)
	var envelope packEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return packEnvelope{}, ErrValidationInput
	}
	canonical, err := json.Marshal(envelope)
	if err != nil || envelope.Version != 1 || !bytes.Equal(raw, canonical) {
		return packEnvelope{}, ErrValidationInput
	}
	return envelope, nil
}

func RestoreFreshnessEnvelope(ctx context.Context, raw []byte, in FreshnessPackRestoreInput, maxBytes int) (*FreshnessPack, error) {
	envelope, err := decodePackEnvelope(raw, maxBytes)
	if err != nil || in.Pack.Codec == nil {
		return nil, ErrValidationInput
	}
	retention, err := in.Pack.Codec.InspectPackArchive(envelope.Archive, in.Pack.MaxArchiveBytes)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(retention.Metadata, envelope.Request) {
		return nil, ErrValidationInput
	}
	in.Archive, in.RequestJSON = envelope.Archive, envelope.Request
	return RestoreFreshnessPack(ctx, in)
}

// ProveExpiredPackEnvelope authenticates the exact request and materialization
// even after token expiry. It only supplies expiry evidence for an already
// authorized GC; unknown keys, tampering and mismatched filenames fail closed.
func ProveExpiredPackEnvelope(ctx context.Context, id string, raw []byte, codec *contextpack.CursorCodec, maxBytes int) (bool, error) {
	if ctx == nil || codec == nil {
		return false, ErrValidationInput
	}
	if _, ok := ctx.Deadline(); !ok {
		return false, ErrValidationInput
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	envelope, err := decodePackEnvelope(raw, maxBytes)
	if err != nil {
		return false, err
	}
	retention, err := codec.InspectPackArchive(envelope.Archive, maxBytes)
	if err != nil || retention.ResultID != id || !bytes.Equal(retention.Metadata, envelope.Request) {
		return false, ErrValidationAuthority
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return retention.Expired, nil
}

// RecheckRetained enforces the signed token, retention TTL and authority after
// a storage operation and immediately before the caller releases a result.
func (p *FreshnessPack) RecheckRetained(ctx context.Context, token, principal string, guard contextpack.CursorGuard) error {
	if p == nil || token != p.token || principal != p.principal || guard == nil {
		return ErrValidationInput
	}
	return p.recheck(ctx, token, "", principal, sanitizedPackGuard(guard))
}
