package contextpack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrCursorInvalid = errors.New("cursor invalid")
	ErrCursorExpired = errors.New("cursor expired")
)

const cursorTTL = 15 * time.Minute
const cursorLimit = 32768
const cursorExactMax int64 = 9007199254740991

// CursorBinding consists of trusted, normalized identities. RequestDigest and
// BindingDigest must represent the complete immutable request and authority
// binding; neither source text nor an untrusted request belongs in a token.
type CursorBinding struct {
	Purpose         string            `json:"purpose"`
	PrincipalDigest string            `json:"principal_digest"`
	BindingDigest   string            `json:"binding_digest"`
	RequestDigest   string            `json:"request_digest"`
	OrderVersion    string            `json:"order_version"`
	Scope           model.ScopeVector `json:"scope"`
	ResultID        string            `json:"result_id"`
	ResultDigest    string            `json:"result_digest"`
}

// SignedPosition contains only bounded continuation keys and offsets, never
// source bodies or query text. Progress is meaningful only if the page producer
// also proves that it emitted or enumerated the corresponding data.
type SignedPosition struct {
	NextItem        int64    `json:"next_item"`
	NextCoverageGap int64    `json:"next_coverage_gap"`
	NextWarningGap  int64    `json:"next_warning_gap"`
	NextByteOffset  int64    `json:"next_byte_offset"`
	LastKey         []string `json:"last_key"`
}

type SignedCursor struct {
	Version   int            `json:"version"`
	KeyID     string         `json:"key_id"`
	Binding   CursorBinding  `json:"binding"`
	Position  SignedPosition `json:"position"`
	IssuedAt  int64          `json:"issued_at"`
	ExpiresAt int64          `json:"expires_at"`
	seal      [32]byte
}

// CursorGuard must check the authenticated principal, allowed binding,
// generation availability and current policy/record/group/incarnation epochs
// against trusted stores. The codec does not resolve those authorities itself.
type CursorGuard func(context.Context, CursorBinding) error

type CursorCodec struct {
	key   [32]byte
	keyID string
	now   func() time.Time
}

// GenerateCursorKey uses the operating system cryptographic random source.
// The caller must store the key owner-only and never include it in backups.
func GenerateCursorKey() ([32]byte, error) {
	var key [32]byte
	_, err := io.ReadFull(rand.Reader, key[:])
	return key, err
}

// NewCursorCodec accepts an already authorized, owner-only random key. Key
// rotation invalidates every token signed with the previous key.
func NewCursorCodec(key [32]byte) (*CursorCodec, error) {
	if key == ([32]byte{}) {
		return nil, ErrCursorInvalid
	}
	sum := sha256.Sum256(key[:])
	return &CursorCodec{key: key, keyID: hex.EncodeToString(sum[:]), now: time.Now}, nil
}

func cursorHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func cursorUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != "00000000-0000-0000-0000-000000000000"
}

func validPurpose(s string) bool {
	switch s {
	case "pack", "list", "mapping", "rename-result", "rename-page":
		return true
	}
	return false
}

