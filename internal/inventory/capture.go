// Package inventory contains bounded source capture primitives. Git
// enumeration and membership verification are separate capabilities.
package inventory

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

var (
	ErrCaptureChanged = errors.New("source changed during capture")
	ErrCaptureLimit   = errors.New("source exceeds capture limit")
	ErrCaptureInput   = errors.New("invalid capture input")
)

// AuthorizeCapture checks the caller's current actor, registered root and
// effective policy for this path. It is called before and after reading.
type AuthorizeCapture func(context.Context, string) error

type captureReader func(context.Context, *paths.Root, string, int) ([]byte, error)

// CaptureFile reads only a path under an already registered, authorized root.
// It applies the built-in deny and content scanner before returning any bytes.
// The caller supplies current policy/actor checks and must later revalidate
// membership and freshness; this is not an atomic filesystem snapshot.
func CaptureFile(ctx context.Context, root *paths.Root, scanner *policy.Scanner, path string, maxBytes int, authorize AuthorizeCapture) (snapshot.File, error) {
	return captureFile(ctx, root, scanner, path, maxBytes, authorize, readObservedCapture)
}

func captureFile(ctx context.Context, root *paths.Root, scanner *policy.Scanner, path string, maxBytes int, authorize AuthorizeCapture, read captureReader) (snapshot.File, error) {
	if ctx == nil || root == nil || scanner == nil || authorize == nil || read == nil || maxBytes <= 0 || int64(maxBytes) == int64(^uint64(0)>>1) {
		return snapshot.File{}, ErrCaptureInput
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return snapshot.File{}, err
		}
		if err := authorize(ctx, path); err != nil {
			return snapshot.File{}, err
		}
		if err := policy.CheckPath(path); err != nil {
			return snapshot.File{}, err
		}
		data, err := read(ctx, root, path, maxBytes)
		if errors.Is(err, ErrCaptureChanged) {
			if attempt == 2 {
				return snapshot.File{}, err
			}
			continue
		}
		if err != nil {
			return snapshot.File{}, err
		}
		if err := scanner.CheckContent(data); err != nil {
			return snapshot.File{}, err
		}
		if err := authorize(ctx, path); err != nil {
			return snapshot.File{}, err
		}
		if err := ctx.Err(); err != nil {
			return snapshot.File{}, err
		}
		if err := root.Verify(); err != nil {
			return snapshot.File{}, err
		}
		return snapshot.NewFile(path, data, maxBytes)
	}
	return snapshot.File{}, ErrCaptureChanged
}
