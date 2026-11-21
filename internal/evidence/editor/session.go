// Package editor captures explicit editor updates for a local owner. Each
// session holds one document in memory, separate from the disk index.
package editor

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const (
	MaxBufferBytes = 1 << 20
	MaxSessionTTL  = 15 * time.Minute
)

var (
	ErrDenied      = errors.New("editor document approval unavailable")
	ErrSession     = errors.New("editor document session unavailable")
	ErrVersion     = errors.New("editor buffer version unavailable")
	ErrDiskChanged = errors.New("editor base disk state changed")
	ErrSavePending = errors.New("editor save not observed on disk")
)

// Document pins one owner-approved document. An empty BaseDiskHash means the
// owner checked the path and established that the file was absent.
type Document struct {
	WorktreeID      string
	ProfileID       string
	PrincipalDigest string
	PolicyDigest    string
	Path            string
	BaseDiskHash    string
}

// InspectDisk rechecks the supplied principal, worktree, profile, path and
// policy, then observes the current disk hash. Uncertain state returns an
// error. An empty result with nil error means verified absence.
type InspectDisk func(context.Context, Document) (string, error)

// Reference identifies a captured buffer version. It cannot authorize disk
// reads or be used as a source reference in an indexed graph.
type Reference struct {
	SessionID   string
	Version     uint64
	ContentHash string
}

type Selection struct {
	Start snapshot.Position
	End   snapshot.Position
}

type View struct {
	Document  Document
	Reference Reference
	Range     model.ByteRange
	Bytes     []byte
	Freshness model.Freshness
}

type Session struct {
	mu       sync.Mutex
	document Document
	inspect  InspectDisk
	id       string
	deadline time.Time
	timer    *time.Timer
	closed   bool
	version  uint64
	file     snapshot.File
}

var scanner, scannerErr = policy.NewScanner(MaxBufferBytes)

// Open starts a bounded session after the owner verifies the document's base.
// Hosts open separate sessions for distinct documents or editor connections.
func Open(ctx context.Context, doc Document, inspect InspectDisk, ttl time.Duration) (*Session, error) {
	if inspect == nil || !model.ValidUUID(doc.WorktreeID) || !model.ValidDigest(doc.ProfileID) ||
		!model.ValidDigest(doc.PrincipalDigest) || !model.ValidDigest(doc.PolicyDigest) ||
		!validDiskHash(doc.BaseDiskHash) || policy.CheckPath(doc.Path) != nil {
		return nil, ErrDenied
	}
	if ttl <= 0 || ttl > MaxSessionTTL {
		return nil, ErrSession
	}
	if scannerErr != nil {
		return nil, ErrDenied
	}
	s := &Session{document: doc, inspect: inspect, id: rand.Text()}
	if err := s.checkBase(ctx); err != nil {
		return nil, err
	}
	s.deadline = time.Now().Add(ttl)
	s.mu.Lock()
	s.timer = time.AfterFunc(ttl, s.Close)
	s.mu.Unlock()
	return s, nil
}

// Capture admits a full version from the bound editor connection. Each newer
// update retires the old reference, including updates whose content is denied.
// The caller owns the input until Capture returns.
func (s *Session) Capture(ctx context.Context, version uint64, source []byte) (Reference, error) {
	if err := s.beginVersion(version); err != nil {
		return Reference{}, err
	}
	if err := s.checkBase(ctx); err != nil {
		return Reference{}, err
	}
	file, err := snapshot.NewFile(s.document.Path, source, MaxBufferBytes)
	if err != nil {
		return Reference{}, err
	}
	if err := scanner.CheckContent(file.Bytes()); err != nil {
		return Reference{}, err
	}
	if err := s.checkBase(ctx); err != nil {
		return Reference{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.liveLocked() {
		return Reference{}, ErrSession
	}
	if s.version != version {
		return Reference{}, ErrVersion
	}
	s.file = file
	return Reference{SessionID: s.id, Version: version, ContentHash: file.ContentHash()}, nil
}

// Read returns a copy from the exact current reference. Selections use strict
// zero-based UTF-16 positions in this buffer, including its original newlines.
func (s *Session) Read(ctx context.Context, ref Reference, selection *Selection) (View, error) {
	file, err := s.lookup(ref)
	if err != nil {
		return View{}, err
	}
	if err := s.checkBase(ctx); err != nil {
		return View{}, err
	}
	start, end := 0, file.Len()
	if selection != nil {
		start, err = file.OffsetAt(selection.Start, snapshot.UTF16)
		if err != nil {
			return View{}, err
		}
		end, err = file.OffsetAt(selection.End, snapshot.UTF16)
		if err != nil {
			return View{}, err
		}
	}
	body, err := file.Range(start, end)
	if err != nil {
		return View{}, err
	}
	if err := s.checkBase(ctx); err != nil {
		return View{}, err
	}
	if _, err := s.lookup(ref); err != nil {
		return View{}, err
	}
	return View{
		Document: s.document, Reference: ref, Bytes: body,
		Range: model.ByteRange{StartByte: int64(start), EndByte: int64(end)},
		Freshness: model.Freshness{Status: "not_checked", Scope: "none", BufferScope: "editor_session",
			Consistency: "captured_buffer_version", SourceView: "editor_snapshot"},
	}, nil
}

// ObserveSave acknowledges an external editor save only when the owner sees
// the saved bytes. It never writes a file or promotes a disk generation.
func (s *Session) ObserveSave(ctx context.Context, ref Reference) error {
	if _, err := s.lookup(ref); err != nil {
		return err
	}
	disk, err := s.observe(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.lookupLocked(ref); err != nil {
		return err
	}
	switch disk {
	case ref.ContentHash:
		s.closeLocked()
		return nil
	case s.document.BaseDiskHash:
		return ErrSavePending
	default:
		s.closeLocked()
		return ErrDiskChanged
	}
}

// Close invalidates the session after discard, reload, delete, disconnect or
// expiry. A host restart or reopen requires a new session and new references.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

func (s *Session) closeLocked() {
	s.closed = true
	s.file = snapshot.File{}
	if s.timer != nil {
		s.timer.Stop()
	}
}

func (s *Session) liveLocked() bool {
	if s.closed || !time.Now().Before(s.deadline) {
		s.closeLocked()
		return false
	}
	return true
}

func (s *Session) beginVersion(version uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.liveLocked() {
		return ErrSession
	}
	if version <= s.version {
		return ErrVersion
	}
	s.version = version
	s.file = snapshot.File{}
	return nil
}

func (s *Session) lookup(ref Reference) (snapshot.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupLocked(ref)
}

func (s *Session) lookupLocked(ref Reference) (snapshot.File, error) {
	if !s.liveLocked() || ref.SessionID != s.id {
		return snapshot.File{}, ErrSession
	}
	if ref.Version != s.version || s.file.ContentHash() == "" || ref.ContentHash != s.file.ContentHash() {
		return snapshot.File{}, ErrVersion
	}
	return s.file, nil
}

func (s *Session) checkBase(ctx context.Context) error {
	disk, err := s.observe(ctx)
	if err != nil {
		return err
	}
	if disk != s.document.BaseDiskHash {
		s.Close()
		return ErrDiskChanged
	}
	return nil
}

func (s *Session) observe(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	disk, err := s.inspect(ctx, s.document)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil || !validDiskHash(disk) {
		s.Close()
		return "", ErrDenied
	}
	return disk, nil
}

func validDiskHash(value string) bool { return value == "" || model.ValidDigest(value) }
