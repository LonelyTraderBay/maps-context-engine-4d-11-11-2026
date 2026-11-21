package golang

import (
	"context"
	"errors"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func overlayFixture(t *testing.T, source string) (*CapturedOverlay, string) {
	t.Helper()
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": source})
	root := filepath.Join(t.TempDir(), "owned-fixture")
	request := CapturedOverlayInput{Snapshot: input.Snapshot, Profile: input.Profile, SyntheticRoot: root, PolicyEpoch: input.PolicyEpoch, Scanner: input.Scanner, AuthorizeScope: input.AuthorizeScope, AuthorizePath: input.AuthorizePath, MaxManifestBytes: input.MaxManifestBytes, MaxFileBytes: input.MaxFileBytes, MaxOverlayBytes: 1024 * 1024}
	overlay, err := NewCapturedOverlay(preflightContext(t), request)
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		if !errors.Is(err, ErrLoaderUnsupported) {
			t.Fatalf("unverified platform accepted: %v", err)
		}
		t.Skip("platform loader unsupported")
	}
	if err != nil {
		t.Fatal(err)
	}
	return overlay, filepath.Join(root, "p.go")
}

func TestCapturedOverlayRejectsSourceDisagreement(t *testing.T) {
	const source = "package fixture\nvar X = 1\n"
	overlay, filename := overlayFixture(t, source)
	files, err := overlay.Overlay()
	if err != nil || string(files[filename]) != source || len(files) != 2 {
		t.Fatalf("overlay mismatch: %v", err)
	}
	files[filename][0] = 'X'
	if _, err := overlay.ParseFile(token.NewFileSet(), filename, files[filename]); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("changed bytes accepted: %v", err)
	}
	again, err := overlay.Overlay()
	if err != nil || string(again[filename]) != source {
		t.Fatalf("capture changed through overlay: %v", err)
	}
	parsed, err := overlay.ParseFile(token.NewFileSet(), filename, []byte(source))
	if err != nil || parsed.Name.Name != "fixture" {
		t.Fatalf("captured source rejected: %v", err)
	}
	if _, err := overlay.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(filename), "..", "outside.go"), []byte(source)); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("outside source accepted: %v", err)
	}
	if _, err := overlay.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(filename), "go.mod"), []byte("module example.test/fixture\n")); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("non-Go input parsed: %v", err)
	}
}

func TestCapturedOverlaySanitizesParseErrorAndRevocation(t *testing.T) {
	overlay, filename := overlayFixture(t, "package fixture\nvar =\n")
	if parsed, err := overlay.ParseFile(token.NewFileSet(), filename, []byte("package fixture\nvar =\n")); !errors.Is(err, ErrCapturedParse) || parsed != nil || strings.Contains(err.Error(), filename) {
		t.Fatalf("parse error exposed source/path: %v", err)
	}
	overlay, filename = overlayFixture(t, "package fixture\n")
	calls := 0
	overlay.authorizePath = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return errors.New("private path detail")
		}
		return nil
	}
	if parsed, err := overlay.ParseFile(token.NewFileSet(), filename, []byte("package fixture\n")); !errors.Is(err, ErrLoaderAuthority) || parsed != nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("revocation leaked AST: %v", err)
	}
}

func TestCapturedOverlayPreservesCancellation(t *testing.T) {
	overlay, filename := overlayFixture(t, "package fixture\n")
	ctx, cancel := context.WithCancel(overlay.ctx)
	overlay.ctx = ctx
	overlay.authorizePath = func(ctx context.Context, _, _, _ string, _ int64) error { cancel(); return ctx.Err() }
	if parsed, err := overlay.ParseFile(token.NewFileSet(), filename, []byte("package fixture\n")); !errors.Is(err, context.Canceled) || parsed != nil {
		t.Fatalf("cancellation hidden: %v", err)
	}
}

func TestCapturedOverlayConcurrentParsing(t *testing.T) {
	const source = "package fixture\nfunc Value() int { return 1 }\n"
	overlay, filename := overlayFixture(t, source)
	var workers sync.WaitGroup
	errCh := make(chan error, 16)
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			file, err := overlay.ParseFile(token.NewFileSet(), filename, []byte(source))
			if err != nil {
				errCh <- err
				return
			}
			if file.Name.Name != "fixture" {
				errCh <- ErrInputMismatch
			}
		}()
	}
	workers.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestCapturedOverlayRejectsBudgetAndProfileMismatch(t *testing.T) {
	input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/fixture\n", "p.go": "package fixture\n"})
	request := CapturedOverlayInput{Snapshot: input.Snapshot, Profile: input.Profile, SyntheticRoot: t.TempDir(), PolicyEpoch: input.PolicyEpoch, Scanner: input.Scanner, AuthorizeScope: input.AuthorizeScope, AuthorizePath: input.AuthorizePath, MaxManifestBytes: input.MaxManifestBytes, MaxFileBytes: input.MaxFileBytes, MaxOverlayBytes: 1}
	ctx := preflightContext(t)
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		if _, err := NewCapturedOverlay(ctx, request); !errors.Is(err, ErrLoaderUnsupported) {
			t.Fatal(err)
		}
		return
	}
	if _, err := NewCapturedOverlay(ctx, request); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("overlay budget ignored: %v", err)
	}
	request.MaxOverlayBytes = 1024 * 1024
	request.Profile.Goos = "windows"
	if _, err := NewCapturedOverlay(ctx, request); !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("profile mismatch accepted: %v", err)
	}
}
