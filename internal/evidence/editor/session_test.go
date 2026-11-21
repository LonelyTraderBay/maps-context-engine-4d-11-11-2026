package editor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func TestC074SeparateEditorBuffersAndUTF16(t *testing.T) {
	doc, inspect, disk := documentFixture(t, "original\r\n")
	left := openFixture(t, doc, inspect)
	right := openFixture(t, doc, inspect)
	ctx := t.Context()
	source := []byte("A😀é\r\nB")
	first := captureFixture(t, left, 1, source)
	second := captureFixture(t, right, 1, []byte("other editor"))
	source[0] = 'Z'
	if first.SessionID == second.SessionID || first.ContentHash != digest([]byte("A😀é\r\nB")) {
		t.Fatal("session identity or captured hash differs")
	}
	if _, err := left.Read(ctx, second, nil); !errors.Is(err, ErrSession) {
		t.Fatalf("foreign session reference: %v", err)
	}
	wrong := first
	wrong.ContentHash = second.ContentHash
	if _, err := left.Read(ctx, wrong, nil); !errors.Is(err, ErrVersion) {
		t.Fatalf("wrong content hash: %v", err)
	}
	selection := Selection{Start: snapshot.Position{Character: 1}, End: snapshot.Position{Line: 1}}
	view, err := left.Read(ctx, first, &selection)
	if err != nil || string(view.Bytes) != "😀é\r\n" || view.Range != (model.ByteRange{StartByte: 1, EndByte: 9}) ||
		view.Reference != first || view.Document != doc || view.Freshness.SourceView != "editor_snapshot" ||
		view.Freshness.BufferScope != "editor_session" || view.Freshness.Consistency != "captured_buffer_version" ||
		view.Freshness.Status != "not_checked" {
		t.Fatalf("buffer selection metadata: %+v, %v", view, err)
	}
	view.Bytes[0] = 'X'
	selection.Start.Character = 2
	if _, err := left.Read(ctx, first, &selection); !errors.Is(err, snapshot.ErrInvalidRange) {
		t.Fatalf("split surrogate pair: %v", err)
	}
	selection = Selection{Start: snapshot.Position{Line: 1, Character: 1}, End: snapshot.Position{Line: 0}}
	if _, err := left.Read(ctx, first, &selection); !errors.Is(err, snapshot.ErrInvalidRange) {
		t.Fatalf("reversed range: %v", err)
	}
	assertBytes(t, left, first, "A😀é\r\nB")
	assertBytes(t, right, second, "other editor")
	newer := captureFixture(t, left, 4, []byte("revision 4"))
	if _, err := left.Read(ctx, first, nil); !errors.Is(err, ErrVersion) {
		t.Fatalf("stale reference: %v", err)
	}
	if _, err := left.Capture(ctx, 3, []byte("late update")); !errors.Is(err, ErrVersion) {
		t.Fatalf("out-of-order version: %v", err)
	}
	assertBytes(t, left, newer, "revision 4")
	assertBytes(t, right, second, "other editor")
	if data, err := os.ReadFile(disk); err != nil || string(data) != "original\r\n" {
		t.Fatalf("original disk content changed: %v", err)
	}
}

