package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

var errInput = errors.New("CLI input unavailable")

// readBounded expects a reader whose Close interrupts a blocked Read. It
// leaves the reader open on success; cancellation closes and joins the read.
func readBounded(ctx context.Context, reader io.ReadCloser, limit int) ([]byte, error) {
	if ctx == nil || reader == nil || limit <= 0 {
		return nil, errInput
	}
	type outcome struct {
		data []byte
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
		done <- outcome{data: data, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = reader.Close()
		<-done
		return nil, ctx.Err()
	case value := <-done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if value.err != nil || len(value.data) == 0 || len(value.data) > limit {
			return nil, errInput
		}
		return value.data, nil
	}
}

// readInput opens an explicitly supplied regular file without following links,
// checks bounded stable bytes twice, and rechecks the anchored parent. A dash
// consumes stdin instead. It never executes a target or prints its input.
func readInput(ctx context.Context, name string, stdin io.ReadCloser, limit int) ([]byte, error) {
	if name == "-" {
		return readBounded(ctx, stdin, limit)
	}
	absolute, err := filepath.Abs(name)
	if err != nil || name == "" {
		return nil, errInput
	}
	root, err := paths.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, errInput
	}
	defer root.Close()
	file, err := root.OpenRegular(filepath.Base(absolute))
	if err != nil {
		return nil, errInput
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > int64(limit) {
		return nil, errInput
	}
	first, err := readBounded(ctx, file, limit)
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, errInput
	}
	second, err := readBounded(ctx, file, limit)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(first, second) || root.Verify() != nil {
		return nil, errInput
	}
	current, err := root.OpenRegular(filepath.Base(absolute))
	if err != nil {
		return nil, errInput
	}
	currentInfo, statErr := current.Stat()
	closeErr := current.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(before, currentInfo) || root.Verify() != nil {
		return nil, errInput
	}
	return first, nil
}
