//go:build !linux

package packstore

import "context"

func (*Store) Create(context.Context, string, []byte, VerifyBytes) error { return ErrUnsupported }
func (*Store) Read(context.Context, string, VerifyBytes) ([]byte, error) { return nil, ErrUnsupported }
func (*Store) GCExpired(context.Context, GCWriter, string, ExpiryProof) (int, error) {
	return 0, ErrUnsupported
}
