//go:build linux

package compose

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

type groupListCursor struct {
	Installation       string `json:"installation"`
	CatalogIncarnation string `json:"catalog_incarnation"`
	RevisionDigest     string `json:"revision_digest"`
	After              string `json:"after"`
	ExpiresAt          int64  `json:"expires_at"`
}

func groupListSignature(key *[32]byte, raw []byte) []byte {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte("MCE4D-GROUP-LIST-CURSOR1\x00"))
	mac.Write(raw)
	return mac.Sum(nil)
}

func decodeGroupListCursor(token string, key *[32]byte, installation string, now time.Time) (groupListCursor, error) {
	var cursor groupListCursor
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || len(token) > 1024 {
		return cursor, contextpack.ErrCursorInvalid
	}
	encoding := base64.RawURLEncoding.Strict()
	raw, rawErr := encoding.DecodeString(payload)
	signed, signedErr := encoding.DecodeString(signature)
	if rawErr != nil || signedErr != nil || !hmac.Equal(groupListSignature(key, raw), signed) ||
		json.Unmarshal(raw, &cursor) != nil || cursor.Installation != installation ||
		!model.ValidUUID(cursor.CatalogIncarnation) || !model.ValidUUID(cursor.After) || !model.ValidDigest(cursor.RevisionDigest) {
		return groupListCursor{}, contextpack.ErrCursorInvalid
	}
	if now.Unix() >= cursor.ExpiresAt {
		return groupListCursor{}, contextpack.ErrCursorExpired
	}
	return cursor, nil
}

func RunNativeGroupAdmin(ctx context.Context, installation, config string, request model.GroupAdminRequest, cpus int) (_ model.GroupAdminData, next *string, resultErr error) {
	out := model.GroupAdminData{Operation: "group_admin", Groups: []model.GroupSummary{}}
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return out, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	service, err := protectedConfig(ctx, state, config, cpus)
	if err != nil {
		return out, nil, err
	}
	effective, _, err := service.StartupConfig()
	if err != nil {
		return out, nil, err
	}
	cat, err := state.OpenCatalog(ctx)
	if err != nil {
		return out, nil, err
	}
	switch request.Action {
	case "get":
		if request.GroupID == nil {
			return out, nil, catalog.ErrGroupInput
		}
		group, err := cat.GroupMetadata(ctx, *request.GroupID)
		if err != nil {
			return out, nil, err
		}
		out.Groups = append(out.Groups, group)
	case "list":
		if request.Limit == nil {
			return out, nil, catalog.ErrGroupInput
		}
		out.Groups, next, err = nativeGroupList(ctx, state, cat, request, min(effective.Core.ResultTtlSeconds, 900))
		if err != nil {
			return model.GroupAdminData{}, nil, err
		}
	case "put", "remove":
		group, err := changeNativeGroup(ctx, state, cat, request, effective.Core.WriterLockWaitMs)
		if err != nil {
			return out, nil, err
		}
		out.Groups = append(out.Groups, group)
	default:
		return out, nil, catalog.ErrGroupInput
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return model.GroupAdminData{}, nil, err
	}
	scanner, err := policy.NewScanner(len(raw))
	if err != nil || scanner.CheckContent(raw) != nil {
		return model.GroupAdminData{}, nil, app.ErrAdminDenied
	}
	if err := state.Check(ctx); err != nil {
		return model.GroupAdminData{}, nil, err
	}
	return out, next, ctx.Err()
}

func nativeGroupList(ctx context.Context, state *sqliteowner.ProtectedState, cat *catalog.Store,
	request model.GroupAdminRequest, ttl int64) ([]model.GroupSummary, *string, error) {
	key, err := state.CursorKey(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer clear(key[:])
	installation := state.InstallationID()
	now := time.Now()
	var cursor groupListCursor
	if request.Cursor != nil {
		cursor, err = decodeGroupListCursor(*request.Cursor, &key, installation, now)
		if err != nil {
			return nil, nil, err
		}
	}
	page, err := cat.ListGroups(ctx, cursor.After, int(*request.Limit))
	if err != nil {
		return nil, nil, err
	}
	if request.Cursor != nil && (page.RevisionDigest != cursor.RevisionDigest || page.CatalogIncarnation != cursor.CatalogIncarnation) {
		return nil, nil, contextpack.ErrCursorExpired
	}
	if !page.More {
		return page.Groups, nil, nil
	}
	expires := cursor.ExpiresAt
	if request.Cursor == nil {
		expires = now.Unix() + ttl
	}
	raw, err := json.Marshal(groupListCursor{Installation: installation, CatalogIncarnation: page.CatalogIncarnation,
		RevisionDigest: page.RevisionDigest, After: page.Groups[len(page.Groups)-1].Config.GroupID, ExpiresAt: expires})
	if err != nil {
		return nil, nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(groupListSignature(&key, raw))
	return page.Groups, &token, nil
}

func changeNativeGroup(ctx context.Context, state *sqliteowner.ProtectedState, cat *catalog.Store,
	request model.GroupAdminRequest, waitMilliseconds int64) (_ model.GroupSummary, resultErr error) {
	if request.Config != nil {
		raw, err := json.Marshal(request.Config)
		if err != nil || len(raw) > 32768 {
			return model.GroupSummary{}, catalog.ErrGroupInput
		}
		scanner, err := policy.NewScanner(32768)
		if err != nil || scanner.CheckContent(raw) != nil {
			return model.GroupSummary{}, app.ErrAdminDenied
		}
		for _, member := range request.Config.Members {
			root, err := cat.ReadWorktreeRoot(ctx, member.WorktreeID, state.Check)
			if err != nil || !state.TargetApproved(root) {
				return model.GroupSummary{}, app.ErrAdminDenied
			}
		}
	}
	wait, err := nativeMilliseconds(waitMilliseconds)
	if err != nil {
		return model.GroupSummary{}, err
	}
	operation, err := nativeOperationID()
	if err != nil {
		return model.GroupSummary{}, err
	}
	writer, err := runtime.AcquireCatalogMutation(ctx, state.CatalogLease(), operation, wait)
	if err != nil {
		return model.GroupSummary{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	authorized, err := cat.WithAuthorizedMutation(ctx, writer, operation, state.Check)
	if err != nil {
		return model.GroupSummary{}, err
	}
	return authorized.ChangeGroup(ctx, request, time.Now())
}
