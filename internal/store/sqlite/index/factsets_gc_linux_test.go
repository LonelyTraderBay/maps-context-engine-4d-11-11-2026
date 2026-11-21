package index

import (
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

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

func TestC076FactGCRecovery(t *testing.T) {
	for _, phase := range []string{"before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			_, _, db, request, stage := gcStoreFixture(t, false)
			before := gcData(t, db)
			var path string
			if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestC076GCProcess$")
			cmd.Env = append(os.Environ(), "MCE4D_C076_DB="+path, "MCE4D_C076_PHASE="+phase, "MCE4D_C076_PROFILE="+request.ProfileID)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 79 || string(output) != "C076_GC_BOUNDARY "+phase+"\n" {
				t.Fatalf("GC process did not reach the expected boundary: %q, %v", output, err)
			}
			db = openC076FixtureDB(t, path)
			store, err := Open(ctx, db, db, fixtureWorktree, fixtureLease(ports.SharedLease))
			if err != nil {
				t.Fatal(err)
			}
			if phase == "before_commit" && gcData(t, db) != before {
				t.Fatal("interrupted transaction changed retained facts")
			}
			wantGenerations, wantBlobs := 3, 2
			if phase == "after_commit" {
				wantGenerations, wantBlobs = 2, 1
			}
			if generationCount(t, db) != wantGenerations || blobCount(t, db) != wantBlobs {
				t.Fatal("recovering GC lost referenced data or retained removed rows")
			}
			var pinnedOther string
			if err := db.QueryRow(`SELECT generation_id FROM active_generations WHERE profile_id=?`, strings.Repeat("d", 64)).Scan(&pinnedOther); err != nil || pinnedOther != gcOther {
				t.Fatalf("other profile lost its active generation: %q, %v", pinnedOther, err)
			}
			file, err := store.ReadPublishedFile(ctx, readFixtureRequest(t, stage))
			if err != nil || file.GenerationID != gcActive || string(file.Bytes) != "package main\n" {
				t.Fatalf("active snapshot lost shared source: %v", err)
			}
			if err := checkForeignKeys(ctx, db); err != nil {
				t.Fatal(err)
			}
			outcome, err := store.CollectGeneration(ctx, request, fixtureGeneration, true)
			if err != nil || (phase == "before_commit" && !outcome.Applied) || (phase == "after_commit" && !outcome.AlreadyAbsent) {
				t.Fatalf("GC retry did not reconcile: %+v, %v", outcome, err)
			}
		})
	}
}

// The child uses only the parent-created Go test fixture. Exit skips every
// defer in the child to exercise SQLite recovery across a process boundary.
func TestC076GCProcess(t *testing.T) {
	path := os.Getenv("MCE4D_C076_DB")
	if path == "" {
		return
	}
	rel, err := filepath.Rel(os.TempDir(), path)
	if err != nil || !strings.HasPrefix(rel, "TestC076FactGCRecovery") {
		t.Fatal("GC helper requires its synthetic test directory")
	}
	phase := os.Getenv("MCE4D_C076_PHASE")
	if phase != "before_commit" && phase != "after_commit" {
		t.Fatal("unknown fixture phase")
	}
	db := openC076FixtureDB(t, path)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	store, err := Open(ctx, db, db, fixtureWorktree, fixtureLease(ports.SharedLease))
	if err != nil {
		t.Fatal(err)
	}
	interrupt := func() {
		fmt.Fprintln(os.Stdout, "C076_GC_BOUNDARY", phase)
		os.Exit(79)
	}
	checks := 0
	request := GCRequest{ProfileID: os.Getenv("MCE4D_C076_PROFILE"), JobID: fixtureJob, KeepGenerations: 1,
		Writer: &writerProbe{}, Authorize: func(context.Context, string, string) error {
			checks++
			// The preceding checks are before the transaction and after
			// retiring the old generation. The third is after deleting relations.
			if phase == "before_commit" && checks == 3 {
				interrupt()
			}
			return nil
		}}
	result, err := store.CollectGeneration(ctx, request, fixtureGeneration, true)
	if err != nil || !result.Applied || phase != "after_commit" {
		t.Fatalf("GC fixture did not commit: %+v, %v", result, err)
	}
	interrupt()
}

func openC076FixtureDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("GC fixture database unavailable: %v", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
