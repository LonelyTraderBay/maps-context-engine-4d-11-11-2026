//go:build !linux && !windows

package runtime

import "context"

type GoExecutable struct{}

func OpenGoExecutable(context.Context, GoExecutableInput) (*GoExecutable, error) {
	return nil, ErrGoToolchain
}
func (*GoExecutable) CheckForLoad(context.Context, string) error { return ErrGoToolchain }
func (*GoExecutable) Close() error                               { return ErrGoToolchain }
