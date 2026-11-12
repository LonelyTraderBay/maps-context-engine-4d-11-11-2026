// Package snapshot owns immutable captured inputs. Construction does not grant
// filesystem access or establish that content was authorized for capture.
package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var (
	ErrInvalidSource = errors.New("invalid captured source")
	ErrInvalidRange  = errors.New("invalid captured byte range")
	ErrSourceLimit   = errors.New("captured source limit exceeded")
)

// File owns an immutable copy of the original UTF-8 source bytes. Hashes and
// ranges refer to these bytes, including their original newline encoding.
type File struct {
	path   string
	hash   string
	source []byte
}

// NewFile validates and copies bytes already obtained by an authorized
// capture adapter. It is not a path open, policy check, or freshness proof.
func NewFile(path string, source []byte, maxBytes int) (File, error) {
	if maxBytes < 0 || len(source) > maxBytes {
		return File{}, ErrSourceLimit
	}
	if model.ValidateRelativePath(path, false) != nil || !utf8.Valid(source) {
		return File{}, ErrInvalidSource
	}
	data := bytes.Clone(source)
	sum := sha256.Sum256(data)
	return File{path: path, hash: hex.EncodeToString(sum[:]), source: data}, nil
}

func (f File) Path() string        { return f.path }
func (f File) ContentHash() string { return f.hash }
func (f File) Len() int            { return len(f.source) }

// Bytes returns a copy so callers cannot change the captured input.
func (f File) Bytes() []byte { return bytes.Clone(f.source) }

// Range returns an exact half-open byte slice and rejects offsets in the
// middle of a UTF-8 code point. It does not normalize newlines or Unicode.
func (f File) Range(start, end int) ([]byte, error) {
	if !f.validRange(start, end) {
		return nil, ErrInvalidRange
	}
	return bytes.Clone(f.source[start:end]), nil
}

// Lines returns one-based display lines for a valid range. The end line of a
// nonempty range is the line containing its last byte; empty ranges use start.
func (f File) Lines(start, end int) (int, int, error) {
	if !f.validRange(start, end) {
		return 0, 0, ErrInvalidRange
	}
	startLine := f.lineAt(start)
	last := end
	if end > start {
		last--
	}
	endLine := f.lineAt(last)
	return startLine, endLine, nil
}

func (f File) lineAt(offset int) int {
	line := 1
	for i := 0; i < offset; i++ {
		switch f.source[i] {
		case '\r':
			if i+1 < len(f.source) && f.source[i+1] == '\n' {
				if i+1 >= offset {
					return line
				}
				i++
			}
			line++
		case '\n':
			line++
		}
	}
	return line
}

func (f File) boundary(offset int) bool {
	return offset == len(f.source) || f.source[offset]&0xc0 != 0x80
}

func (f File) validRange(start, end int) bool {
	return f.path != "" && start >= 0 && end >= start && end <= len(f.source) && f.boundary(start) && f.boundary(end)
}