func TestC074SaveDiscardReloadAndDelete(t *testing.T) {
	doc, inspect, disk := documentFixture(t, "base")
	ctx := t.Context()
	first := openFixture(t, doc, inspect)
	other := openFixture(t, doc, inspect)
	ref := captureFixture(t, first, 1, []byte("saved externally"))
	otherRef := captureFixture(t, other, 1, []byte("other unsaved"))
	if err := first.ObserveSave(ctx, ref); !errors.Is(err, ErrSavePending) {
		t.Fatalf("unexpected save confirmation: %v", err)
	}
	assertBytes(t, first, ref, "saved externally")
	if data, err := os.ReadFile(disk); err != nil || string(data) != "base" {
		t.Fatalf("save request mutated disk: %v", err)
	}
	if err := os.WriteFile(disk, []byte("saved externally"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := first.ObserveSave(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Read(ctx, ref, nil); !errors.Is(err, ErrSession) {
		t.Fatalf("saved session still open: %v", err)
	}
	if _, err := other.Read(ctx, otherRef, nil); !errors.Is(err, ErrDiskChanged) {
		t.Fatalf("divergent editor did not detect saved disk: %v", err)
	}
	if _, err := Open(ctx, doc, inspect, time.Minute); !errors.Is(err, ErrDiskChanged) {
		t.Fatalf("obsolete disk base accepted: %v", err)
	}
	doc.BaseDiskHash = ref.ContentHash
	current := openFixture(t, doc, inspect)
	discard := captureFixture(t, current, 1, []byte("discarded"))
	current.Close()
	if _, err := current.Read(ctx, discard, nil); !errors.Is(err, ErrSession) {
		t.Fatalf("discard did not invalidate: %v", err)
	}
	reloaded := openFixture(t, doc, inspect)
	newRef := captureFixture(t, reloaded, 1, []byte("after reload"))
	if _, err := reloaded.Read(ctx, discard, nil); !errors.Is(err, ErrSession) {
		t.Fatalf("reload reused prior session: %v", err)
	}
	if err := os.Remove(disk); err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.Read(ctx, newRef, nil); !errors.Is(err, ErrDiskChanged) {
		t.Fatalf("delete left obsolete buffer accessible: %v", err)
	}
	doc.BaseDiskHash = ""
	untitled := openFixture(t, doc, inspect)
	newFile := captureFixture(t, untitled, 1, []byte("unsaved file"))
	assertBytes(t, untitled, newFile, "unsaved file")
	if _, err := os.Stat(disk); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("buffer created a file on disk: %v", err)
	}
}

func TestC074DenialsRetireVersions(t *testing.T) {
	doc, inspect, _ := documentFixture(t, "base")
	ctx := t.Context()
	var revoked atomic.Bool
	guard := func(ctx context.Context, request Document) (string, error) {
		if revoked.Load() {
			return "", errors.New("synthetic owner denial")
		}
		return inspect(ctx, request)
	}
	deniedDoc := doc
	deniedDoc.Path = ".env"
	if _, err := Open(ctx, deniedDoc, guard, time.Minute); !errors.Is(err, ErrDenied) {
		t.Fatalf("denied document path: %v", err)
	}
	deniedDoc = doc
	deniedDoc.PolicyDigest = strings.Repeat("b", 64)
	if _, err := Open(ctx, deniedDoc, guard, time.Minute); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign policy allowed: %v", err)
	}
	s := openFixture(t, doc, guard)
	for _, row := range []struct {
		name string
		body []byte
		want error
	}{
		{"source policy", []byte("password = synthetic_value_for_fixture"), policy.ErrSecretBlocked},
		{"invalid Unicode", []byte{0xff}, snapshot.ErrInvalidSource},
		{"oversize", bytes.Repeat([]byte("a"), MaxBufferBytes+1), snapshot.ErrSourceLimit},
	} {
		t.Run(row.name, func(t *testing.T) {
			version := s.version + 1
			accepted := captureFixture(t, s, version, []byte("prior"))
			if ref, err := s.Capture(t.Context(), version+1, row.body); !errors.Is(err, row.want) || ref != (Reference{}) {
				t.Fatalf("invalid update retained: %v", err)
			}
			if _, err := s.Read(t.Context(), accepted, nil); !errors.Is(err, ErrVersion) {
				t.Fatalf("prior version survived denied update: %v", err)
			}
		})
	}
	ref := captureFixture(t, s, s.version+1, []byte("authorized"))
	revoked.Store(true)
	if view, err := s.Read(ctx, ref, nil); !errors.Is(err, ErrDenied) || view.Bytes != nil {
		t.Fatalf("revoked owner received bytes: %v", err)
	}
	revoked.Store(false)
	if _, err := s.Read(ctx, ref, nil); !errors.Is(err, ErrSession) {
		t.Fatalf("revoked reference resumed: %v", err)
	}
}

func TestC074UpdateDuringReadAndExpiry(t *testing.T) {
	doc, inspect, _ := documentFixture(t, "base")
	ctx := t.Context()
	entered, release := make(chan struct{}), make(chan struct{})
	var pause atomic.Bool
	guard := func(ctx context.Context, request Document) (string, error) {
		if pause.Swap(false) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return inspect(ctx, request)
	}
	s := openFixture(t, doc, guard)
	ref := captureFixture(t, s, 1, []byte("superseded"))
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pause.Store(true)
	outcome := make(chan error, 1)
	go func() {
		_, err := s.Read(readCtx, ref, nil)
		outcome <- err
	}()
	select {
	case <-entered:
	case <-readCtx.Done():
		t.Fatal(readCtx.Err())
	}
	newer := captureFixture(t, s, 2, []byte("replacement"))
	close(release)
	if err := <-outcome; !errors.Is(err, ErrVersion) {
		t.Fatalf("superseded in-flight read returned: %v", err)
	}
	assertBytes(t, s, newer, "replacement")
	s.mu.Lock()
	s.deadline = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, err := s.Read(ctx, newer, nil); !errors.Is(err, ErrSession) {
		t.Fatalf("expired session read succeeded: %v", err)
	}
	s.mu.Lock()
	retained := s.file.ContentHash()
	s.mu.Unlock()
	if retained != "" {
		t.Fatal("expired session retained its captured file")
	}
}

func documentFixture(t *testing.T, base string) (Document, InspectDisk, string) {
	t.Helper()
	path := "main.go"
	disk := filepath.Join(t.TempDir(), path)
	if err := os.WriteFile(disk, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	doc := Document{WorktreeID: "00000000-0000-4000-8000-000000000001", ProfileID: digest([]byte("profile")),
		PrincipalDigest: digest([]byte("owner")), PolicyDigest: digest([]byte("policy")), Path: path, BaseDiskHash: digest([]byte(base))}
	inspect := func(ctx context.Context, request Document) (string, error) {
		request.BaseDiskHash = doc.BaseDiskHash
		if err := ctx.Err(); err != nil || request != doc {
			return "", ErrDenied
		}
		data, err := os.ReadFile(disk)
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return digest(data), err
	}
	return doc, inspect, disk
}

func openFixture(t *testing.T, doc Document, inspect InspectDisk) *Session {
	t.Helper()
	s, err := Open(t.Context(), doc, inspect, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func captureFixture(t *testing.T, session *Session, version uint64, bytes []byte) Reference {
	t.Helper()
	ref, err := session.Capture(t.Context(), version, bytes)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func assertBytes(t *testing.T, session *Session, ref Reference, want string) {
	t.Helper()
	view, err := session.Read(t.Context(), ref, nil)
	if err != nil || string(view.Bytes) != want {
		t.Fatalf("expected captured content: %v", err)
	}
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
