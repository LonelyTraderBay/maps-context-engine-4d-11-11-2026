package index

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

// The helper operates only on a parent-created synthetic database and source.
func TestPublishCrashHelper(t *testing.T) {
	path := os.Getenv("MCE4D_TEST_CRASH_DB")
	if path == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("MCE4D_TEST_CRASH_MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest model.CaptureManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	file, err := snapshot.NewFile("main.go", []byte("package main\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, db, db, fixtureWorktree, fixtureLease(ports.SharedLease))
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	stage := StageFilesRequest{Capture: CaptureWrite{GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: captured, Writer: &writerProbe{}, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 1024, Authorize: func(context.Context, string, string, string, int64) error { return nil }}}
	request := publishRequest(stage)
	phase := os.Getenv("MCE4D_TEST_CRASH_PHASE")
	pause := func() {
		fmt.Fprintln(os.Stdout, "READY_TO_KILL")
		<-ctx.Done()
	}
	request.Revalidate = func(context.Context, *snapshot.Snapshot) error {
		if phase == "before_transaction" {
			pause()
		}
		return nil
	}
	checks := 0
	request.AuthorizeGeneration = func(context.Context, string, string, string, int64) error {
		checks++
		if phase == "before_commit" && checks == 3 {
			pause()
		}
		return nil
	}
	if err := store.PublishCapturedOnly(ctx, request); err != nil {
		t.Fatal(err)
	}
	if phase == "after_commit" {
		pause()
	}
	t.Fatal("expected parent to terminate helper")
}

func TestPublicationSurvivesProcessTermination(t *testing.T) {
	for _, phase := range []string{"before_transaction", "before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			store, db, stage := stageFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := store.StageFiles(ctx, stage); err != nil {
				t.Fatal(err)
			}
			const old = "66666666-6666-4666-8666-666666666666"
			manifest := stage.Capture.Snapshot.Manifest()
			if _, err := db.Exec(`INSERT INTO generations(generation_id,profile_id,snapshot_digest,state,owner_job_id,policy_digest,policy_epoch,provider_fingerprint,created_at) VALUES(?,?,?,'BUILDING',?,?,1,?,'2026-01-01T00:00:00Z')`, old, manifest.ProfileID, manifest.SnapshotDigest, fixtureJob, manifest.PolicyDigest, manifest.ProviderDigest); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO files(generation_id,path,content_hash,kind,status,byte_length,generated,reason_code) SELECT ?,path,content_hash,kind,status,byte_length,generated,reason_code FROM files WHERE generation_id=?`, old, fixtureGeneration); err != nil {
				t.Fatal(err)
			}
			publishFixture(t, db, old, manifest, true)
			var sequence int
			var name, path string
			if err := db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublishCrashHelper$")
			cmd.Env = append(os.Environ(), "MCE4D_TEST_CRASH_DB="+path, "MCE4D_TEST_CRASH_PHASE="+phase, "MCE4D_TEST_CRASH_MANIFEST="+base64.StdEncoding.EncodeToString(raw))
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
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
				if line != "READY_TO_KILL\n" {
					t.Fatalf("helper did not reach %s: %q %s", phase, line, stderr.String())
				}
			case <-ctx.Done():
				t.Fatalf("helper timed out at %s", phase)
			}
			wantActive := old
			if phase == "after_commit" {
				wantActive = fixtureGeneration
			}
			file, err := store.ReadPublishedFile(ctx, readFixtureRequest(t, stage))
			if err != nil || file.GenerationID != wantActive || string(file.Bytes) != "package main\n" {
				t.Fatalf("reader saw torn snapshot: %+v %v", file, err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("helper unexpectedly exited successfully")
			}
			outcome, err := store.PublicationOutcomeByJob(ctx, PublicationOutcomeRequest{GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: stage.Capture.Snapshot, MaxManifestBytes: 8192, Authorize: func(context.Context, string, string, string, int64) error { return nil }})
			wantOutcome := "building"
			if phase == "after_commit" {
				wantOutcome = "published_active"
			}
			if err != nil || outcome.Status != wantOutcome {
				t.Fatalf("bad job outcome %+v: %v", outcome, err)
			}
			file, err = store.ReadPublishedFile(ctx, readFixtureRequest(t, stage))
			if err != nil || file.GenerationID != wantActive {
				t.Fatalf("active snapshot changed after termination: %+v %v", file, err)
			}
			var integrity string
			if err := db.QueryRow(`PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
				t.Fatalf("database integrity: %q %v", integrity, err)
			}
			if err := checkForeignKeys(ctx, db); err != nil {
				t.Fatal(err)
			}
		})
	}
}
