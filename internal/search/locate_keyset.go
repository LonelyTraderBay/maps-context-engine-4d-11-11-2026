package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrLocatePageInvalid = errors.New("locate page is invalid")
	ErrLocateUnsupported = errors.New("locate filter or mode is unsupported")
)

type LocatePage struct {
	Result     Result
	NextCursor *string
}

// LocateKeysetPage pages one immutable, single-member published generation.
// The trusted guard must authorize the principal and recheck the complete
// catalog, policy, record and store identities. Before invoking this function
// for a continuation the caller can use VerifyPrincipal to recover its pinned
// binding; it must never substitute the currently active generation. This
// primitive does not serialize a public envelope or grant source permissions.
// Module, topic and text/file filters fail explicitly until implemented.
func LocateKeysetPage(ctx context.Context, reader Reader, codec *contextpack.CursorCodec, binding contextpack.CursorBinding, guard contextpack.CursorGuard, principalID string, request model.LocateRequest, base Request) (LocatePage, error) {
	if ctx == nil || reader == nil || codec == nil || guard == nil || principalID == "" || binding.Purpose != "list" || binding.ResultID != "" || binding.ResultDigest != "" || binding.OrderVersion != "locate-id-v1" {
		return LocatePage{}, ErrLocatePageInvalid
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return LocatePage{}, ErrLocatePageInvalid
	}
	request, err = model.DecodeLocateRequestJSON(raw, 32768)
	if err != nil {
		return LocatePage{}, ErrLocatePageInvalid
	}
	if request.Mode != "symbol" && request.Mode != "text" && request.Mode != "file" || (request.Mode == "text" || request.Mode == "file") && (request.Position != nil || len(request.Kinds) != 0 || request.Package != nil) {
		return LocatePage{}, ErrLocateUnsupported
	}
	if model.VerifyScopeVectorDigest(binding.Scope, 32768) != nil || binding.Scope.Kind != "single" || len(binding.Scope.Members) != 1 || len(binding.Scope.UnavailableMembers) != 0 {
		return LocatePage{}, ErrLocatePageInvalid
	}
	member := binding.Scope.Members[0]
	if request.Scope != nil {
		scope := request.Scope
		if scope.GroupID != nil || scope.WorktreeID != nil && *scope.WorktreeID != member.WorktreeID || scope.ProfileID != nil && *scope.ProfileID != member.ProfileID || scope.GenerationID != nil && *scope.GenerationID != member.GenerationID || scope.PinnedVector != nil && model.ComparePinnedScope(*scope.PinnedVector, binding.Scope, 32768) != nil {
			return LocatePage{}, ErrLocatePageInvalid
		}
	}
	continuation := request.Cursor
	request.Cursor, request.Limit = nil, 20 // pagination controls do not change query identity
	digest, err := model.RequestDigest("locate", principalID, "LocateRequest", request, 32768, 4)
	if err != nil {
		return LocatePage{}, ErrLocatePageInvalid
	}
	if binding.RequestDigest != "" && binding.RequestDigest != digest {
		return LocatePage{}, contextpack.ErrCursorInvalid
	}
	binding.RequestDigest = digest
	var claims contextpack.SignedCursor
	if continuation == nil {
		anchor, e := codec.Issue(ctx, binding, contextpack.SignedPosition{LastKey: []string{}}, guard)
		if e != nil {
			return LocatePage{}, e
		}
		claims, err = codec.Verify(ctx, anchor, binding, guard)
	} else {
		claims, err = codec.Verify(ctx, *continuation, binding, guard)
	}
	if err != nil {
		return LocatePage{}, err
	}
	position := claims.Position
	if position.NextCoverageGap != 0 || position.NextWarningGap != 0 || position.NextByteOffset != 0 || continuation == nil && (position.NextItem != 0 || len(position.LastKey) != 0) || continuation != nil && (position.NextItem == 0 || len(position.LastKey) == 0) {
		return LocatePage{}, contextpack.ErrCursorInvalid
	}
	after, err := decodeLocateKey(position.LastKey)
	if err != nil {
		return LocatePage{}, err
	}
	// Restore the requested page limit after deriving the normalized digest.
	decoded, err := model.DecodeLocateRequestJSON(raw, 32768)
	if err != nil {
		return LocatePage{}, ErrLocatePageInvalid
	}
	base.ProfileID, base.GenerationID, base.Keyset, base.AfterID, base.Limit = member.ProfileID, member.GenerationID, true, after, int(decoded.Limit)
	base.Query, base.Position, base.Qualified = "", nil, false
	base.Kinds = append([]string(nil), decoded.Kinds...)
	base.Package = ""
	if decoded.Package != nil {
		base.Package = *decoded.Package
	}
	if decoded.Position != nil {
		base.Mode, base.Position = "position", decoded.Position
	} else if decoded.Mode == "text" {
		base.Mode, base.Query = "text", *decoded.Query
	} else if decoded.Mode == "file" {
		base.Mode, base.Query = "file", *decoded.Query
	} else {
		base.Mode, base.Query = "prefix", *decoded.Query
	}
	result, err := reader.Search(ctx, base)
	if err != nil {
		return LocatePage{}, err
	}
	if result.WorktreeID != member.WorktreeID || result.ProfileID != member.ProfileID || result.GenerationID != member.GenerationID || result.SnapshotDigest != member.SnapshotDigest || result.IndexIncarnation != member.IndexIncarnation {
		return LocatePage{}, ErrLocatePageInvalid
	}
	last, count := after, 0
	if base.Mode == "text" || base.Mode == "file" {
		if len(result.Symbols) != 0 {
			return LocatePage{}, ErrLocatePageInvalid
		}
		for _, hit := range result.Text {
			id := ""
			if base.Mode == "file" {
				var e error
				id, e = FileLocateKey(hit.Path)
				if e != nil || hit.EvidenceKind != "path_match" || !strings.Contains(hit.Path, base.Query) {
					return LocatePage{}, ErrLocatePageInvalid
				}
			} else {
				hash := sha256.Sum256([]byte("mce4d:search-file:v1\x00" + hit.Path))
				id = "file-" + hex.EncodeToString(hash[:])
				if hit.EvidenceKind != "text_match" {
					return LocatePage{}, ErrLocatePageInvalid
				}
			}
			if id <= last {
				return LocatePage{}, ErrLocatePageInvalid
			}
			last, count = id, count+1
		}
	} else {
		if len(result.Text) != 0 {
			return LocatePage{}, ErrLocatePageInvalid
		}
		for _, hit := range result.Symbols {
			if hit.Symbol.SymbolID <= last {
				return LocatePage{}, ErrLocatePageInvalid
			}
			if hit.Source != nil {
				if _, e := model.MatchPinnedSource(binding.Scope, *hit.Source, nil, 32768); e != nil {
					return LocatePage{}, ErrLocatePageInvalid
				}
			}
			last, count = hit.Symbol.SymbolID, count+1
		}
	}
	if count > int(decoded.Limit) || continuation != nil && count == 0 || result.Truncated && (count == 0 || result.NextKey != last) || !result.Truncated && result.NextKey != "" {
		return LocatePage{}, ErrLocatePageInvalid
	}
	page := LocatePage{Result: result}
	page.Result.NextKey = ""
	if result.Truncated {
		parts, e := encodeLocateKey(last)
		if e != nil || position.NextItem > 9007199254740991-int64(count) {
			return LocatePage{}, ErrLocatePageInvalid
		}
		next := contextpack.SignedPosition{NextItem: position.NextItem + int64(count), LastKey: parts}
		if !contextpack.CursorAdvances(position, next) {
			return LocatePage{}, ErrLocatePageInvalid
		}
		token, e := codec.Issue(ctx, binding, next, guard)
		if e != nil {
			return LocatePage{}, e
		}
		page.NextCursor = &token
	}
	if err := codec.Recheck(ctx, claims, guard); err != nil {
		return LocatePage{}, err
	}
	return page, nil
}

