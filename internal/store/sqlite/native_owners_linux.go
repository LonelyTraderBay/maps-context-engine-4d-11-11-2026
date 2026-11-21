//go:build linux

package sqlite

import (
	"context"
	"errors"
)

// Only a trusted profile owner may construct these internal stores. It owns
// roots, receipt identity/key, the process-isolated capture and the DB lifecycle.
// No transport may supply these fields, raw pools, SQL or a proof boolean.
func openNativeIndexOwner(ctx context.Context, s *nativeWALStore) (*IndexOwner, error) {
	if s == nil || s.name != "index.sqlite" {
		return nil, ErrStateUnsafe
	}
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	readers, writer := s.pool(true, 4), s.pool(false, 1)
	owner, err := openIndexOwner(ctx, s.lease, readers, writer, s.scope, nativeUIMaps)
	if err != nil {
		return nil, errors.Join(err, readers.Close(), writer.Close())
	}
	return owner, nil
}

func openNativeRecordsOwner(ctx context.Context, s *nativeWALStore) (*RecordsOwner, error) {
	if s == nil || s.name != "records.sqlite" {
		return nil, ErrStateUnsafe
	}
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	// R1 reads identity/epochs only. Record mutation arrives with its own task
	// and may not inherit an implicit writable connection from this owner.
	readers := s.pool(true, 1)
	owner, err := OpenRecordsOwner(ctx, s.lease, readers, s.scope)
	if err != nil {
		return nil, errors.Join(err, readers.Close())
	}
	return owner, nil
}
