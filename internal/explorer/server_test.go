package explorer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const homeDocument = "<!doctype html><title>Local Explorer fixture</title>"

type fixtureReader struct {
	reads atomic.Int64
	check atomic.Bool
}

func (r *fixtureReader) Check(ctx context.Context) error {
	if !r.check.Load() {
		return app.ErrValidationAuthority
	}
	return ctx.Err()
}

func (r *fixtureReader) Read(ctx context.Context, request model.Request, options app.CoreCallOptions) (ReadResult, error) {
	r.reads.Add(1)
	if _, ok := request.(model.StatusRequest); !ok {
		return ReadResult{}, app.ErrCoreUnsupported
	}
	response := emptyResponse()
	response.RequestID = options.RequestID
	response.Data = model.StatusData{Operation: "status", ProjectStage: "unknown", IndexState: "missing",
		Scopes: []model.ScopeMember{}, Capabilities: []model.Capability{}, RegisteredScopes: []model.RegisteredScopeStatus{}}
	body, err := encode(response)
	return ReadResult{Body: body, Recheck: r.Check}, err
}

func fixtureServer(t *testing.T, ttl time.Duration) (*Server, *fixtureReader, string) {
	t.Helper()
	r := &fixtureReader{}
	r.check.Store(true)
	s, code, err := Listen(context.Background(), Options{Reader: r, QueryTimeout: 2 * time.Second, BearerTTL: ttl,
		Assets: fstest.MapFS{"index.html": {Data: []byte(homeDocument)}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, r, code
}

func request(s *Server, method, path, body, token string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host, req.RemoteAddr = s.host, "127.0.0.1:49001"
	req.Header.Set("Origin", s.Origin())
	if method == http.MethodPost {
		req.Header.Set(CSRFHeader, CSRFValue)
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func respond(s *Server, r *http.Request) *httptest.ResponseRecorder {
	out := httptest.NewRecorder()
	s.ServeHTTP(out, r)
	return out
}

func bootstrapBearer(t *testing.T, s *Server, code string) model.BootstrapData {
	t.Helper()
	out := respond(s, request(s, http.MethodPost, APIPath+"/session/bootstrap", `{"code":"`+code+`"}`, ""))
	if out.Code != http.StatusOK {
		t.Fatalf("bootstrap status %d", out.Code)
	}
	var payload struct{ Data model.BootstrapData }
	if err := json.Unmarshal(out.Body.Bytes(), &payload); err != nil || len(payload.Data.Bearer) != tokenLength {
		t.Fatal("invalid bootstrap response")
	}
	if out.Header().Get("Cache-Control") != "no-store" || out.Header().Get("Set-Cookie") != "" ||
		out.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("invalid bootstrap transport headers")
	}
	return payload.Data
}

func TestExplorerExactOriginAndReadRoutes(t *testing.T) {
	s, owner, code := fixtureServer(t, 30*time.Minute)
	session := bootstrapBearer(t, s, code)
	bearer := session.Bearer
	for _, c := range []struct {
		name, method, endpoint, body string
		change                       func(*http.Request)
		status                       int
	}{
		{"host", "GET", "/workspace", "", func(r *http.Request) { r.Host = "localhost:" + strings.Split(s.host, ":")[1] }, 403},
		{"non-loopback peer", "GET", "/workspace", "", func(r *http.Request) { r.RemoteAddr = "192.0.2.1:49000" }, 403},
		{"different port", "POST", "/source/read", "{}", func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:1") }, 403},
		{"null frame origin", "POST", "/source/read", "{}", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"cross-origin browser context", "GET", "/workspace", "", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, 403},
		{"missing intent", "POST", "/graph/query", "{}", func(r *http.Request) { r.Header.Del(CSRFHeader) }, 403},
		{"duplicate credential", "GET", "/workspace", "", func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+bearer) }, 401},
		{"cookie only", "GET", "/workspace", "", func(r *http.Request) {
			r.Header.Del("Authorization")
			r.AddCookie(&http.Cookie{Name: "session", Value: bearer})
		}, 401},
		{"workspace body", "GET", "/workspace", "{}", nil, 400},
		{"workspace repeated query", "GET", "/workspace?verify=none&verify=none", "", nil, 400},
		{"source oversize", "POST", "/source/read", strings.Repeat(" ", 32769), func(r *http.Request) { r.ContentLength = -1; r.TransferEncoding = []string{"chunked"} }, 400},
		{"preview status ahead of gate", "POST", "/preview/status", "{}", nil, 404},
		{"preview mode ahead of gate", "POST", "/preview/mode", "{}", nil, 404},
		{"CLI permission route absent", "POST", "/preview-permission", "{}", nil, 404},
		{"proxy route absent", "GET", "/proxy", "", nil, 404},
		{"registry route absent", "GET", "/registry", "", nil, 404},
		{"preflight cannot authorize", "OPTIONS", "/source/read", "", nil, 404},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := request(s, c.method, APIPath+c.endpoint, c.body, bearer)
			if c.change != nil {
				c.change(in)
			}
			got := respond(s, in)
			if got.Code != c.status || got.Header().Get("Access-Control-Allow-Origin") != "" ||
				strings.Contains(got.Body.String(), bearer) || strings.Contains(got.Body.String(), code) {
				t.Fatalf("unexpected boundary status %d, want %d", got.Code, c.status)
			}
		})
	}
	if owner.reads.Load() != 0 {
		t.Fatal("rejected operation reached the reader")
	}
	legit := request(s, "GET", APIPath+"/workspace?verify=none", "", bearer)
	legit.Header.Del("Origin")
	legit.Header.Set("Sec-Fetch-Site", "same-origin")
	if out := respond(s, legit); out.Code != 200 || owner.reads.Load() != 1 {
		t.Fatalf("same-origin GET failed with %d", out.Code)
	}
	owner.check.Store(false)
	if got := respond(s, legit); got.Code != 403 || strings.Contains(got.Body.String(), `"operation":"status"`) {
		t.Fatal("authority loss at delivery exposed response")
	}
}

func TestExplorerCredentialsBoundedAndSingleUse(t *testing.T) {
	issued := time.Now()
	creds, code, err := newCredentials(issued, issued.Add(3*time.Second), 30*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int64
	var bearer string
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			b, end, reason := creds.exchange(code, issued)
			if reason == "" {
				bearer = b
				successes.Add(1)
				if !end.Equal(issued.Add(3 * time.Second)) {
					t.Error("source authorization did not cap the bearer")
				}
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 || creds.authenticate(bearer, issued.Add(time.Second)) != "" ||
		creds.authenticate(bearer, issued.Add(3*time.Second)) != "AUTH_EXPIRED" ||
		creds.authenticate(bearer, issued.Add(2*time.Second)) != "AUTH_EXPIRED" {
		t.Fatal("credential lifetime or single-use violation")
	}
	for range attemptLimit {
		if !creds.countAttempt(issued) {
			t.Fatal("allowed attempt refused")
		}
	}
	if creds.countAttempt(issued.Add(59*time.Second)) || !creds.countAttempt(issued.Add(time.Minute)) {
		t.Fatal("invalid attempt window")
	}
	stale, expiredCode, err := newCredentials(issued, time.Time{}, bearerTTL, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, denial := stale.exchange(expiredCode, issued.Add(bootstrapTTL)); denial != "AUTH_EXPIRED" {
		t.Fatal("expired bootstrap accepted")
	}
}

func TestExplorerSessionUntilExitPreservesAuthority(t *testing.T) {
	owner := &fixtureReader{}
	owner.check.Store(true)
	s, code, err := Listen(context.Background(), Options{Reader: owner, QueryTimeout: time.Second, BearerTTL: time.Second,
		SessionUntilExit: true, Assets: fstest.MapFS{"index.html": {Data: []byte(homeDocument)}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := s.auth.codeTime.start
	s.now = func() time.Time { return now }
	session := bootstrapBearer(t, s, code)
	if session.ExpiresAt != nil {
		t.Fatal("process session returned a timed expiry")
	}
	if out := respond(s, request(s, "POST", APIPath+"/session/bootstrap", `{"code":"`+code+`"}`, "")); out.Code != 401 {
		t.Fatalf("one-time code reused: %d", out.Code)
	}
	poll := request(s, "GET", APIPath+"/workspace", "", session.Bearer)
	now = now.Add(24 * time.Hour)
	if out := respond(s, poll); out.Code != 200 {
		t.Fatalf("process session expired: %d", out.Code)
	}
	owner.check.Store(false)
	if out := respond(s, poll); out.Code != 403 {
		t.Fatalf("revoked source permission was accepted: %d", out.Code)
	}
	owner.check.Store(true)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.auth.authenticate(session.Bearer, now) != "AUTH_INVALID" {
		t.Fatal("closed server retained its bearer")
	}

	cutoff := now.Add(time.Hour)
	limited, code, err := newCredentials(now, cutoff, time.Second, true)
	if err != nil {
		t.Fatal(err)
	}
	bearer, end, reason := limited.exchange(code, now)
	if reason != "" || !end.Equal(cutoff) || limited.authenticate(bearer, now.Add(time.Minute)) != "" ||
		limited.authenticate(bearer, cutoff) != "AUTH_EXPIRED" {
		t.Fatal("process session did not honor source authorization deadline")
	}
}

func TestExplorerPollDoesNotRenewBearer(t *testing.T) {
	s, owner, code := fixtureServer(t, 2*time.Second)
	now := s.auth.codeTime.start
	s.now = func() time.Time { return now }
	session := bootstrapBearer(t, s, code)
	poll := request(s, "GET", APIPath+"/workspace", "", session.Bearer)
	now = now.Add(time.Second)
	for range 2 {
		if out := respond(s, poll); out.Code != 200 {
			t.Fatalf("poll failed: %d", out.Code)
		}
	}
	now = now.Add(time.Second)
	if out := respond(s, poll); out.Code != 401 || !strings.Contains(out.Body.String(), "AUTH_EXPIRED") || owner.reads.Load() != 2 {
		t.Fatal("polling renewed a bearer or dispatched an expired session")
	}
}

func TestExplorerRealLoopbackLifecycle(t *testing.T) {
	s, _, _ := fixtureServer(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- s.Serve(ctx) }()
	client := &http.Client{Transport: &http.Transport{}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	got, err := client.Get(s.Origin() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(got.Body)
	_ = got.Body.Close()
	if err != nil || string(body) != homeDocument || got.StatusCode != 200 ||
		!strings.Contains(got.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("fixture assets returned unexpected response %d", got.StatusCode)
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("foreground loopback server did not stop")
	}
	if resp, err := client.Get(s.Origin() + "/"); err == nil {
		resp.Body.Close()
		t.Fatal("listener remained available after shutdown")
	}
}
