//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

func TestCacheChargeRetainsCheckpointAndMetadataCapacity(t *testing.T) {
	image := walLogicalImage{PageSize: 4096, PageCount: 100, SHA256: sha256.Sum256([]byte("ack"))}
	before := cacheUsage{main: 4096, wal: 100 * (4096 + 24), other: 1000}
	charged, err := cacheCharge(before, image, 0, 0, 8<<20)
	if err != nil || charged < 100*4096+before.wal+65536+before.other {
		t.Fatal("checkpoint or shm capacity was not charged", charged, err)
	}
	if _, err := cacheCharge(before, image, 100, 512, charged); !errors.Is(err, ErrCacheCapacity) {
		t.Fatal("commit could spend an already full budget", err)
	}
	if _, err := cacheCharge(before, image, 1<<50, 0, 1<<53-1); !errors.Is(err, ErrCacheCapacity) {
		t.Fatal("unbounded page count accepted", err)
	}
}

func TestCacheLockProbeChild(t *testing.T) {
	path := os.Getenv("MCE4D_CACHE_LOCK_TEST_FILE")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(os.SEEK_SET), Start: 0, Len: 1}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lock); !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
		t.Fatal("parent POSIX lock was lost during quota observation", err)
	}
}

func TestCacheObservationDoesNotDropPOSIXLocks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "index.sqlite"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte("synthetic lock canary")); err != nil {
		t.Fatal(err)
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: int16(os.SEEK_SET), Start: 0, Len: 1}
	if err := unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if usage, err := observeCacheUsage(ctx, root, 1<<20); err != nil || usage.main != 21 {
		t.Fatal("bounded O_PATH cache observation", usage, err)
	}
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCacheLockProbeChild$")
	child.Env = append(os.Environ(), "MCE4D_CACHE_LOCK_TEST_FILE="+file.Name())
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if err := os.Link(file.Name(), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := observeCacheUsage(ctx, root, 1<<20); !errors.Is(err, ErrStateUnsafe) {
		t.Fatal("cache hardlink was accepted", err)
	}
}

