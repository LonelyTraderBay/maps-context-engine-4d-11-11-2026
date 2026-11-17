//go:build !linux

package runtime

import "context"

type GoToolchainProof struct{}

func CheckPrivateToolUmask(context.Context) error { return ErrGoToolchain }

func CheckAdministratorPath(string) error { return ErrGoToolchain }
func InspectGoToolchain(context.Context, string) (*GoToolchainProof, error) {
	return nil, ErrGoToolchain
}
func (*GoToolchainProof) Check(context.Context) error             { return ErrGoToolchain }
func (*GoToolchainProof) CheckPath(context.Context, string) error { return ErrGoToolchain }
func (*GoToolchainProof) Digest() string                          { return "" }
func (*GoToolchainProof) StandardPackages() []string              { return nil }
func (*GoToolchainProof) Close() error                            { return nil }
