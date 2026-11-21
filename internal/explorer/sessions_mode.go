package explorer

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const modeAckTTL = 3 * time.Second

var ErrModeAckTimeout = errors.New("preview mode acknowledgement expired")

func (p *previewSession) disarm() {
	p.mode, p.command, p.commandWindow = "inspect", "", lifetime{}
	p.epoch++
}

func (p previewSession) currentMode(now time.Time) string {
	if p.mode == "arming" && !p.commandWindow.live(now) {
		return "inspect"
	}
	return p.mode
}

func (s *PreviewSessions) checkInteract(ctx context.Context, p previewSession, now func() time.Time) error {
	permit := p.permission.Profile
	if p.state != "bound" || !permit.AllowInteract || permit.TargetEnvironment != "isolated_test" && permit.TargetEnvironment != "local_mock" {
		return ErrPreviewDenied
	}
	if err := s.recheck(ctx, p, now()); err != nil {
		return err
	}
	binding, err := s.authority.Binding(ctx, p.scope, p.build)
	if err != nil {
		return err
	}
	if binding != p.binding {
		return ErrPreviewMapMissing
	}
	return s.recheck(ctx, p, now())
}

func (s *PreviewSessions) mode(ctx context.Context, actor [32]byte, r model.PreviewModeRequest, now func() time.Time) (previewReply, error) {
	row, err := s.preview(ctx, actor, r.SessionID, now())
	if err != nil {
		return previewReply{}, err
	}
	if row.revision != r.ExpectedRevision || row.epoch != r.ExpectedAuthorizationEpoch || row.permission.ProfileID != r.PermissionProfileID ||
		row.state != "bound" {
		return previewReply{}, ErrPreviewChanged
	}
	switch r.Mode {
	case "interact":
		if row.currentMode(now()) != "inspect" {
			return previewReply{}, ErrPreviewChanged
		}
		if err := s.checkInteract(ctx, row, now); err != nil {
			return previewReply{}, err
		}
	case "inspect":
	default:
		return previewReply{}, ErrPreviewChanged
	}
	command, err := requestID()
	if err != nil {
		return previewReply{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[row.id]
	if !ok || current.revision != row.revision {
		return previewReply{}, ErrPreviewChanged
	}
	issued := now()
	row = current
	row.revision++
	row.disarm()
	if r.Mode == "interact" {
		if row.expired || !row.window.live(issued) || ctx.Err() != nil {
			return previewReply{}, ErrPreviewExpired
		}
		row.mode, row.command = "arming", command
		row.commandWindow = lifetime{issued, issued.Add(modeAckTTL)}
	}
	s.rows[row.id] = row
	// An Inspect command is best-effort. It is never persisted as pending.
	row.command = command
	if r.Mode == "inspect" {
		return previewReply{data: row.control("preview_mode", nil).data}, nil
	}
	return row.control("preview_mode", nil), nil
}

func (s *PreviewSessions) ack(ctx context.Context, actor [32]byte, r model.PreviewAckRequest, now func() time.Time) (previewReply, error) {
	row, err := s.preview(ctx, actor, r.SessionID, now())
	if err != nil {
		return previewReply{}, err
	}
	if row.state != "bound" || row.mode != "arming" || row.revision != r.ExpectedRevision ||
		row.epoch != r.AuthorizationEpoch || row.command != r.CommandID {
		return previewReply{}, ErrPreviewChanged
	}
	var denial error
	if !row.commandWindow.live(now()) {
		denial = ErrModeAckTimeout
	} else if r.Acknowledged {
		denial = s.checkInteract(ctx, row, now)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[row.id]
	if !ok || current.revision != row.revision {
		return previewReply{}, ErrPreviewChanged
	}
	row = current
	if !row.commandWindow.live(now()) {
		denial = ErrModeAckTimeout
	} else if row.expired || !row.window.live(now()) {
		denial = ErrPreviewExpired
	} else if ctx.Err() != nil {
		denial = ctx.Err()
	}
	row.revision++
	if !r.Acknowledged || denial != nil {
		row.disarm()
		s.rows[row.id] = row
		if denial != nil {
			return previewReply{}, denial
		}
		return previewReply{data: row.control("preview_ack", nil).data}, nil
	}
	row.mode, row.command, row.commandWindow = "interact", "", lifetime{}
	s.rows[row.id] = row
	return row.control("preview_ack", nil), nil
}
