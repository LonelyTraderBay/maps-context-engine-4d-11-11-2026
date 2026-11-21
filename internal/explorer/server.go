// Package explorer owns the opt-in loopback HTTP transport and memory-only
// session. All graph and source authority comes from the installed app owner.
package explorer

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

const APIPath = "/api/explorer/v1"

const (
	CSRFHeader = "X-MCE4D-Intent"
	CSRFValue  = "explorer"
	ListenAddr = "127.0.0.1:0"
)

var ErrAssetsUnavailable = errors.New("EXPLORER_ASSETS_UNAVAILABLE")

// Reader represents one already authorized local binding. Check verifies the
// caller's permission even when a handler does not read a published snapshot.
type Reader interface {
	Check(context.Context) error
	Read(context.Context, model.Request, app.CoreCallOptions) (ReadResult, error)
}

type ReadResult struct {
	Body    []byte
	Recheck func(context.Context) error
}

type Options struct {
	Reader            Reader
	Preview           *PreviewSessions
	PreviewOrigins    func(context.Context, time.Time) ([]string, error)
	Records           *app.RecordService
	RecordOutputCap   int64
	Assets            fs.FS
	QueryTimeout      time.Duration
	BearerTTL         time.Duration
	AuthorizationEnds time.Time
}

type Server struct {
	listener net.Listener
	host     string
	origin   string
	options  Options
	assets   map[string]asset
	registry *contracts.Registry
	auth     *credentials
	now      func() time.Time
	busy     chan struct{}
	handlers sync.WaitGroup
	mu       sync.Mutex
	stopping bool
}

// Listen always takes an ephemerally assigned IPv4 loopback port.
// Its bootstrap code is for direct display in the initiating local console.
func Listen(ctx context.Context, options Options) (*Server, string, error) {
	if ctx == nil || options.Reader == nil || options.QueryTimeout <= 0 {
		return nil, "", app.ErrCoreInput
	}
	options.QueryTimeout = min(options.QueryTimeout, time.Minute)
	assets, err := loadAssets(options.Assets)
	if err != nil {
		return nil, "", err
	}
	check, cancel := context.WithTimeout(ctx, min(options.QueryTimeout, 15*time.Second))
	defer cancel()
	if err := options.Reader.Check(check); err != nil {
		return nil, "", err
	}
	registry, err := contracts.LoadRegistry()
	if err != nil {
		return nil, "", err
	}
	listener, err := (&net.ListenConfig{}).Listen(check, "tcp4", ListenAddr)
	if err != nil {
		return nil, "", err
	}
	auth, code, err := newCredentials(time.Now(), options.AuthorizationEnds, options.BearerTTL)
	if err != nil {
		return nil, "", errors.Join(err, listener.Close())
	}
	host := listener.Addr().String()
	return &Server{listener: listener, host: host, origin: "http://" + host,
		options: options, assets: assets, registry: registry, auth: auth,
		now: time.Now, busy: make(chan struct{}, 8)}, code, nil
}

func (s *Server) Origin() string { return s.origin }

func (s *Server) Close() error {
	s.auth.revoke()
	if s.options.Preview != nil {
		s.options.Preview.close()
	}
	err := s.listener.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// Serve is a foreground lifecycle. It drains owned handlers before the
// caller closes storage or releases the bound scope.
func (s *Server) Serve(ctx context.Context) error {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{
		Handler: s, BaseContext: func(net.Listener) context.Context { return run },
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 6 * time.Second,
		IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10,
		WriteTimeout: s.options.QueryTimeout + 8*time.Second,
		ErrorLog:     discardLog(),
	}
	stopped := make(chan error, 1)
	go func() {
		<-run.Done()
		s.mu.Lock()
		s.stopping = true
		s.mu.Unlock()
		s.auth.revoke()
		if s.options.Preview != nil {
			s.options.Preview.close()
		}
		drain, done := context.WithTimeout(context.WithoutCancel(run), 2*time.Second)
		defer done()
		err := server.Shutdown(drain)
		if err != nil {
			err = server.Close()
		}
		s.handlers.Wait()
		stopped <- err
	}()
	err := server.Serve(s.listener)
	cancel()
	drainErr := <-stopped
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		err = nil
	}
	return errors.Join(err, drainErr)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	s.handlers.Add(1)
	s.mu.Unlock()
	defer s.handlers.Done()
	headers(w.Header())
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || peer != "127.0.0.1" || r.Host != s.host || r.URL.IsAbs() ||
		r.URL.RawPath != "" || r.URL.Opaque != "" {
		s.reject(w, http.StatusForbidden, "ACCESS_DENIED")
		return
	}
	if !apiRoutePath(r.URL.Path) {
		if r.URL.Path == "/" && r.Method == http.MethodGet && s.options.PreviewOrigins != nil {
			s.previewFrameHeader(r.Context(), w.Header())
		}
		s.serveAsset(w, r)
		return
	}
	s.serveAPI(w, r)
}

func (s *Server) previewFrameHeader(ctx context.Context, response http.Header) {
	ctx, cancel := context.WithTimeout(ctx, min(s.options.QueryTimeout, time.Second))
	defer cancel()
	origins, err := s.options.PreviewOrigins(ctx, s.now())
	if err != nil || len(origins) > 32 {
		return
	}
	checked := make([]string, 0, len(origins))
	for _, origin := range origins {
		if origin != s.origin && model.CheckLocalPreviewLocation(origin, "/") == nil {
			checked = append(checked, origin)
		}
	}
	value := strings.Join(checked, " ")
	if value != "" && len(value) <= 4096 {
		response.Set("Content-Security-Policy", response.Get("Content-Security-Policy")+"; frame-src "+value)
	}
}

func headers(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}

type asset struct {
	mime string
	body []byte
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		s.reject(w, http.StatusNotFound, "UNSUPPORTED_CAPABILITY")
		return
	}
	a, ok := s.assets[r.URL.Path]
	if !ok {
		s.reject(w, http.StatusNotFound, "UNSUPPORTED_CAPABILITY")
		return
	}
	w.Header().Set("Content-Type", a.mime)
	_, _ = w.Write(a.body)
}

func loadAssets(files fs.FS) (map[string]asset, error) {
	if files == nil {
		return nil, ErrAssetsUnavailable
	}
	assets := make(map[string]asset)
	total := int64(0)
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != "." && name != "assets" {
				return fs.SkipDir
			}
			return nil
		}
		if len(assets) == 128 || !entry.Type().IsRegular() {
			return ErrAssetsUnavailable
		}
		route, mime := assetRoute(name)
		if route == "" {
			return nil
		}
		f, err := files.Open(name)
		if err != nil {
			return err
		}
		limit := min(4<<20, 16<<20-total)
		body, readErr := io.ReadAll(io.LimitReader(f, limit+1))
		if err := errors.Join(readErr, f.Close()); err != nil || int64(len(body)) > limit || len(body) == 0 {
			return ErrAssetsUnavailable
		}
		total += int64(len(body))
		assets[route] = asset{mime, body}
		return nil
	})
	if _, exists := assets["/"]; !exists || err != nil {
		return nil, ErrAssetsUnavailable
	}
	return assets, nil
}
