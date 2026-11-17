//go:build linux

package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// Session is a single-use, preview-only LSP process. Callers must Close it if
// Preview is not invoked; the supplied startup context also bounds its life.
type Session struct {
	mu            sync.Mutex
	process       *managedProcess
	server        *os.File
	serverRoot    *paths.Root
	permit        *sessionPermit
	expected      string
	check         func(context.Context) error
	workspace     string
	docs          map[string]CapturedDocument
	settings      map[string]any
	encoding      snapshot.PositionEncoding
	nextID        int64
	initialized   bool
	used          bool
	closed        bool
	closeErr      error
	diagnostics   bool
	renameTimeout time.Duration
}

// StartSession runs only the allowlisted gopls build on the guarded captured
// workspace. No target repository, automatic installation or apply is involved.
func StartSession(ctx context.Context, config SessionConfig) (*Session, error) {
	if ctx == nil || config.ServerBinRoot == nil || config.Check == nil || config.Limiter == nil || !resultHash(config.ExpectedSHA256) {
		return nil, ErrLSPAuthority
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrLSPAuthority
	}
	startupDuration, err := sessionTimeout(config.StartupTimeoutMs)
	if err != nil {
		return nil, err
	}
	renameDuration, err := sessionTimeout(config.RenameTimeoutMs)
	if err != nil {
		return nil, err
	}
	startup, cancel := context.WithTimeout(ctx, startupDuration)
	defer cancel()
	docs, err := capturedDocuments(startup, config.Environment.WorkspaceRoot, config.Documents, int(resultHardBytes))
	if err != nil || len(docs) > 128 {
		if startup.Err() != nil {
			return nil, startup.Err()
		}
		return nil, ErrLSPAuthority
	}
	var total int64
	var first *model.SourceRef
	for _, doc := range docs {
		if !strings.HasSuffix(doc.File.Path(), ".go") || doc.Version == nil || doc.File.Len() > 64*1024 {
			return nil, ErrLSPAuthority
		}
		total += int64(doc.File.Len())
		if total > 2*1024*1024 {
			return nil, ErrLSPAuthority
		}
		if first == nil {
			identity := doc.Ref
			first = &identity
		} else if first.WorktreeID != doc.Ref.WorktreeID || first.ProfileID != doc.Ref.ProfileID || first.GenerationID != doc.Ref.GenerationID {
			return nil, ErrLSPAuthority
		}
	}
	env, flags, err := runtime.OfflineGoEnvironment(config.Environment)
	if err != nil || config.Check(startup) != nil {
		if startup.Err() != nil {
			return nil, startup.Err()
		}
		return nil, ErrLSPAuthority
	}
	permit, err := config.Limiter.acquire(startup)
	if err != nil {
		return nil, err
	}
	keep := false
	var process *managedProcess
	var file *os.File
	defer func() {
		if !keep {
			stopped := true
			if process != nil {
				stopped = process.close() == nil
			}
			if file != nil {
				_ = file.Close()
			}
			if stopped {
				permit.release()
			}
		}
	}()
	file, err = config.ServerBinRoot.OpenRegular("gopls")
	if err != nil {
		return nil, ErrLSPAuthority
	}
	if verifyServerBinding(startup, config.ServerBinRoot, file, config.ExpectedSHA256) != nil || config.Check(startup) != nil {
		if startup.Err() != nil {
			return nil, startup.Err()
		}
		return nil, ErrLSPAuthority
	}
	process, err = startManaged(file, nil, config.Environment.WorkspaceRoot, env)
	if err != nil {
		return nil, err
	}
	s := &Session{process: process, server: file, serverRoot: config.ServerBinRoot, permit: permit, expected: config.ExpectedSHA256, check: config.Check, workspace: config.Environment.WorkspaceRoot, docs: docs, settings: map[string]any{"vulncheck": "Off", "fileWatcher": "off", "buildFlags": flags}, encoding: snapshot.UTF16, nextID: 1, renameTimeout: renameDuration}
	if err := s.initialize(startup); err != nil {
		return nil, err
	}
	if err := s.verify(startup); err != nil {
		return nil, err
	}
	keep = true
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close(context.Background())
		case <-process.done:
			_ = s.Close(context.Background())
		case <-process.stop:
		}
	}()
	return s, nil
}

