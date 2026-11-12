//go:build linux

package inventory

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
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

func verifyCaptureFile(file *os.File) error {
	err := paths.VerifyRegularFile(file)
	if !errors.Is(err, paths.ErrHardLinked) {
		return err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		return errors.Join(err, statErr)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink == 0 {
		return errors.Join(ErrCaptureChanged, err)
	}
	return err
}

func sameCapturedObservation(a, b os.FileInfo) bool {
	before, okBefore := a.Sys().(*syscall.Stat_t)
	after, okAfter := b.Sys().(*syscall.Stat_t)
	return okBefore && okAfter && os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime()) && before.Ctim == after.Ctim
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
