package index

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

const fixtureGeneration = "33333333-3333-4333-8333-333333333333"
const fixtureJob = "44444444-4444-4444-8444-444444444444"

type writerProbe struct {
	mu         sync.Mutex
	calls      int
	failAt     int
	streamHeld bool
}

type fixtureWriterGuard struct{}

type fixtureStreamGuard struct{ writer *writerProbe }

func (w *writerProbe) HoldIndexStream(ctx context.Context, worktreeID, jobID string) (ports.LeaseGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if worktreeID != fixtureWorktree || jobID != fixtureJob {
		return nil, errFixtureLease
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.streamHeld {
		return nil, ports.ErrIndexStreamBusy
	}
	w.streamHeld = true
	return &fixtureStreamGuard{writer: w}, nil
}

func (g *fixtureStreamGuard) Close() error {
	if g.writer == nil {
		return errFixtureLease
	}
	g.writer.mu.Lock()
	defer g.writer.mu.Unlock()
	g.writer.streamHeld = false
	g.writer = nil
	return nil
}

func (fixtureWriterGuard) Close() error { return nil }

func (w *writerProbe) HoldWriter(ctx context.Context, worktreeID, jobID string) (ports.LeaseGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if worktreeID != fixtureWorktree || jobID != fixtureJob {
		return nil, errFixtureLease
	}
	return fixtureWriterGuard{}, nil
}

func (w *writerProbe) CheckWriter(ctx context.Context, worktreeID, jobID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if worktreeID != fixtureWorktree || jobID != fixtureJob || w.calls == w.failAt {
		return errFixtureLease
	}
	return nil
}

func captureWriteFixture(t *testing.T, path, content string) (*Store, *sql.DB, CaptureWrite) {
	t.Helper()
	db, _ := fixtureDB(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, db, fixtureWorktree, fixtureLease(ports.ExclusiveLease)); err != nil {
		t.Fatal(err)
	}
	lease := fixtureLease(ports.SharedLease)
	store, err := Open(ctx, db, db, fixtureWorktree, lease)
	if err != nil {
		t.Fatal(err)
	}
	profile, policyDigest, provider := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	conn, err := borrowConfigured(ctx, db, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING',?,?,1,?,'now')`, fixtureGeneration, profile, fixtureJob, policyDigest, provider); err != nil {
		t.Fatal(err)
	}
	if err := releaseConfigured(ctx, conn, false); err != nil {
		t.Fatal(err)
	}
	file, err := snapshot.NewFile(path, []byte(content), 128)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{
		SchemaVersion: 1, WorktreeID: fixtureWorktree, ProfileID: profile, PolicyDigest: policyDigest, ProviderDigest: provider,
		Files:                  []model.ManifestFile{{Path: path, Status: "captured", ContentHash: &hash, ByteLength: &length}},
		DependencyInputDigests: []string{},
	}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(128)
	if err != nil {
		t.Fatal(err)
	}
	request := CaptureWrite{
		GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: captured, Scanner: scanner,
		Authorize: func(context.Context, string, string, string, int64) error { return nil },
		Writer:    &writerProbe{}, MaxManifestBytes: 8192, MaxSourceBytes: 1024,
	}
	return store, db, request
}

func blobCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM content_blobs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStoreCapturedBlobsBindsGenerationAndIsIdempotent(t *testing.T) {
	store, db, request := captureWriteFixture(t, "a.go", "abc")
	ctx := context.Background()
	if err := store.StoreCapturedBlobs(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCapturedBlobs(ctx, request); err != nil {
		t.Fatalf("idempotent content insert: %v", err)
	}
	var value []byte
	if err := db.QueryRow(`SELECT bytes FROM content_blobs`).Scan(&value); err != nil || string(value) != "abc" || blobCount(t, db) != 1 {
		t.Fatalf("blob state: %q %v", value, err)
	}
	request.JobID = "55555555-5555-4555-8555-555555555555"
	request.Writer = &writerProbe{}
	if err := store.StoreCapturedBlobs(ctx, request); err == nil || blobCount(t, db) != 1 {
		t.Fatalf("wrong job was accepted: %v", err)
	}
}

func TestStoreCapturedBlobsRejectsSecretsDeniedPathsAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name, path, content string
		want                error
	}{
		{"secret", "a.go", `api_key = "abcdefghijklmnop"`, policy.ErrSecretBlocked},
		{"denied", ".env", "fixture", policy.ErrDeniedPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, db, request := captureWriteFixture(t, tc.path, tc.content)
			if err := store.StoreCapturedBlobs(context.Background(), request); !errors.Is(err, tc.want) || blobCount(t, db) != 0 {
				t.Fatalf("rejected content persisted: %v", err)
			}
		})
	}
	store, db, request := captureWriteFixture(t, "a.go", "abcdef")
	request.MaxSourceBytes = 5
	if err := store.StoreCapturedBlobs(context.Background(), request); !errors.Is(err, ErrCaptureLimit) || blobCount(t, db) != 0 {
		t.Fatalf("source budget ignored: %v", err)
	}
}

