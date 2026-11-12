package inventory

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

var (
	ErrIndexCorrupt     = errors.New("Git index is malformed or has an invalid checksum")
	ErrIndexUnsupported = errors.New("Git index format or required extension is unsupported")
	ErrIndexBudget      = errors.New("Git index exceeds the configured budget")
)

// IndexEntry is untrusted metadata, not proof that a worktree file still
// exists or has these bytes. Path is raw Git pathname bytes; consumers must
// apply portable-path validation, policy, authorization, and secure capture.
type IndexEntry struct {
	Path            []byte
	ObjectID        []byte
	Mode            uint32
	Stage           uint8
	AssumeUnchanged bool
	SkipWorktree    bool
	IntentToAdd     bool
}

type GitIndex struct {
	Version           uint32
	Entries           []IndexEntry
	SparseDirectories bool
}

// ParseGitIndex parses a bounded, already-secured index snapshot. objectHash
// must be the verified repository format (sha1 or sha256), not a guess. It
// accepts Git index v2-v4 and reports sparse-directory entries explicitly;
// split index and unknown required extensions fail closed. It never opens a
// file, runs Git, checks .gitignore, or claims the worktree is fresh.
func ParseGitIndex(raw []byte, objectHash string, maxEntries, maxPathBytes, maxDecodedPathBytes int) (GitIndex, error) {
	if maxEntries < 1 || maxEntries > 100000 || maxPathBytes < 1 || maxPathBytes > 65536 || maxDecodedPathBytes < 1 || maxDecodedPathBytes > 64*1024*1024 || len(raw) > 64*1024*1024 {
		return GitIndex{}, ErrIndexBudget
	}
	var hashLen int
	switch objectHash {
	case "sha1":
		hashLen = sha1.Size
	case "sha256":
		hashLen = sha256.Size
	default:
		return GitIndex{}, ErrIndexUnsupported
	}
	if len(raw) < 12+hashLen || !bytes.Equal(raw[:4], []byte("DIRC")) {
		return GitIndex{}, ErrIndexCorrupt
	}
	body, checksum := raw[:len(raw)-hashLen], raw[len(raw)-hashLen:]
	if objectHash == "sha1" {
		sum := sha1.Sum(body)
		if !bytes.Equal(sum[:], checksum) {
			return GitIndex{}, ErrIndexCorrupt
		}
	} else {
		sum := sha256.Sum256(body)
		if !bytes.Equal(sum[:], checksum) {
			return GitIndex{}, ErrIndexCorrupt
		}
	}
	version := binary.BigEndian.Uint32(body[4:8])
	if version < 2 || version > 4 {
		return GitIndex{}, ErrIndexUnsupported
	}
	count := binary.BigEndian.Uint32(body[8:12])
	if count > uint32(maxEntries) {
		return GitIndex{}, ErrIndexBudget
	}
	result := GitIndex{Version: version, Entries: make([]IndexEntry, 0, int(count))}
	pos := 12
	var previous []byte
	var previousStage uint8
	hasSparseEntry := false
	decodedPathBytes := 0
	for i := uint32(0); i < count; i++ {
		start := pos
		fixed := 40 + hashLen + 2
		if fixed > len(body)-pos {
			return GitIndex{}, ErrIndexCorrupt
		}
		mode := binary.BigEndian.Uint32(body[pos+24 : pos+28])
		objectID := bytes.Clone(body[pos+40 : pos+40+hashLen])
		flags := binary.BigEndian.Uint16(body[pos+40+hashLen : pos+fixed])
		pos += fixed
		var ext uint16
		if flags&0x4000 != 0 {
			if version == 2 || len(body)-pos < 2 {
				return GitIndex{}, ErrIndexCorrupt
			}
			ext = binary.BigEndian.Uint16(body[pos : pos+2])
			pos += 2
			if ext & ^uint16(0x6000) != 0 {
				return GitIndex{}, ErrIndexUnsupported
			}
		}
		var path []byte
		if version == 4 {
			remove, next, err := indexPrefixLength(body, pos, len(previous))
			if err != nil {
				return GitIndex{}, err
			}
			pos = next
			end := bytes.IndexByte(body[pos:], 0)
			if end < 0 {
				return GitIndex{}, ErrIndexCorrupt
			}
			pathLen := len(previous) - remove + end
			if pathLen > maxPathBytes || pathLen > maxDecodedPathBytes-decodedPathBytes {
				return GitIndex{}, ErrIndexBudget
			}
			path = make([]byte, 0, len(previous)-remove+end)
			path = append(path, previous[:len(previous)-remove]...)
			path = append(path, body[pos:pos+end]...)
			pos += end + 1
		} else {
			end := bytes.IndexByte(body[pos:], 0)
			if end < 0 {
				return GitIndex{}, ErrIndexCorrupt
			}
			if end > maxPathBytes || end > maxDecodedPathBytes-decodedPathBytes {
				return GitIndex{}, ErrIndexBudget
			}
			path = bytes.Clone(body[pos : pos+end])
			pos += end + 1
			padded := start + ((pos - start + 7) &^ 7)
			if padded > len(body) {
				return GitIndex{}, ErrIndexCorrupt
			}
			for _, b := range body[pos:padded] {
				if b != 0 {
					return GitIndex{}, ErrIndexCorrupt
				}
			}
			pos = padded
		}
		decodedPathBytes += len(path)
		nameLen := int(flags & 0x0fff)
		if len(path) < 0x0fff && nameLen != len(path) || len(path) >= 0x0fff && nameLen != 0x0fff {
			return GitIndex{}, ErrIndexCorrupt
		}
		stage := uint8((flags >> 12) & 3)
		skip := ext&0x4000 != 0
		directory := mode == 0040000
		if !validIndexMode(mode) || directory && (!skip || stage != 0) || !validIndexPath(path, directory) {
			return GitIndex{}, ErrIndexCorrupt
		}
		if i > 0 {
			order := bytes.Compare(previous, path)
			if order > 0 || order == 0 && (stage <= previousStage || previousStage == 0) {
				return GitIndex{}, ErrIndexCorrupt
			}
		}
		result.Entries = append(result.Entries, IndexEntry{Path: path, ObjectID: objectID, Mode: mode, Stage: stage, AssumeUnchanged: flags&0x8000 != 0, SkipWorktree: skip, IntentToAdd: ext&0x2000 != 0})
		previous, previousStage = path, stage
		hasSparseEntry = hasSparseEntry || directory
	}
	for pos < len(body) {
		if len(body)-pos < 8 {
			return GitIndex{}, ErrIndexCorrupt
		}
		signature := string(body[pos : pos+4])
		size := binary.BigEndian.Uint32(body[pos+4 : pos+8])
		pos += 8
		if uint64(size) > uint64(len(body)-pos) {
			return GitIndex{}, ErrIndexCorrupt
		}
		if signature == "sdir" {
			if result.SparseDirectories || size != 0 {
				return GitIndex{}, ErrIndexCorrupt
			}
			result.SparseDirectories = true
		} else if signature[0] < 'A' || signature[0] > 'Z' {
			return GitIndex{}, ErrIndexUnsupported
		}
		pos += int(size)
	}
	if hasSparseEntry && !result.SparseDirectories {
		return GitIndex{}, ErrIndexCorrupt
	}
	return result, nil
}

