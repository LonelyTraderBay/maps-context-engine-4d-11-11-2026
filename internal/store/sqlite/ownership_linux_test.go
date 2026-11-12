//go:build linux

package sqlite

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const ownerWorktree = "11111111-1111-4111-8111-111111111111"
const ownerJob = "22222222-2222-4222-8222-222222222222"

func ownerDB(t *testing.T, path string, limit int) *sql.DB {
	t.Helper()
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(limit)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestIndexPoolOwnershipKeepsRealLifecycleUntilHandlesClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	exclusive, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writerLease, err := runtime.AcquireWriter(ctx, exclusive, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "index.sqlite")
	maintenance := ownerDB(t, path, 1)
	maintenanceOwner, err := OpenMaintenanceOwner(ctx, exclusive, maintenance, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maintenanceOwner.InitializeIndex(ctx, writerLease, ownerJob); err != nil {
		t.Fatal(err)
	}
	if err := maintenanceOwner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writerLease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	shared, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	readers, writer := ownerDB(t, path, 4), ownerDB(t, path, 1)
	owner, err := OpenIndexOwner(ctx, shared, readers, writer, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Identity(ctx); err != nil {
		t.Fatal(err)
	}
	if err := shared.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("pool did not retain lifecycle lease: %v", err)
	}
	conn, err := readers.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); !errors.Is(err, ErrPoolBusy) {
		t.Fatalf("owner released with borrowed connection: %v", err)
	}
	if _, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, 100*time.Millisecond); !errors.Is(err, runtime.ErrStoreMaintenance) {
		t.Fatalf("maintenance entered while borrowed handle remained: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Store(); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("closed owner returned store: %v", err)
	}
	if err := shared.Close(); err != nil {
		t.Fatal(err)
	}
	reacquired, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIndexCaptureUsesRealWriterAndLifecycleLeases(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	exclusive, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	maintenanceWriter, err := runtime.AcquireWriter(ctx, exclusive, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "index.sqlite")
	maintenance := ownerDB(t, path, 1)
	maintenanceOwner, err := OpenMaintenanceOwner(ctx, exclusive, maintenance, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maintenanceOwner.InitializeIndex(ctx, maintenanceWriter, ownerJob); err != nil {
		t.Fatal(err)
	}
	profile, policyDigest, provider := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	generation := "33333333-3333-4333-8333-333333333333"
	if _, err := maintenance.Exec(`INSERT INTO generations(generation_id,profile_id,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,'BUILDING',?,?,1,?,'now')`, generation, profile, ownerJob, policyDigest, provider); err != nil {
		t.Fatal(err)
	}
	if err := maintenanceOwner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := maintenanceWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	shared, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	readers, writerDB := ownerDB(t, path, 4), ownerDB(t, path, 1)
	owner, err := OpenIndexOwner(ctx, shared, readers, writerDB, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, shared, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	file, err := snapshot.NewFile("a.go", []byte("package demo\n"), 128)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: ownerWorktree, ProfileID: profile, PolicyDigest: policyDigest, ProviderDigest: provider, Files: []model.ManifestFile{{Path: "a.go", Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(128)
	if err != nil {
		t.Fatal(err)
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	request := index.CaptureWrite{GenerationID: generation, JobID: ownerJob, PolicyEpoch: 1, Snapshot: captured, Scanner: scanner, Authorize: func(context.Context, string, string, string, int64) error {
		once.Do(func() { close(entered); <-resume })
		return nil
	}, Writer: writer, MaxManifestBytes: 8192, MaxSourceBytes: 1024}
	result := make(chan error, 1)
	go func() { result <- store.StoreCapturedBlobs(ctx, request) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(resume)
		t.Fatal("capture did not reach authorization")
	}
	if err := writer.Close(); !errors.Is(err, runtime.ErrWriterInUse) {
		close(resume)
		t.Fatalf("active capture lost writer lock: %v", err)
	}
	close(resume)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("capture did not finish")
	}
	var count int
	if err := writerDB.QueryRow(`SELECT count(*) FROM content_blobs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("capture was not persisted: %d %v", count, err)
	}
	if err := os.Rename(filepath.Join(dir, "writer.lock"), filepath.Join(dir, "writer.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "writer.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCapturedBlobs(ctx, request); !errors.Is(err, runtime.ErrLockReplaced) {
		t.Fatalf("replaced writer lock accepted: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreCapturedBlobs(ctx, request); !errors.Is(err, runtime.ErrInvalidLease) {
		t.Fatalf("closed writer accepted: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := shared.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerSubprocess(t *testing.T) {
	if os.Getenv("MCE4D_OWNER_HELPER") != "1" {
		return
	}
	dir := os.Getenv("MCE4D_OWNER_DIR")
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	shared, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	path := filepath.Join(dir, "index.sqlite")
	readers, writer := ownerDB(t, path, 4), ownerDB(t, path, 1)
	owner, err := OpenIndexOwner(ctx, shared, readers, writer, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Identity(ctx); err != nil {
		t.Fatal(err)
	}
	if readers.Stats().Idle == 0 {
		t.Fatal("fixture requires an idle pooled connection")
	}
	fmt.Fprintln(os.Stdout, "READY")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

func TestIndexIdlePoolBlocksMaintenanceAcrossProcesses(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "a")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	exclusive, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writerLease, err := runtime.AcquireWriter(ctx, exclusive, ownerJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	maintenance := ownerDB(t, filepath.Join(dir, "index.sqlite"), 1)
	maintenanceOwner, err := OpenMaintenanceOwner(ctx, exclusive, maintenance, ownerWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maintenanceOwner.InitializeIndex(ctx, writerLease, ownerJob); err != nil {
		t.Fatal(err)
	}
	if err := maintenanceOwner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writerLease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, binary, "-test.run=^TestOwnerSubprocess$")
	cmd.Env = append(os.Environ(), "MCE4D_OWNER_HELPER=1", "MCE4D_OWNER_DIR="+dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "READY\n" {
		t.Fatalf("pool helper: %q %v", line, err)
	}
	if _, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, 120*time.Millisecond); !errors.Is(err, runtime.ErrStoreMaintenance) {
		t.Fatalf("maintenance ignored idle pool in another process: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	after, err := runtime.AcquireLifecycle(ctx, root, ownerWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatalf("maintenance did not resume after process exit: %v", err)
	}
	if err := after.Close(); err != nil {
		t.Fatal(err)
	}
}