func TestStoreCapturedBlobsRollsBackOnRevocation(t *testing.T) {
	store, db, request := captureWriteFixture(t, "a.go", "abc")
	denied := errors.New("fixture policy revoked")
	calls := 0
	request.Authorize = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return denied
		}
		return nil
	}
	if err := store.StoreCapturedBlobs(context.Background(), request); !errors.Is(err, denied) || blobCount(t, db) != 0 {
		t.Fatalf("policy revocation did not roll back: %v", err)
	}
	request.Authorize = func(context.Context, string, string, string, int64) error { return nil }
	request.Writer = &writerProbe{failAt: 2}
	if err := store.StoreCapturedBlobs(context.Background(), request); !errors.Is(err, errFixtureLease) || blobCount(t, db) != 0 {
		t.Fatalf("lost writer lease did not roll back: %v", err)
	}
}

func TestStoreCapturedBlobsRejectsCorruptExistingContent(t *testing.T) {
	store, db, request := captureWriteFixture(t, "a.go", "abc")
	file, ok := request.Snapshot.File("a.go")
	if !ok {
		t.Fatal("fixture source missing")
	}
	if _, err := db.Exec(`INSERT INTO content_blobs(content_hash,bytes,byte_length) VALUES(?,X'78797A',3)`, file.ContentHash()); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCapturedBlobs(context.Background(), request); !errors.Is(err, ErrBlobMismatch) || blobCount(t, db) != 1 {
		t.Fatalf("corrupt content accepted: %v", err)
	}
}

func TestStoreCapturedBlobsAcceptsEmptyFileAsBlob(t *testing.T) {
	store, db, request := captureWriteFixture(t, "empty.go", "")
	file, err := snapshot.NewFile("empty.go", nil, 128)
	if err != nil {
		t.Fatal(err)
	}
	request.Snapshot, err = snapshot.Freeze(request.Snapshot.Manifest(), []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := store.StoreCapturedBlobs(context.Background(), request); err != nil {
			t.Fatalf("empty content insert %d: %v", i, err)
		}
	}
	var storageClass string
	var length int
	if err := db.QueryRow(`SELECT typeof(bytes),length(bytes) FROM content_blobs`).Scan(&storageClass, &length); err != nil || storageClass != "blob" || length != 0 || blobCount(t, db) != 1 {
		t.Fatalf("empty content must be a zero-length BLOB: %q %d %v", storageClass, length, err)
	}
}

func TestStoreCapturedBlobsRejectsGenerationMismatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change string
	}{
		{"owner", `UPDATE generations SET owner_job_id='55555555-5555-4555-8555-555555555555'`},
		{"profile", `UPDATE generations SET profile_id='dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'`},
		{"policy", `UPDATE generations SET policy_digest='dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'`},
		{"epoch", `UPDATE generations SET policy_epoch=2`},
		{"provider", `UPDATE generations SET provider_fingerprint='dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'`},
		{"snapshot", `UPDATE generations SET snapshot_digest='dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'`},
		{"state", `UPDATE generations SET state='FAILED'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, db, request := captureWriteFixture(t, "a.go", "abc")
			if _, err := db.Exec(tc.change); err != nil {
				t.Fatal(err)
			}
			if err := store.StoreCapturedBlobs(context.Background(), request); !errors.Is(err, ErrCaptureMismatch) || blobCount(t, db) != 0 {
				t.Fatalf("mismatched generation persisted source: %v", err)
			}
		})
	}
}

func TestStoreCapturedBlobsReportsCommittedFinalizationFailure(t *testing.T) {
	store, db, request := captureWriteFixture(t, "main.go", "package main\n")
	request.Writer = failingCloseWriter{}
	err := store.StoreCapturedBlobs(context.Background(), request)
	if !errors.Is(err, ErrAfterCommit) || !errors.Is(err, errFixtureLease) || blobCount(t, db) != 1 {
		t.Fatalf("commit/finalization outcome hidden: %v", err)
	}
}

func TestStoreCapturedBlobsRollsBackOnLifecycleLeaseLoss(t *testing.T) {
	store, db, request := captureWriteFixture(t, "a.go", "abc")
	lease := store.lease.(*leaseProbe)
	lease.failAt = lease.calls + 2
	if err := store.StoreCapturedBlobs(context.Background(), request); !errors.Is(err, errFixtureLease) || blobCount(t, db) != 0 {
		t.Fatalf("lost lifecycle lease did not roll back: %v", err)
	}
}
