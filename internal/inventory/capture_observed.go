//go:build linux || windows

package inventory

import (
	"context"
	"errors"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"io"
	"os"
)

func readObservedCapture(ctx context.Context, root *paths.Root, path string, maxBytes int) (data []byte, resultErr error) {
	file, err := root.OpenRegular(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			data = nil
			resultErr = errors.Join(resultErr, err)
		}
	}()
	return readOpenedCapture(ctx, root, path, file, file, maxBytes)
}

// readOpenedCapture is also exercised with a mutating reader in a fixture; in
// production the reader is the securely opened file itself.
func readOpenedCapture(ctx context.Context, root *paths.Root, path string, file *os.File, reader io.Reader, maxBytes int) ([]byte, error) {
	if err := verifyCaptureFile(file); err != nil {
		return nil, err
	}
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: reader}, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, ErrCaptureLimit
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !sameCapturedObservation(before, after) {
		return nil, ErrCaptureChanged
	}
	if err := verifyCaptureFile(file); err != nil {
		return nil, err
	}
	current, err := root.OpenRegular(path)
	if err != nil {
		if errors.Is(err, paths.ErrRootChanged) {
			return nil, err
		}
		return nil, errors.Join(ErrCaptureChanged, err)
	}
	currentInfo, statErr := current.Stat()
	closeErr := current.Close()
	if statErr != nil || closeErr != nil {
		return nil, errors.Join(statErr, closeErr)
	}
	if !sameCapturedObservation(after, currentInfo) {
		return nil, ErrCaptureChanged
	}
	if err := root.Verify(); err != nil {
		return nil, err
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if cancelErr := r.ctx.Err(); cancelErr != nil {
		return n, cancelErr
	}
	return n, err
}
