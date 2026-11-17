//go:build linux

package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/records"
)

func TestNativeConfigurationPragmasClosed(t *testing.T) {
	for _, query := range []string{"PRAGMA journal_mode", "PRAGMA query_only=ON", "PRAGMA foreign_keys=ON", "PRAGMA wal_autocheckpoint=0"} {
		if !nativeConfigurationPragma(query) {
			t.Fatal("required configuration refused", query)
		}
	}
	for _, query := range []string{"SELECT * FROM generations", "PRAGMA journal_mode=WAL", "PRAGMA wal_checkpoint(TRUNCATE)", "PRAGMA foreign_keys=OFF", "PRAGMA query_only=ON; DROP TABLE schema_meta", "ATTACH '/target/source' AS other"} {
		if nativeConfigurationPragma(query) {
			t.Fatal("unsafe out-of-transaction statement accepted", query)
		}
	}
}

func TestNativePrefixMatchesSQLiteRecovery(t *testing.T) {
	for _, tail := range []bool{false, true} {
		for _, size := range []int{512, 4096, 65536} {
			t.Run(fmt.Sprintf("%d/tail=%t", size, tail), func(t *testing.T) {
				f := newWALSnapshotFixtureWithOptions(t, size, tail)
				main, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				wal, err := os.ReadFile(filepath.Join(f.state, "catalog.sqlite-wal"))
				if err != nil {
					t.Fatal(err)
				}
				want := fixturePrefixReceipt(t, main, wal, f.pair.WALCommitFrame, f.pair.WALCommitPages)
				got, err := observeNativeWALPrefix(f.ctx, bytes.NewReader(main), int64(len(main)), bytes.NewReader(wal), int64(len(wal)))
				if err != nil || got != want {
					t.Fatalf("streaming prefix differs from SQLite oracle: %+v %+v %v", got, want, err)
				}
			})
		}
	}
}

func nativeFixtureDomain(t *testing.T) (protectedKernelFixture, *stateDomain) {
	t.Helper()
	if os.Getenv("MCE4D_NATIVE_OWNER_FIXTURE") != "synthetic-v1" {
		t.Skip("requires root-owned, separately provisioned native WAL fixture")
	}
	f := openProtectedKernelFixture(t)
	domain, err := f.domain.Child(f.ctx, "worktrees/"+ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = domain.root.Close() })
	return f, domain
}

func TestNativeCaptureChildFixture(t *testing.T) {
	f, domain := nativeFixtureDomain(t)
	name := os.Getenv("MCE4D_NATIVE_CAPTURE_NAME")
	if name != "index.sqlite" && name != "records.sqlite" {
		t.Fatal("unrecognized fixed capture source")
	}
	lease, err := runtime.AcquireLifecycle(f.ctx, domain.root, ownerWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	observed, err := captureNativeWALImage(f.ctx, domain, lease, ownerWorktree, name)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "CAPTURE "+string(raw))
}

func TestNativePeerReaderFixture(t *testing.T) {
	f, domain := nativeFixtureDomain(t)
	lease, err := runtime.AcquireLifecycle(f.ctx, domain.root, ownerWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	state := nativeFixtureStore(t, f, domain, lease, "index.sqlite", os.Getenv("MCE4D_NATIVE_INDEX_INCARNATION"))
	owner, err := openNativeIndexOwner(f.ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	tx, err := owner.lifetime.pools[0].BeginTx(f.ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(f.ctx, "SELECT count(*) FROM generations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("peer could not observe acknowledged state: %d %v", count, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func nativeFixtureCapture(t *testing.T, ctx context.Context, name string) (nativeWALCapture, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		return nativeWALCapture{}, err
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestNativeCaptureChildFixture$", "-test.timeout=5s")
	cmd.Env = append(os.Environ(), "MCE4D_NATIVE_CAPTURE_NAME="+name)
	output := &boundedProfileOutput{limit: 8192}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		return nativeWALCapture{}, fmt.Errorf("capture child: %w: %s", err, output.String())
	}
	lines := bufio.NewScanner(bytes.NewReader(output.Bytes()))
	for lines.Scan() {
		if raw, ok := strings.CutPrefix(lines.Text(), "CAPTURE "); ok {
			var out nativeWALCapture
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				return out, err
			}
			return out, nil
		}
	}
	return nativeWALCapture{}, fmt.Errorf("capture output unavailable: %s", output.String())
}

func nativeFixtureStore(t *testing.T, f protectedKernelFixture, domain *stateDomain, lease *runtime.Lifecycle, name, incarnation string) *nativeWALStore {
	t.Helper()
	kind := strings.TrimSuffix(name, ".sqlite")
	journal, err := domain.Child(f.ctx, ".maintenance/"+kind)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.root.Close() })
	control, err := paths.OpenRoot("/authority/" + kind)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Close() })
	inc, err := profileUUIDBytes(incarnation)
	if err != nil {
		t.Fatal(err)
	}
	store := &nativeWALStore{domain: domain, journal: journal, control: control, lease: lease, scope: ownerWorktree, name: name,
		identity: walAuthorityIdentity{Authority: [16]byte{7}, Store: sha256.Sum256([]byte("native fixture " + kind)), Incarnation: inc}, key: sha256.Sum256([]byte("synthetic native " + kind)),
		authorize: func(ctx context.Context) error { return ctx.Err() }, capture: func(ctx context.Context) (nativeWALCapture, error) { return nativeFixtureCapture(t, ctx, name) }}
	return store
}

