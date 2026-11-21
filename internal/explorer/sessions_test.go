package explorer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type fixturePreviewAuthority struct {
	permission model.PreviewPermissionSummary
	scope      model.ScopeVector
}

func (a *fixturePreviewAuthority) Permission(ctx context.Context, _ string, _ time.Time) (model.PreviewPermissionSummary, error) {
	return a.permission, ctx.Err()
}

func (a *fixturePreviewAuthority) Resolve(ctx context.Context, _ *model.ScopeRequest) (model.ScopeVector, error) {
	return a.scope, ctx.Err()
}

func (a *fixturePreviewAuthority) Recheck(ctx context.Context, vector model.ScopeVector) error {
	if vector.VectorDigest != a.scope.VectorDigest {
		return ErrPreviewChanged
	}
	return ctx.Err()
}

func (a *fixturePreviewAuthority) Binding(ctx context.Context, _ model.ScopeVector, build string) (PreviewBinding, error) {
	if build != strings.Repeat("a", 64) {
		return PreviewBinding{}, ErrPreviewMapMissing
	}
	return PreviewBinding{ID: strings.Repeat("b", 64), ResolverDigest: strings.Repeat("c", 64)}, ctx.Err()
}

func (a *fixturePreviewAuthority) ResolveAnchor(ctx context.Context, _ model.ScopeVector, _ PreviewBinding, _ model.DOMAnchorRef) (*model.MappingMatch, error) {
	return nil, ctx.Err()
}

func (a *fixturePreviewAuthority) ResolveTarget(ctx context.Context, _ model.ScopeVector, _ PreviewBinding, _ string, _ model.GraphRef, _ string, _ int) ([]model.MappingMatch, int64, error) {
	return nil, 0, ctx.Err()
}

func TestPreviewHTTPStatusAndPermissionHead(t *testing.T) {
	s, _, code := fixtureServer(t, 30*time.Minute)
	start := time.Now()
	now := start
	s.now = func() time.Time { return now }
	worktree := "11111111-1111-4111-8111-111111111111"
	h := strings.Repeat("d", 64)
	scope := model.ScopeVector{Kind: "single", CatalogIncarnation: worktree, UnavailableMembers: []model.MemberUnavailable{},
		Members: []model.ScopeMember{{WorktreeID: worktree, ProfileID: h, GenerationID: worktree, SnapshotDigest: h, PolicyDigest: h,
			PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: worktree, RecordsIncarnation: worktree}}}
	digest, err := model.ComputeScopeVectorDigest(scope, 32768)
	if err != nil {
		t.Fatal(err)
	}
	scope.VectorDigest = digest
	a := &fixturePreviewAuthority{scope: scope, permission: model.PreviewPermissionSummary{
		ProfileID: "fixture", Version: 1, EffectiveStatus: "approved", Digest: h,
		Profile: model.PreviewPermissionProfile{AllowAttach: true, SourceWorktreeIDs: []string{worktree}, ApprovedOrigin: "http://127.0.0.1:9001",
			ApprovedPathPrefix: "/app", ExpiresAt: start.Add(time.Minute).UTC().Format(time.RFC3339Nano)}}}
	s.options.Preview = NewPreviewSessions(a, nil, 24576)
	bearer := bootstrapBearer(t, s, code).Bearer
	post := func(path string, payload any, want int) []byte {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		response := respond(s, request(s, http.MethodPost, APIPath+"/preview/"+path, string(raw), bearer))
		if response.Code != want {
			t.Fatalf("%s: got %d, want %d: %s", path, response.Code, want, response.Body)
		}
		if strings.Contains(response.Body.String(), bearer) {
			t.Fatal("preview response carried source credential")
		}
		return response.Body.Bytes()
	}
	attach := model.PreviewAttachRequest{Origin: a.permission.Profile.ApprovedOrigin, Path: "/application",
		BuildMapID: strings.Repeat("a", 64), PermissionProfileID: "fixture"}
	post("attach", attach, 403)
	attach.Path = "/app/child"
	original := a.permission.Profile.ApprovedOrigin
	attach.Origin, a.permission.Profile.ApprovedOrigin = "https://preview.example.invalid", "https://preview.example.invalid"
	post("attach", attach, 403)
	attach.Origin, a.permission.Profile.ApprovedOrigin = original, original
	var attached struct{ Data model.PreviewData }
	if err := json.Unmarshal(post("attach", attach, 200), &attached); err != nil || attached.Data.PairingNonce == nil {
		t.Fatalf("attach proof: %+v %v", attached, err)
	}
	binding := model.PreviewBindRequest{SessionID: attached.Data.SessionID, ExpectedRevision: 1,
		DocumentEpoch: 1, BuildMapID: attach.BuildMapID, PairingNonce: "wrong", Capabilities: []string{"heartbeat"}}
	post("bind", binding, 409)
	binding.PairingNonce = *attached.Data.PairingNonce
	var bound struct{ Data model.PreviewData }
	if err := json.Unmarshal(post("bind", binding, 200), &bound); err != nil || bound.Data.State != "bound" ||
		bound.Data.BindingID == nil || bound.Data.PairingNonce != nil || bound.Data.Revision != 2 {
		t.Fatalf("bind proof: %+v %v", bound, err)
	}
	post("bind", binding, 409)
	status := func() model.PreviewStatusData {
		t.Helper()
		var output struct{ Data model.PreviewStatusData }
		body := post("status", model.PreviewStatusRequest{SessionID: binding.SessionID}, 200)
		if strings.Contains(string(body), "pairing_nonce") {
			t.Fatal("status included pairing secret")
		}
		if err := json.Unmarshal(body, &output); err != nil {
			t.Fatal(err)
		}
		return output.Data
	}
	for step := range 3 {
		now = start.Add(time.Duration(step) * time.Second)
		current := status()
		if current.State != "bound" || current.Revision != 2 || current.PermissionVersion != 1 ||
			current.InteractionAllowed || current.ExpiresAt != attached.Data.ExpiresAt {
			t.Fatalf("status changed the session: %+v", current)
		}
	}
	other := [32]byte{1}
	if _, err := s.options.Preview.status(context.Background(), other, binding.SessionID, s.now); !errors.Is(err, ErrPreviewDenied) {
		t.Fatalf("different principal could read session: %v", err)
	}
	a.permission.Version, a.permission.EffectiveStatus, a.permission.Digest = 2, "revoked", strings.Repeat("e", 64)
	current := status()
	if current.PermissionVersion != 2 || current.PermissionStatus != "revoked" || current.State != "stale" ||
		current.Revision != 2 || current.BindingID != nil {
		t.Fatalf("current revoked head not reflected: %+v", current)
	}
}
