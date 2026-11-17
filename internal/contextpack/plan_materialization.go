package contextpack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"reflect"
)

const planMaterializationMagic = "MCE4D-PLAN-1\x00"
const planMaterializationMax = 32 * 1024 * 1024

// A plan archive is sensitive temporary data. Its owner must separately prove
// secure storage, quota, current authority and the exact captured snapshot.
type planMaterialization struct {
	Version   int    `json:"version"`
	PackToken string `json:"pack_token"`
	ListToken string `json:"list_token"`
	ExpiresAt int64  `json:"expires_at"`
	Payload   []byte `json:"payload"`
}

type PlanMaterialization struct {
	Payload   []byte
	ListToken string
	ExpiresAt int64
	codec     *CursorCodec
	pack      SignedCursor
	list      SignedCursor
	expires   int64
	anchor    string
}

// Recheck uses the verified token claims and original signed expiry. Editing
// exported fields or constructing a value manually cannot extend its life.
func (m PlanMaterialization) Recheck(ctx context.Context, guard CursorGuard) error {
	if m.codec == nil || ctx == nil || guard == nil || m.ExpiresAt != m.expires || m.ListToken != m.anchor || !matchingPlanTokens(m.pack, m.list) {
		return ErrCursorInvalid
	}
	if m.codec.now().Unix() >= m.expires {
		return ErrPackExpired
	}
	if err := m.codec.Recheck(ctx, m.pack, guard); err != nil {
		return err
	}
	if err := m.codec.Recheck(ctx, m.list, guard); err != nil {
		return err
	}
	if m.codec.now().Unix() >= m.expires {
		return ErrPackExpired
	}
	return ctx.Err()
}

// PlanMaterializationExpiry discloses no archived content. It may be used
// only to prove expiry of an already authorized and owned GC candidate.
type PlanMaterializationExpiry struct {
	ResultID  string
	ExpiresAt int64
	Expired   bool
}

func planStartPosition(p SignedPosition) bool {
	return p.NextItem == 0 && p.NextCoverageGap == 0 && p.NextWarningGap == 0 && p.NextByteOffset == 0 && len(p.LastKey) == 0
}

func matchingPlanTokens(pack, list SignedCursor) bool {
	if pack.Binding.Purpose != "pack" || list.Binding.Purpose != "list" || pack.Binding.ResultID == "" || pack.Binding.ResultDigest == "" || !planStartPosition(pack.Position) || !planStartPosition(list.Position) {
		return false
	}
	listBinding := list.Binding
	listBinding.Purpose = "pack"
	return reflect.DeepEqual(pack.Binding, listBinding)
}

func validPlanTokenTime(c SignedCursor) bool {
	return c.IssuedAt >= 0 && c.IssuedAt <= cursorExactMax-900 && c.ExpiresAt > c.IssuedAt && c.ExpiresAt <= c.IssuedAt+900
}

func (c *CursorCodec) decodePlanMaterialization(raw []byte, maxBytes int) (planMaterialization, SignedCursor, SignedCursor, error) {
	var empty planMaterialization
	var noCursor SignedCursor
	if c == nil || c.now == nil || maxBytes < 1024 || maxBytes > planMaterializationMax || len(raw) <= len(planMaterializationMagic)+32 || len(raw) > maxBytes {
		return empty, noCursor, noCursor, ErrCursorInvalid
	}
	raw = bytes.Clone(raw)
	if string(raw[:len(planMaterializationMagic)]) != planMaterializationMagic {
		return empty, noCursor, noCursor, ErrCursorInvalid
	}
	signature := raw[len(planMaterializationMagic) : len(planMaterializationMagic)+32]
	payload := raw[len(planMaterializationMagic)+32:]
	if !hmac.Equal(signature, c.mac("plan-materialization-v1", payload)) {
		return empty, noCursor, noCursor, ErrCursorInvalid
	}
	var archive planMaterialization
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&archive) != nil {
		return empty, noCursor, noCursor, ErrCursorInvalid
	}
	canonical, err := json.Marshal(archive)
	if err != nil || !bytes.Equal(canonical, payload) || archive.Version != 1 || len(archive.Payload) == 0 {
		return empty, noCursor, noCursor, ErrCursorInvalid
	}
	pack, err := c.authenticate(archive.PackToken, "pack")
	if err != nil {
		return empty, noCursor, noCursor, err
	}
	list, err := c.authenticate(archive.ListToken, "list")
	if err != nil || !matchingPlanTokens(pack, list) || !validPlanTokenTime(pack) || !validPlanTokenTime(list) || archive.ExpiresAt <= pack.IssuedAt || archive.ExpiresAt <= list.IssuedAt || archive.ExpiresAt > pack.ExpiresAt || archive.ExpiresAt > list.ExpiresAt {
		return empty, noCursor, noCursor, ErrCursorInvalid
	}
	return archive, pack, list, nil
}

