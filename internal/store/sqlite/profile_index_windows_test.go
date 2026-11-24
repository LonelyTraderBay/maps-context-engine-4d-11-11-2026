package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
	modsqlite "modernc.org/sqlite"
)

func TestWindowsIndexRetriesOnlyProvenUnchangedPrepare(t *testing.T) {
	for _, scenario := range []string{"unchanged", "changed-image", "unmatched-receipt", "concurrent-read",
		"commit-head-interrupted", "abort-head-interrupted", "wrong-commit-image"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			control := windowsTestControl(t, "")
			const worktree = "44444444-4444-4444-8444-444444444444"
			marker, err := control.createStagedWorktree(ctx, worktree, ctx.Err)
			if err != nil {
				t.Fatal(err)
			}
			files, err := control.openWindowsPoolDirectory("worktrees/" + worktree)
			if err != nil {
				t.Fatal(err)
			}
			defer closeIndexRetryFixture(t, files.control.Close)
			files.scope = worktree
			withOwner := func(mode ports.LeaseMode, use func(windowsStoreMaintenance) error) error {
				_, err := withWindowsMaintenanceLease(ctx, files, mode, time.Second, func(lease *runtime.Lifecycle) (_ struct{}, resultErr error) {
					job, err := windowsMaintenanceID()
					if err != nil {
						return struct{}{}, err
					}
					writer, err := runtime.AcquireWriter(ctx, lease, job, time.Second)
					if err != nil {
						return struct{}{}, err
					}
					defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
					return struct{}{}, use(windowsStoreMaintenance{files: files, lease: lease, writer: writer, job: job})
				})
				return err
			}
			pendingOperation := [32]byte{1}
			var before walLogicalImage
			terminalCommit := scenario == "commit-head-interrupted" || scenario == "wrong-commit-image"
			resolutionKind := walAuthorityAbort
			if err := withOwner(ports.ExclusiveLease, func(o windowsStoreMaintenance) error {
				return withIndexRetryFixture(t, ctx, o, marker.IndexIncarnation,
					func(s windowsMaintenanceStore, head, data *os.File, a windowsWALAuthority) error {
						before = a.head.Image
						if scenario == "concurrent-read" {
							return nil
						}
						event := walAuthorityRecord{kind: walAuthorityPrepare, walAuthorityReceipt: walAuthorityReceipt{
							Sequence: 1, Operation: pendingOperation, Image: before,
						}}
						if scenario == "unmatched-receipt" {
							raw := encodeWALAuthorityRecord(a.identity, a.key, a.receiptChain, event)
							return s.data.appendAuthorityFile(ctx, data, walAuthorityName, a.records, a.receiptChain, raw[:])
						}
						images := windowsIndexImages{s.head, s.data, head, data, a.identity}
						if terminalCommit || scenario == "abort-head-interrupted" {
							image, actual, err := s.data.captureWindowsCheckpoint(ctx, data, s.main)
							if err != nil || actual != before {
								return errors.Join(ErrStateChanged, err)
							}
							if err := images.publishPreimage(ctx, pendingOperation, image); err != nil {
								return err
							}
						}
						if err := a.append(ctx, o.lease, o.files, s.head, head, s.data, data, event); err != nil {
							return err
						}
						if scenario == "abort-head-interrupted" {
							event.kind = walAuthorityAbort
							raw := encodeWALAuthorityRecord(a.identity, a.key, a.receiptChain, event)
							return s.data.appendAuthorityFile(ctx, data, walAuthorityName, a.records, a.receiptChain, raw[:])
						}
						if scenario != "changed-image" && !terminalCommit {
							return nil
						}
						path, err := s.data.control.CanonicalPath()
						if err != nil {
							return err
						}
						db, close, err := openWindowsMaintenanceSQLite(ctx, o.lease, filepath.Join(path, s.main), worktree)
						if err != nil {
							return err
						}
						_, err = db.ExecContext(ctx, "PRAGMA user_version=42")
						if err := errors.Join(err, close()); err != nil || !terminalCommit {
							return err
						}
						image, err := s.data.closedWorktreeImage(ctx, data, s.main, s.kind, marker.IndexIncarnation)
						if err != nil || image == before {
							return errors.Join(ErrStateChanged, err)
						}
						event.kind = walAuthorityCommitQuiesced
						if scenario == "commit-head-interrupted" {
							event.Image, before, resolutionKind = image, image, walAuthorityCommitQuiesced
						}
						raw := encodeWALAuthorityRecord(a.identity, a.key, a.receiptChain, event)
						return s.data.appendAuthorityFile(ctx, data, walAuthorityName, a.records, a.receiptChain, raw[:])
					})
			}); err != nil {
				t.Fatal(err)
			}
			attempted := false
			err = withOwner(ports.SharedLease, func(o windowsStoreMaintenance) error {
				_, err := runWindowsIndexWAL(ctx, o, func(context.Context) error { return ctx.Err() }, windowsCatalogSeedMaxSize,
					func(*index.Store, *runtime.Writer, string) (struct{}, error) {
						attempted = true
						if scenario == "concurrent-read" {
							return struct{}{}, withIndexRetryFixture(t, ctx, o, marker.IndexIncarnation,
								func(s windowsMaintenanceStore, head, data *os.File, a windowsWALAuthority) error {
									if a.pending == nil || a.pending.Image != before {
										t.Fatal("reader fixture did not observe the writer's PREPARE")
									}
									return exerciseWindowsIndexReader(t, ctx, windowsIndexImages{s.head, s.data, head, data, a.identity}, before)
								})
						}
						return struct{}{}, nil
					}, nil)
				return err
			})
			if scenario == "concurrent-read" {
				if !attempted || err != nil {
					t.Fatalf("concurrent reader failed: attempted=%v err=%v", attempted, err)
				}
				return
			}
			if scenario != "unchanged" && scenario != "commit-head-interrupted" && scenario != "abort-head-interrupted" {
				if attempted || !errors.Is(err, ErrStoreRecoveryRequired) {
					t.Fatalf("uncertain index state entered SQLite: attempted=%v err=%v", attempted, err)
				}
				return
			}
			if !attempted || err != nil {
				t.Fatalf("proven pending operation did not recover: attempted=%v err=%v", attempted, err)
			}
			if err := withOwner(ports.SharedLease, func(o windowsStoreMaintenance) error {
				return withIndexRetryFixture(t, ctx, o, marker.IndexIncarnation,
					func(s windowsMaintenanceStore, _ *os.File, data *os.File, a windowsWALAuthority) error {
						if a.pending != nil || a.records != 5 || a.head.Sequence != 2 || a.head.Image != before || a.head.Operation == pendingOperation {
							t.Fatalf("unexpected head after abort and retry: records=%d sequence=%d", a.records, a.head.Sequence)
						}
						raw, ok, err := s.data.readEntryLimit(ctx, data, walAuthorityName, walAuthorityMaxBytes)
						if err != nil || !ok {
							return errors.Join(ErrStoreRecoveryRequired, err)
						}
						if raw[2*walAuthorityRecordSize+8] != resolutionKind {
							t.Fatal("retry did not retain the proven outcome of the prior operation")
						}
						return nil
					})
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func exerciseWindowsIndexReader(t *testing.T, ctx context.Context, images windowsIndexImages, before walLogicalImage) (resultErr error) {
	t.Helper()
	dir, err := images.data.control.CanonicalPath()
	if err != nil {
		return err
	}
	writer := sql.OpenDB(&snapshotConnector{driver: &modsqlite.Driver{}, dsn: windowsSQLiteRW(filepath.Join(dir, "index.sqlite"))})
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if _, err := writer.ExecContext(ctx, "PRAGMA user_version=42"); err != nil {
		return err
	}
	reader := sql.OpenDB(&windowsIndexReadConnector{driver: &modsqlite.Driver{}, images: images})
	defer func() { resultErr = errors.Join(resultErr, reader.Close()) }()
	var current, visible int
	if err := writer.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil || current != 42 {
		t.Fatalf("fixture writer did not change its WAL state: %d %v", current, err)
	}
	if err := reader.QueryRowContext(ctx, "PRAGMA user_version").Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("reader observed unacknowledged SQLite state: %d %v", visible, err)
	}
	image, err := images.capture(ctx)
	if err != nil {
		return err
	}
	if sha256.Sum256(image) != before.SHA256 {
		t.Fatal("reader did not obtain the exact previous committed image")
	}
	return nil
}

func withIndexRetryFixture(t *testing.T, ctx context.Context, o windowsStoreMaintenance, incarnation string,
	use func(windowsMaintenanceStore, *os.File, *os.File, windowsWALAuthority) error) error {
	t.Helper()
	stores, release, err := o.openStores()
	if err != nil {
		return err
	}
	defer closeIndexRetryFixture(t, release)
	s := stores[0]
	head, err := s.head.directory(ctx)
	if err != nil {
		return err
	}
	defer closeIndexRetryFixture(t, head.Close)
	data, err := s.data.directory(ctx)
	if err != nil {
		return err
	}
	defer closeIndexRetryFixture(t, data.Close)
	id, err := s.identity(incarnation)
	if err != nil {
		return err
	}
	a, err := readWindowsWALAuthority(ctx, s.head, head, s.data, data, id)
	defer clear(a.key[:])
	if err != nil {
		return err
	}
	return use(s, head, data, a)
}

func closeIndexRetryFixture(t *testing.T, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Error(err)
	}
}