func TestProtectedCacheBudgetFixture(t *testing.T) {
	ctx, root := profileCatalogFixture(t, true)
	owner, err := OpenProtectedWorktreeWithCacheLimit(ctx, root, ownerWorktree, 1<<20, func(ctx context.Context) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	worker := owner.profile.captureClient
	if worker == nil || worker.cmd.Process == nil {
		t.Fatal("native owner did not start its independent capture worker")
	}
	workerPID := worker.cmd.Process.Pid
	writer, err := runtime.AcquireWriter(ctx, owner.lease, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	pool := owner.Index.lifetime.pools[1]
	begin := func() *sql.Tx {
		tx, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	insert := func(tx *sql.Tx, created string) {
		_, err := tx.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING',?,?,1,?,?)`, ownerJob, strings.Repeat("a", 64), ownerJob, strings.Repeat("b", 64), strings.Repeat("c", 64), created)
		if err != nil {
			t.Fatal(err)
		}
	}
	tx := begin()
	before, err := observeCacheUsage(ctx, owner.domain.root, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	insert(tx, strings.Repeat("x", 2<<20))
	after, err := observeCacheUsage(ctx, owner.domain.root, 8<<20)
	if err != nil || before.main != after.main || before.wal != after.wal {
		t.Fatal("native SQL spilled before the quota commit check", before, after, err)
	}
	if err := tx.Commit(); !errors.Is(err, ErrCacheCapacity) || errors.Is(err, ErrNativeOutcomeUnknown) {
		t.Fatal("over-capacity SQL commit was not safely rolled back", err)
	}
	owner.quota.limit = 8 << 20 // fixture-only increase after the rollback is settled
	read, err := owner.Index.lifetime.pools[0].BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := read.QueryRowContext(ctx, "SELECT count(*) FROM generations").Scan(&count); err != nil || count != 0 {
		t.Fatal("quota rollback published a row", count, err)
	}
	tx = begin()
	insert(tx, "2026-11-14T00:00:00Z")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := read.QueryRowContext(ctx, "SELECT count(*) FROM generations").Scan(&count); err != nil || count != 0 {
		t.Fatal("quota writer changed a pinned reader", count, err)
	}
	if err := read.Commit(); err != nil {
		t.Fatal(err)
	}
	index, _ := owner.Index.Store()
	reservation, err := owner.ReserveCache(ctx, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Identity(ctx); err != nil {
		t.Fatal("cache reservation blocked a reader", err)
	}
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if tx, err := pool.BeginTx(short, nil); tx != nil || err == nil {
		t.Fatal("native writer bypassed the cache reservation", err)
	}
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	cacheRoot, lease, protected, err := owner.CacheResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := packstore.New(cacheRoot, protected, lease, ownerWorktree, 64<<10, 128<<10, 4, owner.Check,
		func(ctx context.Context, n int64) (packstore.BudgetReservation, error) {
			return owner.ReserveCache(ctx, n)
		})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("synthetic archive\n"), 1000)
	verify := func(ctx context.Context, data []byte) error {
		if !bytes.Equal(data, payload) {
			return errors.New("invalid synthetic archive")
		}
		_, err := index.Identity(ctx) // required authority recheck while reservation is held
		return err
	}
	if err := spool.Create(ctx, ownerJob, payload, verify); err != nil {
		t.Fatal("retained result publication under total quota", err)
	}
	if loaded, err := spool.Read(ctx, ownerJob, verify); err != nil || !bytes.Equal(loaded, payload) {
		t.Fatal("retained result read", err)
	}
	// Storage-only synthetic rename: the actual protected cache reservation
	// includes its pending/published bytes and permits a live SQL reader while
	// held. This guard proves owner continuity, not typed/source provenance.
	beforeRename, err := observeCacheUsage(ctx, cacheRoot, owner.quota.limit)
	if err != nil {
		t.Fatal(err)
	}
	scanner, _ := policy.NewScanner(1 << 20)
	renameStore, err := lsp.NewBudgetedResultStore(cacheRoot, lease, ownerWorktree, scanner, 64<<10, 128<<10, 5, 4, owner.Check,
		func(ctx context.Context, n int64) (packstore.BudgetReservation, error) {
			return owner.ReserveCache(ctx, n)
		})
	if err != nil {
		t.Fatal(err)
	}
	h := strings.Repeat("a", 64)
	member := model.ScopeMember{WorktreeID: ownerWorktree, ProfileID: h, GenerationID: ownerJob, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: ownerJob, RecordsIncarnation: ownerJob}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: ownerJob, UnavailableMembers: []model.MemberUnavailable{}}
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	ref := model.SourceRef{WorktreeID: ownerWorktree, ProfileID: h, GenerationID: ownerJob, Path: "fixture.go", ContentHash: h, Range: model.ByteRange{StartByte: 0, EndByte: 1}}
	renamePayload := lsp.RenameResultPayload{InitialRequest: model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: ref.Path, ContentHash: h, ByteOffset: 0}, NewName: "Renamed", PreserveContracts: true}, ProviderDigest: h,
		FilePreconditions: []model.RenameFilePrecondition{{WorktreeID: ownerWorktree, ProfileID: h, GenerationID: ownerJob, Path: ref.Path, ContentHash: h}}, Edits: []model.ProposedEdit{{SourceRef: ref, NewText: "Renamed"}}, Conflicts: []model.Gap{}}
	renameGuard := func(ctx context.Context, _ lsp.RenameStoredResult) error { _, err := index.Identity(ctx); return err }
	header, err := renameStore.Create(ctx, scope, h, renamePayload, renameGuard)
	if err != nil {
		t.Fatal("rename publication under shared native quota", err)
	}
	if loaded, err := renameStore.Read(ctx, header.ResultID, h, scope, renameGuard); err != nil || len(loaded.Payload.Edits) != 1 {
		t.Fatal("rename native quota readback", err)
	}
	afterRename, err := observeCacheUsage(ctx, cacheRoot, owner.quota.limit)
	if err != nil || afterRename.other <= beforeRename.other {
		t.Fatal("rename spool not charged to total cache", beforeRename, afterRename, err)
	}
	tooLarge, err := owner.ReserveCache(ctx, owner.quota.limit)
	if tooLarge != nil {
		_ = tooLarge.Close()
	}
	if !errors.Is(err, ErrCacheCapacity) {
		t.Fatal("total cache quota did not include published files", err)
	}
	if owner.profile.captureClient != worker || worker.cmd.Process.Pid != workerPID {
		t.Fatal("capture worker was restarted between admissions")
	}
	if err := worker.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Identity(ctx); !errors.Is(err, ErrNativeOutcomeUnknown) {
		t.Fatal("dead capture worker admitted a data transaction", err)
	}
	if reservation, err := owner.ReserveCache(ctx, 4096); reservation != nil || !errors.Is(err, ErrNativeOutcomeUnknown) {
		t.Fatal("dead capture stream was reused for a reservation", err)
	}
	if owner.profile.captureClient != worker {
		t.Fatal("failed capture worker was silently replaced")
	}
	fmt.Println("PROTECTED_CACHE_BUDGET_PASS: no precommit spill; quota rollback; pinned reader; native/pack/rename gate; reentrant reader; persisted charge")
}