func bootstrapNativeFixtureLog(t *testing.T, s *nativeWALStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	observed, err := s.observed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	control, _ := s.control.CanonicalPath()
	f, err := os.OpenFile(filepath.Join(control, walHeadName), os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	head, err := openWALHeadAuthority(ctx, s.control, []*paths.Root{s.domain.root, s.domain.protected[0]}, f, s.identity, s.key)
	if err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	journal, _ := s.journal.root.CanonicalPath()
	f, err = os.OpenFile(filepath.Join(journal, walAuthorityName), os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		_ = head.Close()
		t.Fatal(err)
	}
	log, err := openAnchoredWALAuthorityLog(ctx, s.journal, f, head, s.identity, s.key, walAuthorityMaxBytes, &observed.Image)
	if err != nil {
		_ = head.Close()
		t.Fatal(err)
	}
	if err := errors.Join(log.Close(), head.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeFencedOwnersFixture(t *testing.T) {
	f, domain := nativeFixtureDomain(t)
	ctx := f.ctx
	exclusive, err := runtime.AcquireLifecycle(ctx, domain.root, ownerWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, exclusive, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var indexInc, recordsInc string
	rootPath, _ := domain.root.CanonicalPath()
	for _, name := range []string{"index.sqlite", "records.sqlite"} {
		db, err := sql.Open("sqlite", profileNativeDSN(filepath.Join(rootPath, name)))
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		owner, err := OpenMaintenanceOwner(ctx, exclusive, db, ownerWorktree)
		if err != nil {
			t.Fatal(err)
		}
		if name == "index.sqlite" {
			identity, err := owner.InitializeIndex(ctx, writer, ownerJob)
			if err != nil {
				t.Fatal(err)
			}
			indexInc = identity.Incarnation
		} else {
			identity, err := owner.InitializeRecords(ctx, writer, ownerJob)
			if err != nil {
				t.Fatal(err)
			}
			recordsInc = identity.Incarnation
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(writer.Close(), exclusive.Close()); err != nil {
		t.Fatal(err)
	}
	lease, err := runtime.AcquireLifecycle(ctx, domain.root, ownerWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	indexState := nativeFixtureStore(t, f, domain, lease, "index.sqlite", indexInc)
	recordsState := nativeFixtureStore(t, f, domain, lease, "records.sqlite", recordsInc)
	bootstrapNativeFixtureLog(t, indexState)
	bootstrapNativeFixtureLog(t, recordsState)
	indexOwner, err := openNativeIndexOwner(ctx, indexState)
	if err != nil {
		t.Fatal(err)
	}
	defer indexOwner.Close()
	recordsOwner, err := openNativeRecordsOwner(ctx, recordsState)
	if err != nil {
		t.Fatal(err)
	}
	defer recordsOwner.Close()
	recordStore, _ := recordsOwner.Store()
	state, err := recordStore.ReadState(ctx, 100, func(context.Context, records.Identity) error { return nil })
	if err != nil || state.RecordEpoch != 0 || state.Identity.Incarnation != recordsInc {
		t.Fatalf("native records read: %+v %v", state, err)
	}
	indexStore, _ := indexOwner.Store()
	if identity, err := indexStore.Identity(ctx); err != nil || identity.Incarnation != indexInc {
		t.Fatalf("native index identity: %+v %v", identity, err)
	}
	readers, writePool := indexOwner.lifetime.pools[0], indexOwner.lifetime.pools[1]
	readTxs := make([]*sql.Tx, 0, 4)
	for i := 0; i < 4; i++ {
		tx, err := readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		readTxs = append(readTxs, tx)
		defer tx.Rollback()
	}
	insert := func(tx *sql.Tx, id string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING',?,?,1,?,'2026-11-14T00:00:00Z')`, id, strings.Repeat("a", 64), ownerJob, strings.Repeat("b", 64), strings.Repeat("c", 64))
		return err
	}
	write, err := writePool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insert(write, "33333333-3333-4333-8333-333333333333"); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(); err != nil {
		t.Fatal("commit/receipt", err)
	}
	// A second process opens while this process retains four pinned readers
	// and its native writer connection. All captures stay in child processes.
	executable, _ := os.Executable()
	peer := exec.CommandContext(ctx, executable, "-test.v", "-test.run=^TestNativePeerReaderFixture$", "-test.timeout=5s")
	peer.Env = append(os.Environ(), "MCE4D_NATIVE_INDEX_INCARNATION="+indexInc)
	if output, err := peer.CombinedOutput(); err != nil {
		t.Fatalf("concurrent peer: %v %s", err, output)
	}
	for _, tx := range readTxs {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM generations").Scan(&count); err != nil || count != 0 {
			t.Fatalf("pinned reader changed: %d %v", count, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if err := lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatal("native pools did not retain DB lifecycle", err)
	}
	if readers.Stats().MaxOpenConnections != 4 || readers.Stats().OpenConnections != 4 || writePool.Stats().MaxOpenConnections != 1 {
		t.Fatal("pool caps/connections differ from DB-02")
	}
	write, err = writePool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insert(write, "44444444-4444-4444-8444-444444444444"); err != nil {
		t.Fatal(err)
	}
	if err := write.Rollback(); err != nil {
		t.Fatal("rollback/receipt", err)
	}
	// Cancel after a SQL write: database/sql rolls back on its goroutine. A
	// cancelled capture leaves pending, then an independent unchanged image
	// permits explicit ABORT without opening writable SQLite for recovery.
	cancelled, cancel := context.WithCancel(ctx)
	write, err = writePool.BeginTx(cancelled, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insert(write, "55555555-5555-4555-8555-555555555555"); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = write.Rollback()
	if err := indexState.recoverUnchangedPending(ctx); err != nil {
		t.Fatal("unchanged pending recovery", err)
	}
	tx, err := readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM generations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback lost/duplicated a committed generation: %d %v", count, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// The SQL commit succeeds, then the independent capture fails. No ACK or
	// data may escape; unchanged-only recovery must refuse this changed image.
	originalCapture := indexState.capture
	failCapture := false
	indexState.capture = func(ctx context.Context) (nativeWALCapture, error) {
		if failCapture {
			return nativeWALCapture{}, errors.New("synthetic capture failure after commit")
		}
		return originalCapture(ctx)
	}
	write, err = writePool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insert(write, "66666666-6666-4666-8666-666666666666"); err != nil {
		t.Fatal(err)
	}
	failCapture = true
	if err := write.Commit(); !errors.Is(err, errNativeOutcomeUnknown) {
		t.Fatal("commit without capture was acknowledged", err)
	}
	failCapture = false
	journalPath, _ := indexState.journal.root.CanonicalPath()
	journalBefore, err := os.ReadFile(filepath.Join(journalPath, walAuthorityName))
	if err != nil {
		t.Fatal(err)
	}
	if err := indexState.recoverUnchangedPending(ctx); !errors.Is(err, errNativeOutcomeUnknown) {
		t.Fatal("changed image was silently aborted", err)
	}
	journalAfter, err := os.ReadFile(filepath.Join(journalPath, walAuthorityName))
	if err != nil || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatal("unknown recovery rewrote the pending journal", err)
	}
	if read, err := readers.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}); !errors.Is(err, errWALAuthorityPending) || read != nil {
		t.Fatal("reader entered a changed pending state", err)
	}
	if err := errors.Join(recordsOwner.Close(), indexOwner.Close()); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "NATIVE_OWNERS_PASS: real index/records schema; four pinned readers plus writer and peer process; commit ACK; rollback/cancel recovery; changed pending blocked; lifecycle/caps")
}
