package lsp

import (
	"context"
	"errors"
	"sync"
)

var ErrLSPSessionLimit = errors.New("LSP session limit reached")

// SessionLimiter is an in-process quota, not a machine-wide lock. The owning
// application must share one instance across its sessions and initialize it
// from the effective runtime configuration. Zero selects the default of one.
type SessionLimiter struct{ slots chan struct{} }

func NewSessionLimiter(max int64) (*SessionLimiter, error) {
	if max == 0 {
		max = 1
	}
	if max < 1 || max > 8 {
		return nil, ErrLSPSessionLimit
	}
	return &SessionLimiter{slots: make(chan struct{}, int(max))}, nil
}

type sessionPermit struct {
	limiter *SessionLimiter
	once    sync.Once
}

func (l *SessionLimiter) acquire(ctx context.Context) (*sessionPermit, error) {
	if l == nil || l.slots == nil || ctx == nil {
		return nil, ErrLSPSessionLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case l.slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-l.slots
			return nil, err
		}
		return &sessionPermit{limiter: l}, nil
	default:
		return nil, ErrLSPSessionLimit
	}
}

func (p *sessionPermit) release() {
	if p == nil || p.limiter == nil {
		return
	}
	p.once.Do(func() { <-p.limiter.slots })
}
