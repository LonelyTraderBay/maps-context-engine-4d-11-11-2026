package explorer

import (
	"context"
	"crypto/subtle"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrPreviewDenied     = errors.New("preview permission denied")
	ErrPreviewChanged    = errors.New("preview session state changed")
	ErrPreviewMapMissing = errors.New("preview build is unbound")
	ErrPreviewCapacity   = errors.New("preview session limit")
	ErrPreviewExpired    = errors.New("preview session expired")
)

type PreviewBinding struct {
	ID             string
	ResolverDigest string
}

// PreviewAuthority provides only checked catalog heads and scoped published
// metadata. It never loads a preview URL or receives browser credentials.
type PreviewAuthority interface {
	Permission(context.Context, string, time.Time) (model.PreviewPermissionSummary, error)
	Resolve(context.Context, *model.ScopeRequest) (model.ScopeVector, error)
	Recheck(context.Context, model.ScopeVector) error
	Binding(context.Context, model.ScopeVector, string) (PreviewBinding, error)
	ResolveAnchor(context.Context, model.ScopeVector, PreviewBinding, model.DOMAnchorRef) (*model.MappingMatch, error)
	ResolveTarget(context.Context, model.ScopeVector, PreviewBinding, string, model.GraphRef, string, int) ([]model.MappingMatch, int64, error)
}

const (
	maxPreviewSessions = 32
	detachedRetention  = time.Minute
)

type PreviewSessions struct {
	authority PreviewAuthority
	cursors   *contextpack.CursorCodec
	outputCap int64
	mu        sync.Mutex
	rows      map[string]previewSession
	closed    bool
}

type previewSession struct {
	actor            [32]byte
	nonce            [32]byte
	scope            model.ScopeVector
	window           lifetime
	id, build, state string
	mode, command    string
	commandWindow    lifetime
	permission       model.PreviewPermissionSummary
	origin, path     string
	binding          PreviewBinding
	revision, epoch  int64
	detachedAt       time.Time
	expired          bool
}

type previewReply struct {
	scope         *model.ScopeVector
	data          model.ResponseData
	limit         int64
	pagination    *model.Pagination
	problem       *model.ErrorInfo
	scrollAllowed bool
}

func NewPreviewSessions(authority PreviewAuthority, cursors *contextpack.CursorCodec, outputCap int64) *PreviewSessions {
	if authority == nil {
		return nil
	}
	return &PreviewSessions{authority: authority, cursors: cursors, outputCap: outputCap, rows: make(map[string]previewSession)}
}

func (s *PreviewSessions) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	clear(s.rows)
}

func (p previewSession) control(operation string, nonce *string) previewReply {
	data := model.PreviewData{Operation: operation, SessionID: p.id, Revision: p.revision,
		DocumentEpoch: 1, HMREpoch: 0, AuthorizationEpoch: p.epoch, State: p.state, ModeState: p.mode,
		PairingNonce: nonce, ExpiresAt: p.window.end.UTC().Format(time.RFC3339Nano),
		PermissionVersion: p.permission.Version}
	if p.binding.ID != "" {
		data.BindingID, data.ResolverDigest = &p.binding.ID, &p.binding.ResolverDigest
	}
	if p.command != "" {
		data.CommandID = &p.command
	}
	return previewReply{scope: &p.scope, data: data}
}

