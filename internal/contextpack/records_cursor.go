package contextpack

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
)

// Record-list cursors bind the authorization, filters, stores and epoch to
// one fingerprint. Durable records can be read before an index exists, so
// this continuation has no synthetic ScopeVector member.
const recordCursorPayload = 1 + 8 + 32 + 36

func (c *CursorCodec) IssueRecordList(bindingDigest, afterID string) (string, error) {
	if c == nil || !cursorHash(bindingDigest) || !cursorUUID(afterID) {
		return "", ErrCursorInvalid
	}
	digest, err := hex.DecodeString(bindingDigest)
	if err != nil {
		return "", ErrCursorInvalid
	}
	payload := make([]byte, recordCursorPayload)
	payload[0] = 1
	binary.BigEndian.PutUint64(payload[1:9], uint64(c.now().Add(cursorTTL).Unix()))
	copy(payload[9:41], digest)
	copy(payload[41:], afterID)
	return base64.RawURLEncoding.EncodeToString(append(payload, c.mac("records-list", payload)...)), nil
}

func (c *CursorCodec) VerifyRecordList(token, bindingDigest string) (string, error) {
	if c == nil || !cursorHash(bindingDigest) ||
		len(token) != base64.RawURLEncoding.EncodedLen(recordCursorPayload+32) {
		return "", ErrCursorInvalid
	}
	wire, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(wire) != recordCursorPayload+32 {
		return "", ErrCursorInvalid
	}
	payload, signature := wire[:recordCursorPayload], wire[recordCursorPayload:]
	if payload[0] != 1 || !hmac.Equal(signature, c.mac("records-list", payload)) {
		return "", ErrCursorInvalid
	}
	expires := int64(binary.BigEndian.Uint64(payload[1:9]))
	now := c.now()
	if expires <= now.Unix() || expires > now.Add(cursorTTL).Unix() ||
		bindingDigest != hex.EncodeToString(payload[9:41]) {
		return "", ErrCursorExpired
	}
	id := string(payload[41:])
	if !cursorUUID(id) {
		return "", ErrCursorInvalid
	}
	return id, nil
}