func (s *Session) verify(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.check(ctx) != nil || verifyServerBinding(ctx, s.serverRoot, s.server, s.expected) != nil || s.check(ctx) != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrLSPAuthority
	}
	return ctx.Err()
}

func (s *Session) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextID
	s.nextID++
	return protocolRequest(ctx, s.process.incoming, s.process.send, id, method, params, s.settings, s.observe)
}

func (s *Session) observe(method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(params, &object) != nil || object == nil {
		s.diagnostics = true
		return
	}
	var diagnostics []json.RawMessage
	if json.Unmarshal(object["diagnostics"], &diagnostics) != nil || diagnostics == nil || len(diagnostics) > 4096 {
		s.diagnostics = true
		return
	}
	for _, raw := range diagnostics {
		var diagnostic map[string]json.RawMessage
		var severity int
		if json.Unmarshal(raw, &diagnostic) != nil || json.Unmarshal(diagnostic["severity"], &severity) != nil || severity < 1 || severity > 4 || severity == 1 {
			s.diagnostics = true
			return
		}
	}
}

func (s *Session) initialize(ctx context.Context) error {
	rootURI := (&url.URL{Scheme: "file", Path: filepath.ToSlash(s.workspace)}).String()
	params := map[string]any{
		"processId": os.Getpid(), "rootUri": rootURI,
		"workspaceFolders": []any{map[string]any{"uri": rootURI, "name": "captured"}},
		"capabilities": map[string]any{
			"general":      map[string]any{"positionEncodings": []string{"utf-16", "utf-8", "utf-32"}},
			"workspace":    map[string]any{"configuration": true, "workspaceEdit": map[string]any{"documentChanges": true}},
			"textDocument": map[string]any{"rename": map[string]any{"prepareSupport": true}},
		},
		"initializationOptions": s.settings,
	}
	raw, err := s.request(ctx, "initialize", params)
	if err != nil {
		return err
	}
	var response struct {
		Capabilities struct {
			PositionEncoding string          `json:"positionEncoding"`
			RenameProvider   json.RawMessage `json:"renameProvider"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return ErrLSPProtocol
	}
	var provider struct {
		PrepareProvider bool `json:"prepareProvider"`
	}
	if json.Unmarshal(response.Capabilities.RenameProvider, &provider) != nil || !provider.PrepareProvider {
		return ErrLSPProtocol
	}
	if response.Capabilities.PositionEncoding != "" {
		s.encoding = snapshot.PositionEncoding(response.Capabilities.PositionEncoding)
	}
	if s.encoding != snapshot.UTF8 && s.encoding != snapshot.UTF16 && s.encoding != snapshot.UTF32 {
		return ErrLSPProtocol
	}
	if err := s.process.send(ctx, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}); err != nil {
		return err
	}
	keys := make([]string, 0, len(s.docs))
	for uri := range s.docs {
		keys = append(keys, uri)
	}
	sort.Strings(keys)
	for _, uri := range keys {
		doc := s.docs[uri]
		params := map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": *doc.Version, "text": string(doc.File.Bytes())}}
		if err := s.process.send(ctx, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": params}); err != nil {
			return err
		}
	}
	s.initialized = true
	return nil
}

// Preview computes exactly one prepareRename/rename pair, validates the entire
// returned edit set against captured bytes, then tears down the session. The
// caller must spool the complete result before issuing any page/token.
func (s *Session) Preview(ctx context.Context, selector model.PositionSelector, newName string) (result ValidatedWorkspaceEdit, resultErr error) {
	if s == nil || ctx == nil {
		return result, ErrLSPAuthority
	}
	if _, ok := ctx.Deadline(); !ok {
		return result, ErrLSPAuthority
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.used || !s.initialized {
		return result, ErrLSPProtocol
	}
	s.used = true
	defer func() {
		resultErr = errors.Join(resultErr, s.closeLocked(ctx))
		if resultErr == nil {
			if ctx.Err() != nil {
				resultErr = ctx.Err()
			} else if s.diagnostics || s.check(ctx) != nil {
				if ctx.Err() != nil {
					resultErr = ctx.Err()
				} else {
					resultErr = ErrLSPAuthority
				}
			}
		}
		if resultErr != nil {
			result = ValidatedWorkspaceEdit{}
		}
	}()
	deadline, cancel := context.WithTimeout(ctx, s.renameTimeout)
	defer cancel()
	if selector.Kind != "source_position" || !token.IsIdentifier(newName) || utf8.RuneCountInString(newName) > 256 || selector.ByteOffset < 0 {
		return result, ErrWorkspaceEdit
	}
	var uri string
	var doc CapturedDocument
	for candidate, item := range s.docs {
		if item.Ref.Path == selector.Path && item.Ref.ContentHash == selector.ContentHash {
			uri, doc = candidate, item
			break
		}
	}
	if uri == "" || selector.ByteOffset > int64(doc.File.Len()) {
		return result, ErrWorkspaceEdit
	}
	position, err := doc.File.PositionAt(int(selector.ByteOffset), s.encoding)
	if err != nil {
		return result, ErrWorkspaceEdit
	}
	if err := s.verify(deadline); err != nil {
		return result, err
	}
	params := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]int{"line": position.Line, "character": position.Character}}
	prepared, err := s.request(deadline, "textDocument/prepareRename", params)
	if err != nil {
		return result, err
	}
	if !validPreparedRange(prepared, doc, s.encoding, selector.ByteOffset) || s.diagnostics {
		return result, ErrLSPProtocol
	}
	params["newName"] = newName
	raw, err := s.request(deadline, "textDocument/rename", params)
	if err != nil || s.diagnostics {
		return result, errors.Join(ErrLSPRemote, err)
	}
	docs := make([]CapturedDocument, 0, len(s.docs))
	for _, doc := range s.docs {
		docs = append(docs, doc)
	}
	result, err = ValidateWorkspaceEdit(deadline, raw, WorkspaceEditInput{WorkspaceRoot: s.workspace, Documents: docs, Encoding: s.encoding, MaxBytes: int(resultHardBytes), MaxEdits: 10000})
	if err != nil {
		return ValidatedWorkspaceEdit{}, err
	}
	if err := s.verify(deadline); err != nil {
		return ValidatedWorkspaceEdit{}, err
	}
	return result, nil
}

func validPreparedRange(raw json.RawMessage, doc CapturedDocument, encoding snapshot.PositionEncoding, offset int64) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	rangeRaw := raw
	if value, ok := fields["range"]; ok {
		if len(fields) > 2 {
			return false
		}
		if _, ok := fields["placeholder"]; len(fields) == 2 && !ok {
			return false
		}
		rangeRaw = value
	}
	wrapped := append([]byte(`{"range":`), rangeRaw...)
	wrapped = append(wrapped, []byte(`,"newText":""}`)...)
	edit, err := decodeTextEdit(wrapped, doc, encoding, int(resultHardBytes))
	return err == nil && edit.SourceRef.Range.StartByte <= offset && offset < edit.SourceRef.Range.EndByte
}

// Close tears down this session and its process group; it never deletes files.
func (s *Session) Close(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrLSPProcess
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked(ctx)
}

func (s *Session) closeLocked(parent context.Context) error {
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	var shutdownErr error
	select {
	case <-s.process.done:
	default:
		if s.initialized && parent.Err() == nil {
			ctx, cancel := context.WithTimeout(parent, 3*time.Second)
			_, shutdownErr = s.request(ctx, "shutdown", nil)
			if shutdownErr == nil {
				shutdownErr = s.process.send(ctx, map[string]any{"jsonrpc": "2.0", "method": "exit", "params": nil})
			}
			if shutdownErr == nil {
				select {
				case <-s.process.done:
				case <-ctx.Done():
					shutdownErr = ctx.Err()
				}
			}
			cancel()
		}
	}
	stopErr := s.process.close()
	if shutdownErr == nil && stopErr == nil && s.process.waitErr != nil {
		shutdownErr = ErrLSPProcess
	}
	verifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	verifyErr := verifyServerBinding(verifyCtx, s.serverRoot, s.server, s.expected)
	cancel()
	if stopErr == nil {
		s.permit.release()
	}
	s.closeErr = errors.Join(shutdownErr, stopErr, verifyErr, s.server.Close())
	return s.closeErr
}
