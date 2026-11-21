package explorer

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

const (
	bootstrapTTL = time.Minute
	bearerTTL    = 30 * time.Minute
	attemptLimit = 5
	tokenLength  = 43 // 32 random bytes, raw base64url
)

type lifetime struct {
	start time.Time
	end   time.Time
}

// Check both wall time and the process monotonic deadline. Expiration latches
// in the caller so clock changes cannot revive an expired credential.
func (l lifetime) live(now time.Time) bool {
	return !now.Before(l.start) && !now.UTC().Before(l.start.UTC()) &&
		now.Before(l.end) && now.UTC().Before(l.end.UTC())
}

type credentials struct {
	mu          sync.Mutex
	bootstrap   [32]byte
	bearer      [32]byte
	hasCode     bool
	hasBearer   bool
	expired     bool
	cutoff      time.Time
	ttl         time.Duration
	codeTime    lifetime
	bearerTime  lifetime
	lastAttempt [attemptLimit]time.Time
	attempts    int
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func tokenHash(token string) ([32]byte, bool) {
	if len(token) != tokenLength {
		return [32]byte{}, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return sha256.Sum256([]byte(token)), err == nil && len(raw) == 32
}

func newCredentials(now, cutoff time.Time, ttl time.Duration) (*credentials, string, error) {
	if ttl <= 0 || ttl > bearerTTL {
		ttl = bearerTTL
	}
	code, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	digest, _ := tokenHash(code)
	return &credentials{bootstrap: digest, hasCode: true, cutoff: cutoff, ttl: ttl, codeTime: lifetime{now, now.Add(bootstrapTTL)}}, code, nil
}

func (c *credentials) countAttempt(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	cut := now.Add(-time.Minute)
	kept := 0
	for i := 0; i < c.attempts; i++ {
		if c.lastAttempt[i].After(cut) {
			c.lastAttempt[kept] = c.lastAttempt[i]
			kept++
		}
	}
	c.attempts = kept
	if kept == attemptLimit {
		return false
	}
	c.lastAttempt[kept] = now
	c.attempts++
	return true
}

func (c *credentials) exchange(code string, now time.Time) (string, time.Time, string) {
	digest, valid := tokenHash(code)
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason := c.checkCode(digest, valid, now); reason != "" {
		return "", time.Time{}, reason
	}
	bearer, err := randomToken()
	if err != nil {
		return "", time.Time{}, "INTERNAL_ERROR"
	}
	end := now.Add(c.ttl)
	if !c.cutoff.IsZero() && c.cutoff.Before(end) {
		end = c.cutoff
	}
	c.bearer, _ = tokenHash(bearer)
	c.hasBearer, c.hasCode = true, false
	c.bootstrap = [32]byte{}
	c.bearerTime = lifetime{now, end}
	return bearer, end.UTC(), ""
}

func (c *credentials) validateCode(code string, now time.Time) string {
	digest, valid := tokenHash(code)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.checkCode(digest, valid, now)
}

func (c *credentials) checkCode(digest [32]byte, valid bool, now time.Time) string {
	if !c.hasCode {
		return "AUTH_INVALID"
	}
	if !c.codeTime.live(now) || !c.cutoff.IsZero() && !now.Before(c.cutoff) {
		c.hasCode = false
		c.bootstrap = [32]byte{}
		return "AUTH_EXPIRED"
	}
	if !valid || subtle.ConstantTimeCompare(digest[:], c.bootstrap[:]) != 1 {
		return "AUTH_INVALID"
	}
	return ""
}

func (c *credentials) authenticate(token string, now time.Time) string {
	digest, valid := tokenHash(token)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !valid || !c.hasBearer || subtle.ConstantTimeCompare(digest[:], c.bearer[:]) != 1 {
		return "AUTH_INVALID"
	}
	if c.expired || !c.bearerTime.live(now) {
		c.expired = true
		return "AUTH_EXPIRED"
	}
	return ""
}

func (c *credentials) revoke() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hasCode, c.hasBearer = false, false
	c.bootstrap, c.bearer = [32]byte{}, [32]byte{}
}
