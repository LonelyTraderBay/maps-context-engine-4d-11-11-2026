package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	fsruntime "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func TestRunValidatesJSONWithoutOpeningState(t *testing.T) {
	called := false
	deps := Dependencies{CPUs: 8, OpenConfig: func(context.Context, string) (ConfigOwner, func() error, error) {
		called = true
		return nil, nil, errors.New("must not open state")
	}}
	input := `{"action":"validate","config":{"schema_version":1,"core":{"max_parse_workers":0}}}`
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"config", "validate", "--input-json", "-"}, io.NopCloser(strings.NewReader(input)), &out, &errOut, deps)
	if code != 0 || called || errOut.Len() != 0 {
		t.Fatalf("validate: exit %d, owner opened %v", code, called)
	}
	response := decodeResult(t, Result{JSON: bytes.TrimSpace(out.Bytes()), ExitCode: code})
	if response.Data.(model.RuntimeConfigData).Config.Core.MaxParseWorkers != 4 {
		t.Fatal("CPU sentinel was not resolved")
	}
	for _, args := range [][]string{
		{"config", "validate", "--input-json"},
		{"config", "validate", "--input-json", "-", "--extra"},
		{"--runtime-config", "relative.json", "config", "show", "--input-json", "-"},
	} {
		out.Reset()
		errOut.Reset()
		if code := Run(context.Background(), args, io.NopCloser(strings.NewReader(input)), &out, &errOut, deps); code != 2 || out.Len() != 0 || called {
			t.Fatalf("invalid CLI arguments accepted: %v", args)
		}
	}
}

func TestRunVersionDoctorAndNoOutputLeak(t *testing.T) {
	for _, command := range []string{"version", "doctor"} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{command, "--json"}, io.NopCloser(strings.NewReader("")), &out, &errOut, Dependencies{CPUs: 4})
		var body map[string]any
		if code != 0 || errOut.Len() != 0 || json.Unmarshal(out.Bytes(), &body) != nil {
			t.Fatalf("%s output invalid", command)
		}
	}
	var out, errOut bytes.Buffer
	input := `{"action":"validate","config":{"schema_version":1,"unknown":"private-input"}}`
	code := Run(context.Background(), []string{"config", "validate", "--input-json", "-"}, io.NopCloser(strings.NewReader(input)), &out, &errOut, Dependencies{CPUs: 4})
	if code != 2 || strings.Contains(out.String()+errOut.String(), "private-input") {
		t.Fatal("invalid input was leaked or accepted")
	}
}

func TestRunHelpListsSupportedQueryActionsWithoutOpeningState(t *testing.T) {
	var out, errOut bytes.Buffer
	deps := Dependencies{CPUs: 4, OpenCore: func(context.Context, string, string, string) (*app.CoreService, func() error, error) {
		t.Fatal("help opened state")
		return nil, nil, nil
	}}
	code := Run(context.Background(), []string{"--help"}, io.NopCloser(strings.NewReader("")), &out, &errOut, deps)
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "query status|locate --input-json") || !strings.Contains(out.String(), "Query context|expand|validate|rename-preview are recognized but unavailable.") || !strings.Contains(out.String(), "require an authorized bound service") {
		t.Fatalf("help misreported query actions or requirements: exit %d", code)
	}
	out.Reset()
	errOut.Reset()
	deps.OpenRetainedCore = func(context.Context, string, string, string) (*app.CoreService, func() error, error) {
		t.Fatal("help opened a retained-result owner")
		return nil, nil, nil
	}
	code = Run(context.Background(), []string{"--help"}, io.NopCloser(strings.NewReader("")), &out, &errOut, deps)
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "query status|locate|context|expand|validate|rename-preview --input-json") || !strings.Contains(out.String(), "require their authorized retained-result owner") || !strings.Contains(out.String(), "never applies edits") {
		t.Fatalf("help misreported retained queries: exit %d", code)
	}
}

func TestDoctorReportsOnlyObservedCapabilities(t *testing.T) {
	var out, errOut bytes.Buffer
	deps := Dependencies{CPUs: 4,
		SQLiteProbe: func(context.Context) (string, bool, error) { return "3.51.0", true, nil },
		LookupExecutable: func(name string) (string, error) {
			if name == "git" {
				return "/private/path/git", nil
			}
			return "", errors.New("private lookup detail")
		},
		OpenConfig: func(context.Context, string) (ConfigOwner, func() error, error) {
			t.Fatal("doctor opened configuration")
			return nil, nil, nil
		},
	}
	code := Run(context.Background(), []string{"doctor", "--json"}, io.NopCloser(strings.NewReader("")), &out, &errOut, deps)
	var body struct {
		Checks map[string]string `json:"checks"`
	}
	if code != 0 || json.Unmarshal(out.Bytes(), &body) != nil || body.Checks["sqlite_fts"] != "available_in_memory" || body.Checks["git"] != "found_unverified" || body.Checks["filesystem"] != "not_checked" || strings.Contains(out.String()+errOut.String(), "private") {
		t.Fatal("doctor misreported or leaked a probe")
	}
}

func TestDoctorConfigurationProbeReportsSanitizedStatus(t *testing.T) {
	for _, tc := range []struct {
		name, expected string
		err            error
	}{
		{"available", "available_read_only", nil},
		{"pending", "recovery_required", fsruntime.ErrConfigPending},
		{"cancelled", "cancelled", context.Canceled},
		{"timeout", "timed_out", context.DeadlineExceeded},
		{"other_error", "unavailable_or_unverified", errors.New("private fixture detail")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			configPath := filepath.Join(t.TempDir(), "approved-fixture.json")
			deps := Dependencies{CPUs: 4, ProbeConfig: func(ctx context.Context, path string) error {
				if _, ok := ctx.Deadline(); !ok || path != configPath {
					t.Fatal("doctor lost the selected configuration or deadline")
				}
				return tc.err
			}}
			args := []string{"--runtime-config", configPath, "doctor", "--json"}
			code := Run(context.Background(), args, io.NopCloser(strings.NewReader("")), &out, &errOut, deps)
			var body struct {
				Checks map[string]string `json:"checks"`
			}
			if code != 0 || errOut.Len() != 0 || json.Unmarshal(out.Bytes(), &body) != nil || body.Checks["configuration"] != tc.expected || body.Checks["filesystem"] != "not_checked" {
				t.Fatalf("doctor config probe: %d, %s", code, out.String())
			}
			if strings.Contains(out.String(), "private") || strings.Contains(out.String(), filepath.Base(configPath)) {
				t.Fatal("doctor leaked private probe details")
			}
		})
	}
}

type signallingReader struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (r *signallingReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.ReadCloser.Read(p)
}

func TestRunCancellationClosesAndJoinsInput(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	signalling := &signallingReader{ReadCloser: reader, started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"config", "validate", "--input-json", "-"}, signalling, &out, &errOut, Dependencies{CPUs: 4})
	}()
	select {
	case <-signalling.started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("input did not start")
	}
	cancel()
	select {
	case code := <-done:
		if code != 4 {
			t.Fatalf("cancel exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled input did not stop")
	}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

func TestRunRejectsShortOutput(t *testing.T) {
	var errOut bytes.Buffer
	if code := Run(context.Background(), []string{"version", "--json"}, io.NopCloser(strings.NewReader("")), shortWriter{}, &errOut, Dependencies{CPUs: 4}); code != 5 {
		t.Fatalf("short write exit %d", code)
	}
}