func indexPrefixLength(body []byte, pos, max int) (int, int, error) {
	if pos >= len(body) {
		return 0, 0, ErrIndexCorrupt
	}
	value := 0
	for n := 0; n < 10; n++ {
		if pos >= len(body) {
			return 0, 0, ErrIndexCorrupt
		}
		b := body[pos]
		pos++
		if n != 0 {
			if value >= max/128+1 {
				return 0, 0, ErrIndexCorrupt
			}
			value = (value + 1) << 7
		}
		value += int(b & 0x7f)
		if value > max {
			return 0, 0, ErrIndexCorrupt
		}
		if b&0x80 == 0 {
			return value, pos, nil
		}
	}
	return 0, 0, ErrIndexCorrupt
}

func validIndexMode(mode uint32) bool {
	return mode == 0100644 || mode == 0100755 || mode == 0120000 || mode == 0160000 || mode == 0040000
}

func validIndexPath(path []byte, directory bool) bool {
	if len(path) == 0 || path[0] == '/' {
		return false
	}
	if directory {
		if path[len(path)-1] != '/' {
			return false
		}
		path = path[:len(path)-1]
	} else if path[len(path)-1] == '/' {
		return false
	}
	for _, part := range bytes.Split(path, []byte{'/'}) {
		if len(part) == 0 || bytes.Equal(part, []byte(".")) || bytes.Equal(part, []byte("..")) || bytes.EqualFold(part, []byte(".git")) {
			return false
		}
	}
	return true
}
