//go:build linux

package compose

import (
	"context"
	"encoding/base64"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func RunNativePreviewPermission(ctx context.Context, installation, config string, request model.PreviewPermissionAdminRequest, cpus int) (out model.PreviewPermissionAdminData, next *string, resultErr error) {
	state, err := sqliteowner.OpenProtectedState(ctx, installation)
	if err != nil {
		return out, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	configService, err := protectedConfig(ctx, state, config, cpus)
	if err != nil {
		return out, nil, err
	}
	effective, _, err := configService.StartupConfig()
	if err != nil {
		return out, nil, err
	}
	cat, err := state.OpenCatalog(ctx)
	if err != nil {
		return out, nil, err
	}
	out = model.PreviewPermissionAdminData{Operation: "preview_permission_admin", Profiles: []model.PreviewPermissionSummary{}}
	now := time.Now()
	switch request.Action {
	case "get":
		if request.ProfileID == nil {
			return out, nil, app.ErrCoreInput
		}
		item, err := cat.ReadPreviewPermission(ctx, *request.ProfileID, now, state.Check)
		if err != nil {
			return out, nil, err
		}
		out.Profiles = append(out.Profiles, item)
		return out, nil, nil
	case "list":
		if request.Limit == nil {
			return out, nil, app.ErrCoreInput
		}
		after := ""
		if request.Cursor != nil {
			decoded, err := base64.RawURLEncoding.Strict().DecodeString(*request.Cursor)
			if err != nil || !utf8.Valid(decoded) || len(decoded) == 0 || utf8.RuneCount(decoded) > 192 {
				return out, nil, app.ErrCoreInput
			}
			after = string(decoded)
		}
		items, more, err := cat.ListPreviewPermissions(ctx, after, int(*request.Limit), now, state.Check)
		if err != nil {
			return out, nil, err
		}
		out.Profiles = items
		if more {
			value := base64.RawURLEncoding.EncodeToString([]byte(items[len(items)-1].ProfileID))
			next = &value
		}
		return out, next, nil
	case "put", "revoke":
	default:
		return out, nil, app.ErrCoreInput
	}
	scanner, err := policy.NewScanner(4096)
	if err != nil {
		return out, nil, err
	}
	if request.Reason != nil && scanner.CheckContent([]byte(*request.Reason)) != nil {
		return out, nil, app.ErrAdminDenied
	}
	if request.Profile != nil {
		for _, worktree := range request.Profile.SourceWorktreeIDs {
			root, err := cat.ReadWorktreeRoot(ctx, worktree, state.Check)
			if err != nil || !state.TargetApproved(root) {
				return out, nil, app.ErrAdminDenied
			}
		}
	}
	wait, err := nativeMilliseconds(effective.Core.WriterLockWaitMs)
	if err != nil {
		return out, nil, err
	}
	operation, err := nativeOperationID()
	if err != nil {
		return out, nil, err
	}
	writer, err := runtime.AcquireCatalogMutation(ctx, state.CatalogLease(), operation, wait)
	if err != nil {
		return out, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	authorized, err := cat.WithAuthorizedMutation(ctx, writer, operation, state.Check)
	if err != nil {
		return out, nil, err
	}
	item, err := authorized.ChangePreviewPermission(ctx, request, "installation:"+state.InstallationID(), time.Now())
	if err != nil {
		return out, nil, err
	}
	out.Profiles = append(out.Profiles, item)
	return out, nil, nil
}
