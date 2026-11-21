package explorer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func TestModeRequiresCurrentAckAndDisarmInvalidatesIt(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	clock := start
	now := func() time.Time { return clock }
	h := strings.Repeat("a", 64)
	id, worktree := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	origin := "http://127.0.0.1:9000"
	p := model.PreviewPermissionSummary{ProfileID: "fixture", Version: 1, EffectiveStatus: "approved", Digest: h,
		Profile: model.PreviewPermissionProfile{AllowAttach: true, AllowInteract: true, TargetEnvironment: "isolated_test",
			SourceWorktreeIDs: []string{worktree}, ApprovedOrigin: origin, ApprovedPathPrefix: "/app",
			ExpiresAt: start.Add(time.Minute).UTC().Format(time.RFC3339Nano)}}
	scope := model.ScopeVector{VectorDigest: h, Members: []model.ScopeMember{{WorktreeID: worktree}}}
	authority := &fixturePreviewAuthority{scope: scope, permission: p}
	binding, err := authority.Binding(ctx, scope, h)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewPreviewSessions(authority, nil, 24576)
	actor := [32]byte{1}
	manager.rows[id] = previewSession{actor: actor, scope: scope, window: lifetime{start, start.Add(time.Minute)},
		id: id, build: h, state: "bound", mode: "inspect", permission: p, origin: origin, path: "/app", revision: 2, binding: binding}

	arm := func(revision, epoch int64) model.PreviewAckRequest {
		t.Helper()
		result, err := manager.mode(ctx, actor, model.PreviewModeRequest{SessionID: id, ExpectedRevision: revision,
			ExpectedAuthorizationEpoch: epoch, PermissionProfileID: p.ProfileID, Mode: "interact"}, now)
		if err != nil {
			t.Fatal(err)
		}
		data := result.data.(model.PreviewData)
		if data.Revision != revision+1 || data.AuthorizationEpoch != epoch+1 || data.ModeState != "arming" || data.CommandID == nil {
			t.Fatalf("invalid command: %+v", data)
		}
		return model.PreviewAckRequest{SessionID: id, ExpectedRevision: data.Revision, AuthorizationEpoch: data.AuthorizationEpoch,
			CommandID: *data.CommandID, Acknowledged: true}
	}
	probe := func(expected string, actualAllowed bool) model.PreviewStatusData {
		t.Helper()
		result, err := manager.status(ctx, actor, id, now)
		if err != nil {
			t.Fatal(err)
		}
		data := result.data.(model.PreviewStatusData)
		if data.ModeState != expected || data.InteractionAllowed != actualAllowed {
			t.Fatalf("mode state %s: %+v", expected, data)
		}
		return data
	}

	late := arm(2, 0)
	probe("arming", false)
	wrong := late
	wrong.AuthorizationEpoch--
	if _, err := manager.ack(ctx, actor, wrong, now); !errors.Is(err, ErrPreviewChanged) {
		t.Fatalf("old epoch accepted: %v", err)
	}
	clock = start.Add(modeAckTTL + time.Millisecond)
	probe("inspect", false)
	if _, err := manager.ack(ctx, actor, late, now); !errors.Is(err, ErrModeAckTimeout) {
		t.Fatalf("late acknowledgement accepted: %v", err)
	}
	after := probe("inspect", false)
	valid := arm(after.Revision, after.AuthorizationEpoch)
	result, err := manager.ack(ctx, actor, valid, now)
	if err != nil || result.data.(model.PreviewData).ModeState != "interact" {
		t.Fatalf("valid acknowledgement failed: %+v %v", result, err)
	}
	after = probe("interact", true)
	result, err = manager.mode(ctx, actor, model.PreviewModeRequest{SessionID: id, ExpectedRevision: after.Revision,
		ExpectedAuthorizationEpoch: after.AuthorizationEpoch, PermissionProfileID: p.ProfileID, Mode: "inspect"}, now)
	if err != nil || result.data.(model.PreviewData).ModeState != "inspect" {
		t.Fatalf("disarm failed: %+v %v", result, err)
	}
	probe("inspect", false)
	if _, err := manager.ack(ctx, actor, valid, now); !errors.Is(err, ErrPreviewChanged) {
		t.Fatalf("disarmed acknowledgement accepted: %v", err)
	}
}
