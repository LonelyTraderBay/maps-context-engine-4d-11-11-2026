package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
)

const windowsIndexPreimageName = "index-before.sqlite"

type windowsIndexImages struct {
	head, data       windowsMaintenanceFiles
	headDir, dataDir *os.File
	identity         walAuthorityIdentity
}

// This lock covers only staging of the immutable preimage, receipt appends
// and reader capture. It is separate from the long-lived worktree lifecycle.
func (s windowsIndexImages) acquire(ctx context.Context, mode ports.LeaseMode) (*runtime.Lifecycle, error) {
	return runtime.AcquireLifecycle(ctx, s.head.control, s.head.scope, mode, 5*time.Second)
}

func (s windowsIndexImages) authority(ctx context.Context) (windowsWALAuthority, error) {
	return readWindowsWALAuthority(ctx, s.head, s.headDir, s.data, s.dataDir, s.identity)
}

// A fully written terminal receipt may survive a crash between the two
// durable appends. Accept just that next commit or abort, anchored to the independent
// PREPARE and the exact closed SQLite image. The head is only advanced.
func (s windowsIndexImages) finishInterruptedIndexReceipt(ctx context.Context, incarnation string, verify func() error) (windowsWALAuthority, error) {
	key, present, err := s.head.readEntryLimit(ctx, s.headDir, windowsCatalogHeadKey, 32)
	defer clear(key)
	if err != nil || !present || len(key) != 32 {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	head, present, err := s.head.readEntryLimit(ctx, s.headDir, walHeadName, walHeadMaxBytes)
	if err != nil || !present || len(head) == 0 || len(head)%walHeadRecordSize != 0 {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	log, present, err := s.data.readEntryLimit(ctx, s.dataDir, walAuthorityName, walAuthorityMaxBytes)
	extent := len(head) / walHeadRecordSize * walAuthorityRecordSize
	if err != nil || !present || len(log) != extent+walAuthorityRecordSize {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	prefix, err := replayWindowsWALAuthority(ctx, s.identity, [32]byte(key), log[:extent], head)
	defer clear(prefix.key[:])
	if err != nil || prefix.pending == nil {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	record := log[extent:]
	event, err := decodeWALAuthorityRecord(s.identity, prefix.key, prefix.receiptChain, record)
	next := prefix.walReceiptState
	if err != nil || (event.kind != walAuthorityCommitQuiesced && event.kind != walAuthorityAbort) || next.apply(event, false) != nil {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	if _, err := s.preimage(ctx, *prefix.pending); err != nil {
		return windowsWALAuthority{}, err
	}
	image, err := s.data.closedWorktreeImage(ctx, s.dataDir, "index.sqlite", "index", incarnation)
	if err != nil || image != event.Image {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	if err := verify(); err != nil {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	position := walJournalPosition{Size: int64(len(log)), SHA256: sha256.Sum256(record)}
	entry := encodeWALHeadRecord(s.identity, prefix.key, prefix.headChain, position)
	if err := s.head.appendAuthorityFile(ctx, s.headDir, walHeadName, prefix.records, prefix.headChain, entry); err != nil {
		return windowsWALAuthority{}, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return s.authority(ctx)
}

func (s windowsIndexImages) preimage(ctx context.Context, pending walAuthorityReceipt) ([]byte, error) {
	raw, ok, err := s.head.readEntryLimit(ctx, s.headDir, windowsIndexPreimageName, windowsIndexImageMaxBytes+32)
	if err != nil || !ok || len(raw) < 32+100 || !bytes.Equal(raw[:32], pending.Operation[:]) {
		return nil, errors.Join(ErrStoreRecoveryRequired, err)
	}
	image := raw[32:]
	proof, err := digestCheckpointedMain(ctx, bytes.NewReader(image), int64(len(image)), binary.BigEndian.Uint32(image[28:32]), windowsIndexImageMaxBytes)
	if err != nil || proof != pending.Image {
		return nil, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return image, nil
}

func (s windowsIndexImages) publishPreimage(ctx context.Context, operation [32]byte, image []byte) error {
	descriptor, err := s.head.acl.descriptor()
	if err != nil {
		return err
	}
	return s.head.publish(ctx, s.headDir, descriptor, windowsIndexPreimageName, append(operation[:], image...), true, nil)
}

func (s windowsIndexImages) capture(ctx context.Context) (_ []byte, resultErr error) {
	gate, err := s.acquire(ctx, ports.SharedLease)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, gate.Close()) }()
	authority, err := s.authority(ctx)
	defer clear(authority.key[:])
	if err != nil {
		return nil, err
	}
	if authority.pending != nil {
		// The writer's BUILDING writes and WAL stay private. Only the last
		// acknowledged SQLite image, authenticated by PREPARE, is visible.
		return s.preimage(ctx, *authority.pending)
	}
	image, proof, err := s.data.captureWindowsCheckpoint(ctx, s.dataDir, "index.sqlite")
	if err != nil || proof != authority.head.Image {
		return nil, errors.Join(ErrStoreRecoveryRequired, err)
	}
	return image, nil
}

// Each borrowed read connection owns its immutable VFS image. A new
// transaction can capture a later generation while an earlier one finishes.
type windowsIndexReadConnector struct {
	driver *modsqlite.Driver
	images windowsIndexImages
}

func (c *windowsIndexReadConnector) Driver() driver.Driver { return c.driver }

func (c *windowsIndexReadConnector) Connect(ctx context.Context) (driver.Conn, error) {
	image, err := c.images.capture(ctx)
	if err != nil {
		return nil, err
	}
	uri, registry, err := windowsSQLiteImageURI(image)
	if err != nil {
		return nil, err
	}
	conn, err := (&snapshotConnector{driver: c.driver, dsn: uri}).Connect(ctx)
	if err != nil {
		return nil, errors.Join(err, registry.Close())
	}
	tx, hasTx := conn.(driver.ConnBeginTx)
	exec, hasExec := conn.(driver.ExecerContext)
	query, hasQuery := conn.(driver.QueryerContext)
	prepare, hasPrepare := conn.(driver.ConnPrepareContext)
	if !hasTx || !hasExec || !hasQuery || !hasPrepare {
		return nil, errors.Join(ErrStateUnsupported, conn.Close(), registry.Close())
	}
	return &windowsIndexReadConn{Conn: conn, ConnBeginTx: tx, ExecerContext: exec,
		QueryerContext: query, ConnPrepareContext: prepare, vfs: registry}, nil
}

type windowsIndexReadConn struct {
	driver.Conn
	driver.ConnBeginTx
	driver.ExecerContext
	driver.QueryerContext
	driver.ConnPrepareContext
	vfs io.Closer
}

func (c *windowsIndexReadConn) Close() error {
	return errors.Join(c.Conn.Close(), c.vfs.Close())
}