// FileLocateKey orders immutable file paths without putting long paths into a
// signed cursor. Colliding keys for distinct paths must be rejected by readers.
func FileLocateKey(filename string) (string, error) {
	if model.ValidateRelativePath(filename, false) != nil {
		return "", ErrLocatePageInvalid
	}
	value, err := model.CanonicalString(filename)
	if err != nil {
		return "", ErrLocatePageInvalid
	}
	_, digest, err := model.EncodeCanonical("locate-file-order-v1", value, 32768)
	if err != nil {
		return "", ErrLocatePageInvalid
	}
	return "file-" + digest, nil
}

// Hex preserves SQLite BINARY byte order and avoids control characters. Three
// 512-byte pieces cover the complete 192-rune ID allowed by the wire model.
func encodeLocateKey(id string) ([]string, error) {
	if id == "" || len(id) > 768 || !utf8.ValidString(id) {
		return nil, ErrLocatePageInvalid
	}
	value := hex.EncodeToString([]byte(id))
	parts := make([]string, 0, 3)
	for len(value) > 512 {
		parts = append(parts, value[:512])
		value = value[512:]
	}
	parts = append(parts, value)
	return parts, nil
}

func decodeLocateKey(parts []string) (string, error) {
	if len(parts) == 0 {
		return "", nil
	}
	if len(parts) > 3 {
		return "", contextpack.ErrCursorInvalid
	}
	for i, part := range parts {
		if len(part) == 0 || len(part) > 512 || i < len(parts)-1 && len(part) != 512 {
			return "", contextpack.ErrCursorInvalid
		}
	}
	encoded := strings.Join(parts, "")
	decoded, err := hex.DecodeString(encoded)
	if err != nil || hex.EncodeToString(decoded) != encoded || len(decoded) == 0 || len(decoded) > 768 || !utf8.Valid(decoded) {
		return "", contextpack.ErrCursorInvalid
	}
	return string(decoded), nil
}