func cursorName(s string, max int) bool {
	if s == "" || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

func normalizeCursorBinding(b CursorBinding) (CursorBinding, error) {
	if !validPurpose(b.Purpose) || !cursorHash(b.PrincipalDigest) || !cursorHash(b.BindingDigest) || !cursorHash(b.RequestDigest) || !cursorName(b.OrderVersion, 64) {
		return CursorBinding{}, ErrCursorInvalid
	}
	if b.ResultID != "" && !cursorUUID(b.ResultID) || b.ResultDigest != "" && (!cursorHash(b.ResultDigest) || b.ResultID == "") {
		return CursorBinding{}, ErrCursorInvalid
	}
	if (b.Purpose == "rename-result" || b.Purpose == "rename-page") && (b.ResultID == "" || b.ResultDigest == "") {
		return CursorBinding{}, ErrCursorInvalid
	}
	scope, err := model.NormalizeScopeVector(b.Scope, cursorLimit)
	if err != nil {
		return CursorBinding{}, ErrCursorInvalid
	}
	b.Scope = scope
	return b, nil
}

func validPosition(p SignedPosition) bool {
	for _, n := range []int64{p.NextItem, p.NextCoverageGap, p.NextWarningGap, p.NextByteOffset} {
		if n < 0 || n > cursorExactMax {
			return false
		}
	}
	if p.LastKey == nil || len(p.LastKey) > 3 {
		return false
	}
	for _, key := range p.LastKey {
		if !cursorName(key, 512) {
			return false
		}
	}
	return true
}

// CursorAdvances rejects repeated or regressing offsets/keysets. A new item may
// restart its absolute byte offset; safety-gap offsets may never regress.
func CursorAdvances(previous, next SignedPosition) bool {
	if !validPosition(previous) || !validPosition(next) || next.NextItem < previous.NextItem || next.NextCoverageGap < previous.NextCoverageGap || next.NextWarningGap < previous.NextWarningGap || next.NextItem == previous.NextItem && next.NextByteOffset < previous.NextByteOffset {
		return false
	}
	compare := 0
	for i := 0; i < len(previous.LastKey) && i < len(next.LastKey); i++ {
		if previous.LastKey[i] != next.LastKey[i] {
			compare = strings.Compare(next.LastKey[i], previous.LastKey[i])
			break
		}
	}
	if compare == 0 {
		compare = len(next.LastKey) - len(previous.LastKey)
	}
	return compare >= 0 && (compare > 0 || next.NextItem > previous.NextItem || next.NextCoverageGap > previous.NextCoverageGap || next.NextWarningGap > previous.NextWarningGap || next.NextByteOffset > previous.NextByteOffset)
}

func (c *CursorCodec) mac(purpose string, payload []byte) []byte {
	h := hmac.New(sha256.New, c.key[:])
	h.Write([]byte("MCE4D-CURSOR-V1\x00" + purpose + "\x00"))
	h.Write(payload)
	return h.Sum(nil)
}

func cloneCursorBinding(b CursorBinding) (CursorBinding, error) {
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > cursorLimit {
		return CursorBinding{}, ErrCursorInvalid
	}
	var clone CursorBinding
	if err = json.Unmarshal(raw, &clone); err != nil {
		return CursorBinding{}, ErrCursorInvalid
	}
	return clone, nil
}

func runCursorGuard(ctx context.Context, guard CursorGuard, binding CursorBinding) error {
	if ctx == nil || guard == nil {
		return ErrCursorInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrCursorInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	copyBinding, err := cloneCursorBinding(binding)
	if err != nil {
		return err
	}
	if err = guard(ctx, copyBinding); err != nil {
		return err
	}
	return ctx.Err()
}

// Issue checks authority before and after signing. A token is not returned if
// a guard fails, even when the first check passed.
func (c *CursorCodec) Issue(ctx context.Context, binding CursorBinding, position SignedPosition, guard CursorGuard) (string, error) {
	if c == nil || c.now == nil || !validPosition(position) {
		return "", ErrCursorInvalid
	}
	binding, err := normalizeCursorBinding(binding)
	if err != nil {
		return "", err
	}
	if err = runCursorGuard(ctx, guard, binding); err != nil {
		return "", err
	}
	token, claims, err := c.issueCandidate(ctx, binding, position)
	if err != nil {
		return "", err
	}
	if err = runCursorGuard(ctx, guard, binding); err != nil {
		return "", err
	}
	if err = c.checkTime(claims); err != nil {
		return "", err
	}
	return token, nil
}

// issueCandidate is private sizing work for an already authorized page. Its
// token must remain private until that page verifies the selected cursor and
// rechecks full authority before return. Public issuers must use Issue.
func (c *CursorCodec) issueCandidate(ctx context.Context, binding CursorBinding, position SignedPosition) (string, SignedCursor, error) {
	if ctx == nil || c == nil || c.now == nil || !validPosition(position) {
		return "", SignedCursor{}, ErrCursorInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", SignedCursor{}, err
	}
	binding, err := normalizeCursorBinding(binding)
	if err != nil {
		return "", SignedCursor{}, err
	}
	now := c.now().Unix()
	if now < 0 || now > cursorExactMax-int64(cursorTTL/time.Second) {
		return "", SignedCursor{}, ErrCursorInvalid
	}
	claims := SignedCursor{Version: 1, KeyID: c.keyID, Binding: binding, Position: position, IssuedAt: now, ExpiresAt: now + int64(cursorTTL/time.Second)}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", SignedCursor{}, ErrCursorInvalid
	}
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(c.mac(binding.Purpose, raw))
	if len(token) > cursorLimit {
		return "", SignedCursor{}, ErrLimit
	}
	if err = c.checkTime(claims); err != nil {
		return "", SignedCursor{}, err
	}
	return token, claims, ctx.Err()
}

func (c *CursorCodec) checkTime(claims SignedCursor) error {
	if claims.IssuedAt < 0 || claims.ExpiresAt <= claims.IssuedAt || claims.ExpiresAt-claims.IssuedAt > int64(cursorTTL/time.Second) {
		return ErrCursorInvalid
	}
	now := c.now().Unix()
	if now < claims.IssuedAt || now >= claims.ExpiresAt {
		return ErrCursorExpired
	}
	return nil
}

// Verify authenticates the exact purpose and complete trusted binding. The
// producer must call Recheck after reading data and immediately before output.
func (c *CursorCodec) Verify(ctx context.Context, token string, expected CursorBinding, guard CursorGuard) (SignedCursor, error) {
	claims, err := c.AuthenticateBound(ctx, token, expected)
	if err != nil {
		return SignedCursor{}, err
	}
	if err = c.Recheck(ctx, claims, guard); err != nil {
		return SignedCursor{}, err
	}
	return claims, nil
}

// AuthenticateBound proves signature, exact binding and lifetime only. It is
// for private immutable preparation, never an authorization grant. A producer
// MUST Recheck these claims with its actual current guard after preparation
// and before returning data, a token, or an error descriptor to its caller.
func (c *CursorCodec) AuthenticateBound(ctx context.Context, token string, expected CursorBinding) (SignedCursor, error) {
	if ctx == nil {
		return SignedCursor{}, ErrCursorInvalid
	}
	if _, ok := ctx.Deadline(); !ok {
		return SignedCursor{}, ErrCursorInvalid
	}
	if err := ctx.Err(); err != nil {
		return SignedCursor{}, err
	}
	expected, err := normalizeCursorBinding(expected)
	if err != nil {
		return SignedCursor{}, err
	}
	claims, err := c.authenticate(token, expected.Purpose)
	if err != nil {
		return SignedCursor{}, err
	}
	a, errA := json.Marshal(claims.Binding)
	b, errB := json.Marshal(expected)
	if errA != nil || errB != nil || !bytes.Equal(a, b) {
		return SignedCursor{}, ErrCursorInvalid
	}
	if err = c.checkTime(claims); err != nil {
		return SignedCursor{}, err
	}
	return claims, ctx.Err()
}

// VerifyPrincipal authenticates a token before exposing its pinned scope and
// result ID. The trusted per-request guard must independently authorize that
// scope, binding and request against current stores and permissions.
func (c *CursorCodec) VerifyPrincipal(ctx context.Context, token, purpose, principalDigest string, guard CursorGuard) (SignedCursor, error) {
	if !cursorHash(principalDigest) {
		return SignedCursor{}, ErrCursorInvalid
	}
	claims, err := c.authenticate(token, purpose)
	if err != nil {
		return SignedCursor{}, err
	}
	if claims.Binding.PrincipalDigest != principalDigest {
		return SignedCursor{}, ErrCursorInvalid
	}
	if err = c.Recheck(ctx, claims, guard); err != nil {
		return SignedCursor{}, err
	}
	return claims, nil
}

func (c *CursorCodec) authenticate(token, purpose string) (SignedCursor, error) {
	if c == nil || c.now == nil || !validPurpose(purpose) || len(token) == 0 || len(token) > cursorLimit {
		return SignedCursor{}, ErrCursorInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return SignedCursor{}, ErrCursorInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != parts[0] {
		return SignedCursor{}, ErrCursorInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(signature) != parts[1] || !hmac.Equal(signature, c.mac(purpose, raw)) {
		return SignedCursor{}, ErrCursorInvalid
	}
	if _, err = model.DecodeIdentityJSON(raw, cursorLimit); err != nil {
		return SignedCursor{}, ErrCursorInvalid
	}
	var claims SignedCursor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&claims); err != nil {
		return SignedCursor{}, ErrCursorInvalid
	}
	canonical, err := json.Marshal(claims)
	if err != nil || !bytes.Equal(raw, canonical) || claims.Version != 1 || claims.KeyID != c.keyID || !validPosition(claims.Position) {
		return SignedCursor{}, ErrCursorInvalid
	}
	got, err := normalizeCursorBinding(claims.Binding)
	if err != nil || got.Purpose != purpose {
		return SignedCursor{}, ErrCursorInvalid
	}
	claims.Binding = got
	verifiedBytes, err := json.Marshal(claims)
	if err != nil {
		return SignedCursor{}, ErrCursorInvalid
	}
	copy(claims.seal[:], c.mac("verified", verifiedBytes))
	return claims, nil
}

// Recheck is for unchanged claims returned by Verify, before output is
// released. A modified or caller-constructed claim has no valid local seal.
func (c *CursorCodec) Recheck(ctx context.Context, claims SignedCursor, guard CursorGuard) error {
	if c == nil || c.now == nil || claims.Version != 1 || claims.KeyID != c.keyID || !validPosition(claims.Position) {
		return ErrCursorInvalid
	}
	raw, err := json.Marshal(claims)
	if err != nil || !hmac.Equal(claims.seal[:], c.mac("verified", raw)) {
		return ErrCursorInvalid
	}
	if err := c.checkTime(claims); err != nil {
		return err
	}
	if err := runCursorGuard(ctx, guard, claims.Binding); err != nil {
		return err
	}
	return c.checkTime(claims)
}
