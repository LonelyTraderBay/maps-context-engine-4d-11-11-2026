package runtime

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

var ErrGitIndexLimit = errors.New("git index size limit exceeded")
var ErrGitIndexLinked = errors.New("git index has multiple hard links")
var ErrGitIndexChanged = errors.New("git index changed while reading")

// ReadGitIndex reads only the index in an already authorized metadata root.
// It neither discovers gitdirs nor follows a .git file. The caller must bind
// that root to the registered worktree and revalidate authorization. The bytes
// are untrusted and must not be persisted or exposed without further checks.
// Stable capture and interpretation of index extensions belong to the
// inventory/capture owners; this function does not execute Git.
func ReadGitIndex(metadataRoot *paths.Root, maxBytes int) (data []byte, resultErr error) {
	return ReadGitIndexContext(context.Background(), metadataRoot, maxBytes)
}

// ReadGitIndexContext additionally aborts bounded reads after cancellation.
func ReadGitIndexContext(ctx context.Context, metadataRoot *paths.Root, maxBytes int) (data []byte, resultErr error) {
	if ctx == nil {
		return nil, ErrGitMetadataUnverified
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes <= 0 || maxBytes > 64*1024*1024 {
		return nil, ErrGitIndexLimit
	}
	file, err := metadataRoot.OpenRegular("index")
	if err != nil {
		if errors.Is(err, paths.ErrHardLinked) {
			return nil, ErrGitIndexLinked
		}
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			data = nil
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	return readGitIndex(metadataRoot, file, gitContextReader{ctx: ctx, reader: file}, maxBytes)
}

type gitContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r gitContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(data)
	if cancelErr := r.ctx.Err(); cancelErr != nil {
		return n, cancelErr
	}
	return n, err
}

// readGitIndex checks observed file metadata and the current index path before
// releasing bytes. This is not an atomic snapshot and does not replace capture
// stability or registered metadata authority.
func readGitIndex(metadataRoot *paths.Root, file *os.File, reader io.Reader, maxBytes int) ([]byte, error) {
	data, err := readObservedFile(metadataRoot, "index", file, reader, maxBytes, ErrGitIndexLimit, ErrGitIndexChanged)
	if errors.Is(err, paths.ErrHardLinked) {
		return nil, ErrGitIndexLinked
	}
	return data, err
}

func readObservedFile(root *paths.Root, path string, file *os.File, reader io.Reader, maxBytes int, limitErr, changedErr error) ([]byte, error) {
	if err := paths.VerifyRegularFile(file); err != nil {
		return nil, err
	}
	before, err := observeGitFile(file)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, limitErr
	}
	if err = paths.VerifyRegularFile(file); err != nil {
		return nil, err
	}
	after, err := observeGitFile(file)
	if err != nil {
		return nil, err
	}
	if !sameObservedFile(before, after) {
		return nil, changedErr
	}
	current, err := root.OpenRegular(path)
	if err != nil {
		return nil, errors.Join(changedErr, err)
	}
	currentInfo, statErr := observeGitFile(current)
	closeErr := current.Close()
	if statErr != nil || closeErr != nil {
		return nil, errors.Join(statErr, closeErr)
	}
	if !sameObservedFile(after, currentInfo) {
		return nil, changedErr
	}
	if err = root.Verify(); err != nil {
		return nil, err
	}
	return data, nil
}

type gitFileObservation struct {
	info    os.FileInfo
	changed [2]int64
}

func observeGitFile(file *os.File) (gitFileObservation, error) {
	info, err := file.Stat()
	if err != nil {
		return gitFileObservation{}, err
	}
	changed, err := gitFileChangeTime(file, info)
	return gitFileObservation{info: info, changed: changed}, err
}

func sameObservedFile(a, b gitFileObservation) bool {
	return os.SameFile(a.info, b.info) && a.info.Size() == b.info.Size() &&
		a.info.ModTime().Equal(b.info.ModTime()) && a.info.Mode() == b.info.Mode() && a.changed == b.changed
}