func (s *PreviewSessions) attach(ctx context.Context, actor [32]byte, bearerEnd time.Time, r model.PreviewAttachRequest, now func() time.Time) (previewReply, error) {
	if model.CheckLocalPreviewLocation(r.Origin, r.Path) != nil {
		return previewReply{}, ErrPreviewDenied
	}
	permission, err := s.authority.Permission(ctx, r.PermissionProfileID, now())
	if err != nil {
		return previewReply{}, err
	}
	scope, err := s.authority.Resolve(ctx, r.Scope)
	if err != nil {
		return previewReply{}, err
	}
	attached := now()
	if scope.Kind != "single" || len(scope.Members) != 1 || len(scope.UnavailableMembers) != 0 ||
		model.VerifyScopeVectorDigest(scope, 32768) != nil || permission.ProfileID != r.PermissionProfileID ||
		!allowedPreview(permission, scope.Members[0].WorktreeID, r.Origin, r.Path) {
		return previewReply{}, ErrPreviewDenied
	}
	expires, err := time.Parse(time.RFC3339Nano, permission.Profile.ExpiresAt)
	if err != nil {
		return previewReply{}, ErrPreviewDenied
	}
	remaining := min(30*time.Minute, expires.Sub(attached))
	if !bearerEnd.IsZero() {
		remaining = min(remaining, bearerEnd.Sub(attached))
	}
	if remaining <= 0 {
		return previewReply{}, ErrPreviewExpired
	}
	binding, err := s.authority.Binding(ctx, scope, r.BuildMapID)
	if err != nil {
		return previewReply{}, err
	}
	if !model.ValidDigest(binding.ID) || !model.ValidDigest(binding.ResolverDigest) {
		return previewReply{}, ErrPreviewMapMissing
	}
	id, err := requestID()
	if err != nil {
		return previewReply{}, err
	}
	nonce, err := randomToken()
	if err != nil {
		return previewReply{}, err
	}
	digest, _ := tokenHash(nonce)
	row := previewSession{actor: actor, nonce: digest, scope: scope, window: lifetime{attached, attached.Add(remaining)},
		id: id, build: r.BuildMapID, state: "pairing", mode: "inspect", permission: permission, origin: r.Origin, path: r.Path, revision: 1}
	if err := s.recheck(ctx, row, now()); err != nil {
		return previewReply{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return previewReply{}, ErrPreviewDenied
	}
	for id, current := range s.rows {
		if attached.After(current.window.end.Add(detachedRetention)) ||
			!current.detachedAt.IsZero() && attached.Sub(current.detachedAt) > detachedRetention {
			delete(s.rows, id)
		}
	}
	if len(s.rows) >= maxPreviewSessions {
		return previewReply{}, ErrPreviewCapacity
	}
	s.rows[id] = row
	return row.control("preview_attach", &nonce), nil
}

func allowedPreview(permission model.PreviewPermissionSummary, worktree, origin, path string) bool {
	p := permission.Profile
	return permission.EffectiveStatus == "approved" && p.AllowAttach && slices.Contains(p.SourceWorktreeIDs, worktree) &&
		model.VerifyPreviewEventOrigin(p.ApprovedOrigin, origin) == nil &&
		model.VerifyCanonicalPreviewPathPrefix(p.ApprovedPathPrefix, path) == nil
}

func (s *PreviewSessions) preview(ctx context.Context, actor [32]byte, id string, now time.Time) (previewSession, error) {
	if err := ctx.Err(); err != nil {
		return previewSession{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if s.closed || !ok || subtle.ConstantTimeCompare(actor[:], row.actor[:]) != 1 ||
		!row.detachedAt.IsZero() && now.Sub(row.detachedAt) > detachedRetention {
		return previewSession{}, ErrPreviewDenied
	}
	if !row.expired && !row.window.live(now) {
		row.expired = true
		s.rows[id] = row
	}
	return row, nil
}

func (s *PreviewSessions) recheck(ctx context.Context, row previewSession, now time.Time) error {
	if err := s.authority.Recheck(ctx, row.scope); err != nil {
		return err
	}
	current, err := s.authority.Permission(ctx, row.permission.ProfileID, now)
	if err != nil {
		return err
	}
	if current.EffectiveStatus == "expired" || row.expired || !row.window.live(now) {
		return ErrPreviewExpired
	}
	if current.Version != row.permission.Version || current.Digest != row.permission.Digest ||
		!allowedPreview(current, row.scope.Members[0].WorktreeID, row.origin, row.path) {
		return ErrPreviewDenied
	}
	return ctx.Err()
}

func (s *PreviewSessions) bind(ctx context.Context, actor [32]byte, r model.PreviewBindRequest, now func() time.Time) (previewReply, error) {
	row, err := s.preview(ctx, actor, r.SessionID, now())
	if err != nil {
		return previewReply{}, err
	}
	nonce, valid := tokenHash(r.PairingNonce)
	if row.state != "pairing" || row.revision != r.ExpectedRevision || row.build != r.BuildMapID ||
		r.DocumentEpoch != 1 || r.HMREpoch != 0 || !valid || subtle.ConstantTimeCompare(row.nonce[:], nonce[:]) != 1 {
		return previewReply{}, ErrPreviewChanged
	}
	if err := s.recheck(ctx, row, now()); err != nil {
		return previewReply{}, err
	}
	binding, err := s.authority.Binding(ctx, row.scope, row.build)
	if err != nil {
		return previewReply{}, err
	}
	if !model.ValidDigest(binding.ID) || !model.ValidDigest(binding.ResolverDigest) {
		return previewReply{}, ErrPreviewMapMissing
	}
	if err := s.recheck(ctx, row, now()); err != nil {
		return previewReply{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[row.id]
	if !ok || current.revision != row.revision || current.expired || !current.window.live(now()) {
		return previewReply{}, ErrPreviewChanged
	}
	row.revision++
	row.nonce = [32]byte{}
	row.binding, row.state = binding, "bound"
	s.rows[row.id] = row
	return row.control("preview_bind", nil), nil
}

func (s *PreviewSessions) status(ctx context.Context, actor [32]byte, id string, now func() time.Time) (previewReply, error) {
	row, err := s.preview(ctx, actor, id, now())
	if err != nil {
		return previewReply{}, err
	}
	scope := &row.scope
	state, permissionStatus, version := row.state, "unavailable", row.permission.Version
	observed := now()
	head, err := s.authority.Permission(ctx, row.permission.ProfileID, observed)
	if err == nil {
		permissionStatus, version = head.EffectiveStatus, head.Version
	}
	var binding *string
	switch {
	case row.state == "detached":
		scope = nil
	case row.expired || !row.window.live(observed) || permissionStatus == "expired":
		state, scope = "blocked", nil
	case err != nil || head.Version != row.permission.Version || head.Digest != row.permission.Digest ||
		!allowedPreview(head, row.scope.Members[0].WorktreeID, row.origin, row.path):
		state, scope = "stale", nil
	default:
		if s.authority.Recheck(ctx, row.scope) != nil {
			state, scope, permissionStatus = "stale", nil, "unavailable"
		} else if row.state == "bound" {
			current, err := s.authority.Binding(ctx, row.scope, row.build)
			if err != nil || current != row.binding {
				state, scope = "stale", nil
			} else {
				binding = &row.binding.ID
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return previewReply{}, err
	}
	if state != "detached" {
		latest, readErr := s.authority.Permission(ctx, row.permission.ProfileID, now())
		if readErr != nil {
			state, scope, binding, permissionStatus = "stale", nil, nil, "unavailable"
		} else if latest.Version != version || latest.Digest != head.Digest || latest.EffectiveStatus != head.EffectiveStatus {
			state, scope, binding = "stale", nil, nil
			version, permissionStatus = latest.Version, latest.EffectiveStatus
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[id]
	if !ok || current.revision != row.revision {
		return previewReply{}, ErrPreviewChanged
	}
	delivered := now()
	if state != "detached" && !row.window.live(delivered) {
		current.expired = true
		s.rows[id] = current
		state, scope, binding = "blocked", nil, nil
	}
	mode := "inspect"
	if state == "bound" && binding != nil && permissionStatus == "approved" {
		mode = row.currentMode(delivered)
	}
	scrollAllowed := state == "bound" && binding != nil && permissionStatus == "approved" &&
		mode == "inspect" && head.Profile.AllowScroll
	return previewReply{scope: scope, scrollAllowed: scrollAllowed, data: model.PreviewStatusData{Operation: "preview_status", SessionID: id,
		Revision: row.revision, AuthorizationEpoch: row.epoch, DocumentEpoch: 1, HMREpoch: 0,
		State: state, ModeState: mode, BindingID: binding, PermissionProfileID: row.permission.ProfileID,
		PermissionVersion: version, PermissionStatus: permissionStatus, InteractionAllowed: mode == "interact",
		ExpiresAt: row.window.end.UTC().Format(time.RFC3339Nano), ServerTime: delivered.UTC().Format(time.RFC3339Nano)}}, nil
}

func (s *PreviewSessions) invalidate(ctx context.Context, actor [32]byte, id string, revision int64, detach bool, now time.Time) (previewReply, error) {
	row, err := s.preview(ctx, actor, id, now)
	if err != nil {
		return previewReply{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[id]
	if !ok || current.revision != row.revision || !detach && (revision != row.revision || row.state == "detached") {
		return previewReply{}, ErrPreviewChanged
	}
	operation := "preview_invalidate"
	if detach {
		operation = "preview_detach"
	}
	if row.state == "detached" {
		return previewReply{data: row.control(operation, nil).data}, nil
	}
	row.nonce, row.binding = [32]byte{}, PreviewBinding{}
	row.disarm()
	row.state = "stale"
	if detach {
		row.state, row.detachedAt = "detached", now
	}
	row.revision++
	s.rows[id] = row
	return previewReply{data: row.control(operation, nil).data}, nil
}
