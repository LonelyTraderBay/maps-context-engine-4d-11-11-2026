//go:build linux

package indexer

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
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const recoveryWorktree = "11111111-1111-4111-8111-111111111111"
const recoveryJob = "22222222-2222-4222-8222-222222222222"
const recoveryRun = "99999999-9999-4999-8999-999999999999"
const recoveryOld = "33333333-3333-4333-8333-333333333333"
const recoveryCandidate = "44444444-4444-4444-8444-444444444444"

func recoverySnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	file, err := snapshot.NewFile("main.go", []byte("package main\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: recoveryWorktree, ProfileID: strings.Repeat("a", 64), PolicyDigest: strings.Repeat("b", 64), ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{{Path: "main.go", Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return captured
}

func recoveryPools(t *testing.T, dir string) (*sql.DB, *sql.DB) {
	t.Helper()
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(dir, "index.sqlite"))}).String()
	open := func(limit int) *sql.DB {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(limit)
		return db
	}
	return open(4), open(1)
}

func stageRecoveryFixture(t *testing.T, ctx context.Context, store *index.Store, writer *runtime.Writer, id string) index.CaptureWrite {
	t.Helper()
	captured := recoverySnapshot(t)
	authorizeGeneration := func(context.Context, string, string, string, int64) error { return nil }
	if err := store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: id, JobID: recoveryJob, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Authorize: authorizeGeneration, MaxManifestBytes: 8192}); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	capture := index.CaptureWrite{GenerationID: id, JobID: recoveryJob, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 1024, Authorize: func(context.Context, string, string, string, int64) error { return nil }}
	if err := store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: []index.FileClassification{{Path: "main.go", Kind: "go"}}}); err != nil {
		t.Fatal(err)
	}
	return capture
}

func TestRecoveryBuildingProcessHelper(t *testing.T) {
	dir := os.Getenv("MCE4D_TEST_RECOVERY_DIR")
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, root, recoveryWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	readers, writerPool := recoveryPools(t, dir)
	owner, err := sqliteowner.OpenIndexOwner(ctx, lease, readers, writerPool, recoveryWorktree)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, lease, recoveryJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	stageRecoveryFixture(t, ctx, store, writer, recoveryCandidate)
	fmt.Fprintln(os.Stdout, "BUILDING_READY")
	<-ctx.Done()
}

func TestRecoveryAfterWriterProcessDeathKeepsPublishedSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	exclusive, err := runtime.AcquireLifecycle(ctx, root, recoveryWorktree, ports.ExclusiveLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	maintenanceWriter, err := runtime.AcquireWriter(ctx, exclusive, recoveryJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unused, db := recoveryPools(t, dir)
	if err := unused.Close(); err != nil {
		t.Fatal(err)
	}
	maintenance, err := sqliteowner.OpenMaintenanceOwner(ctx, exclusive, db, recoveryWorktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance.InitializeIndex(ctx, maintenanceWriter, recoveryJob); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := maintenanceWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := exclusive.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err := runtime.AcquireLifecycle(ctx, root, recoveryWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	readers, writerPool := recoveryPools(t, dir)
	owner, err := sqliteowner.OpenIndexOwner(ctx, lease, readers, writerPool, recoveryWorktree)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, lease, recoveryJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	old := stageRecoveryFixture(t, ctx, store, writer, recoveryOld)
	authorizeGeneration := func(context.Context, string, string, string, int64) error { return nil }
	if err := store.PublishCapturedOnly(ctx, index.PublishCapturedRequest{Capture: old, AuthorizeGeneration: authorizeGeneration, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecoveryBuildingProcessHelper$")
	cmd.Env = append(os.Environ(), "MCE4D_TEST_RECOVERY_DIR="+dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	lines := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); lines <- line }()
	select {
	case line := <-lines:
		if line != "BUILDING_READY\n" {
			t.Fatalf("helper did not stage: %q", line)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := runtime.AcquireRecoveryWriter(ctx, lease, recoveryRun, 120*time.Millisecond); !errors.Is(err, runtime.ErrWorktreeBusy) {
		t.Fatalf("recovery stole live writer: %v", err)
	}
	request := index.PublishedFileRequest{ProfileID: old.Snapshot.Manifest().ProfileID, Path: "main.go", Scanner: old.Scanner, MaxManifestBytes: 8192, MaxFileBytes: 1024, AuthorizeGeneration: authorizeGeneration, AuthorizePath: old.Authorize}
	file, err := store.ReadPublishedFile(ctx, request)
	if err != nil || file.GenerationID != recoveryOld || string(file.Bytes) != "package main\n" {
		t.Fatalf("old source unavailable with live writer: %+v %v", file, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("helper unexpectedly succeeded")
	}
	recovery, err := runtime.AcquireRecoveryWriter(ctx, lease, recoveryRun, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	authorize := func(context.Context, string, ports.RecoveryCandidate) error { return nil }
	report, err := RecoverAbandoned(ctx, RecoveryRequest{RecoveryID: recoveryRun, Lease: recovery, Store: store, Authorize: authorize, BatchSize: 2, MaxCandidates: 4})
	if err != nil || report.Failed != 1 || report.Pending != nil || report.MayHaveMore {
		t.Fatalf("recovery failed: %+v %v", report, err)
	}
	file, err = store.ReadPublishedFile(ctx, request)
	if err != nil || file.GenerationID != recoveryOld || string(file.Bytes) != "package main\n" {
		t.Fatalf("old source lost: %+v %v", file, err)
	}
	var state string
	if err := writerPool.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, recoveryCandidate).Scan(&state); err != nil || state != "FAILED" {
		t.Fatalf("candidate state %q: %v", state, err)
	}
}
