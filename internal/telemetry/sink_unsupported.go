//go:build !linux

package telemetry

import "context"

func (*FileSink) appendEvent(context.Context, []byte) error { return ErrUnsupported }