// SealPlanMaterialization binds opaque plan bytes to both the original pack
// token and list anchor. previousExpiry prevents a restored plan from being
// re-archived with a longer lifetime. No token is made usable by this method.
func (c *CursorCodec) SealPlanMaterialization(ctx context.Context, packToken, listToken, principal string, payload []byte, ttlSeconds, previousExpiry int64, maxBytes int, guard CursorGuard) ([]byte, error) {
	if c == nil || ctx == nil || guard == nil || ttlSeconds < 1 || ttlSeconds > 900 || previousExpiry < 0 || maxBytes < 1024 || maxBytes > planMaterializationMax || len(payload) < 1 {
		return nil, ErrCursorInvalid
	}
	// Account for JSON's base64 expansion before allocating the envelope.
	remaining := maxBytes - len(planMaterializationMagic) - 32 - len(packToken) - len(listToken) - 1024
	if remaining < 0 || len(payload) > remaining/4*3 {
		return nil, ErrLimit
	}
	pack, err := c.VerifyPrincipal(ctx, packToken, "pack", principal, guard)
	if err != nil {
		return nil, err
	}
	list, err := c.VerifyPrincipal(ctx, listToken, "list", principal, guard)
	if err != nil {
		return nil, err
	}
	if !matchingPlanTokens(pack, list) || !validPlanTokenTime(pack) || !validPlanTokenTime(list) {
		return nil, ErrCursorInvalid
	}
	expires := min(pack.ExpiresAt, list.ExpiresAt, pack.IssuedAt+ttlSeconds, list.IssuedAt+ttlSeconds)
	if previousExpiry > 0 && previousExpiry < expires {
		expires = previousExpiry
	}
	if expires <= pack.IssuedAt || expires <= list.IssuedAt || c.now().Unix() >= expires {
		return nil, ErrPackExpired
	}
	archive := planMaterialization{Version: 1, PackToken: packToken, ListToken: listToken, ExpiresAt: expires, Payload: bytes.Clone(payload)}
	encoded, err := json.Marshal(archive)
	if err != nil || len(encoded) > maxBytes-len(planMaterializationMagic)-32 {
		return nil, ErrLimit
	}
	raw := make([]byte, 0, len(planMaterializationMagic)+32+len(encoded))
	raw = append(raw, planMaterializationMagic...)
	raw = append(raw, c.mac("plan-materialization-v1", encoded)...)
	raw = append(raw, encoded...)
	if err := c.Recheck(ctx, pack, guard); err != nil {
		return nil, err
	}
	if err := c.Recheck(ctx, list, guard); err != nil {
		return nil, err
	}
	if c.now().Unix() >= expires {
		return nil, ErrPackExpired
	}
	return raw, nil
}

// OpenPlanMaterialization authenticates both tokens and every stored byte,
// then checks current authority and lifetime before returning a copy.
func (c *CursorCodec) OpenPlanMaterialization(ctx context.Context, raw []byte, token, principal string, maxBytes int, guard CursorGuard) (PlanMaterialization, error) {
	var empty PlanMaterialization
	if ctx == nil || guard == nil {
		return empty, ErrCursorInvalid
	}
	archive, _, _, err := c.decodePlanMaterialization(raw, maxBytes)
	if err != nil || archive.PackToken != token {
		return empty, ErrCursorInvalid
	}
	pack, err := c.VerifyPrincipal(ctx, archive.PackToken, "pack", principal, guard)
	if err != nil {
		return empty, err
	}
	list, err := c.VerifyPrincipal(ctx, archive.ListToken, "list", principal, guard)
	if err != nil {
		return empty, err
	}
	if !matchingPlanTokens(pack, list) {
		return empty, ErrCursorInvalid
	}
	if c.now().Unix() >= archive.ExpiresAt {
		return empty, ErrPackExpired
	}
	result := PlanMaterialization{Payload: bytes.Clone(archive.Payload), ListToken: archive.ListToken, ExpiresAt: archive.ExpiresAt,
		codec: c, pack: pack, list: list, expires: archive.ExpiresAt, anchor: archive.ListToken}
	if err := c.Recheck(ctx, pack, guard); err != nil {
		return empty, err
	}
	if err := c.Recheck(ctx, list, guard); err != nil {
		return empty, err
	}
	if c.now().Unix() >= archive.ExpiresAt {
		return empty, ErrPackExpired
	}
	return result, nil
}

// InspectPlanMaterialization authenticates expiry even after token expiry;
// unknown keys, malformed tokens or modified bytes cannot authorize GC.
func (c *CursorCodec) InspectPlanMaterialization(raw []byte, maxBytes int) (PlanMaterializationExpiry, error) {
	archive, pack, _, err := c.decodePlanMaterialization(raw, maxBytes)
	if err != nil {
		return PlanMaterializationExpiry{}, err
	}
	return PlanMaterializationExpiry{ResultID: pack.Binding.ResultID, ExpiresAt: archive.ExpiresAt, Expired: c.now().Unix() >= archive.ExpiresAt}, nil
}
