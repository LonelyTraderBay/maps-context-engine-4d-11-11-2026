package golang

import (
	"context"
	"errors"
	"go/ast"
	"go/token"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/inventory"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	runtimeadapter "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestCapturedLoaderStopsBeforeExecutionWhenProofOrImportsAreMissing(t *testing.T) {
	for _, tc := range []struct {
		name, source, reason string
		proof                bool
	}{
		{"closure", "package x\nvar X int\n", "CLOSURE_UNVERIFIED", false},
		{"import", "package x\nimport \"fmt\"\nvar X = fmt.Sprint(1)\n", "GO_IMPORTS_UNVERIFIED", true},
		{"missing-local", "package x\nimport \"example.test/x/missing\"\nvar _ = missing.X\n", "GO_LOCAL_DEPENDENCY_MISSING", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := loaderPreflightFixture(t, map[string]string{"go.mod": "module example.test/x\n", "x.go": tc.source})
			if tc.proof {
				input.VerifyClosure = func(context.Context, *snapshot.Snapshot, inventory.FileMap) (bool, error) { return true, nil }
				input.VerifyToolchain = func(context.Context, *model.GoProfile) (bool, error) { return true, nil }
			}
			result, err := LoadCapturedPackages(preflightContext(t), CapturedLoaderInput{Preflight: input, Tool: &runtimeadapter.GoExecutable{}, MaxOutputBytes: 4096,
				VerifyScratch: func(context.Context, string, string, *snapshot.Snapshot) (bool, error) {
					t.Fatal("scratch was checked before required guards")
					return false, nil
				}})
			if err != nil || result.Status != "partial" || result.BuildSelectionVerified || len(result.Packages) != 0 || !containsReason(result.Reasons, tc.reason) {
				t.Fatalf("unsafe loader input accepted: %+v %v", result, err)
			}
		})
	}
}

func TestBoundedLoaderParserLimitsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered, release := make(chan struct{}, 8), make(chan struct{})
	var active, peak atomic.Int64
	parse := boundedLoaderParser(ctx, 2, func(*token.FileSet, string, []byte) (*ast.File, error) {
		current := active.Add(1)
		for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return &ast.File{}, nil
	})
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); _, _ = parse(token.NewFileSet(), "fixture", nil) }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			close(release)
			workers.Wait()
			t.Fatal("parser did not start")
		}
	}
	tooMany := false
	select {
	case <-entered:
		tooMany = true
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	workers.Wait()
	if tooMany || peak.Load() > 2 {
		t.Fatalf("parser limit exceeded: peak=%d", peak.Load())
	}
	canceled, stop := context.WithCancel(context.Background())
	firstEntered, firstRelease := make(chan struct{}), make(chan struct{})
	bounded := boundedLoaderParser(canceled, 1, func(*token.FileSet, string, []byte) (*ast.File, error) {
		close(firstEntered)
		<-firstRelease
		return &ast.File{}, nil
	})
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := bounded(token.NewFileSet(), "fixture", nil); first <- err }()
	<-firstEntered
	go func() { _, err := bounded(token.NewFileSet(), "fixture", nil); second <- err }()
	stop()
	if err := <-second; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting parse ignored cancellation: %v", err)
	}
	close(firstRelease)
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("completed parse ignored cancellation: %v", err)
	}
	if _, err := boundedLoaderParser(ctx, 0, nil)(token.NewFileSet(), "fixture", nil); !errors.Is(err, ErrLoaderInput) {
		t.Fatalf("invalid parser limit accepted: %v", err)
	}
}
