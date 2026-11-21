package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func testPackagedWindowsInterruptedIndex(t *testing.T, ctx context.Context, bin, root, worktree, profile string) {
	t.Helper()
	run := func(binary, input string, want int, environment []string, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(bin, binary), args...)
		cmd.Env = append(os.Environ(), "MCE4D_HOME="+root, "MCE4D_WINDOWS_ROLE_IPC=1")
		cmd.Env = append(cmd.Env, environment...)
		cmd.Stdin = strings.NewReader(input)
		raw, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if want == 0 && err == nil || want != 0 && errors.As(err, &exit) && exit.ExitCode() == want {
			return raw
		}
		t.Fatalf("%s %q expected exit %d: %v %s", binary, args, want, err, raw)
		return nil
	}
	cli := func(input string, want int, args ...string) []byte {
		t.Helper()
		return run("mce4d.exe", input, want, nil, args...)
	}
	query := func(operation, input string, want int) []byte {
		t.Helper()
		return cli(input, want, "--worktree-id", worktree, "--profile-id", profile, "query", operation, "--input-json", "-")
	}
	member := func() model.ScopeMember {
		t.Helper()
		var result struct {
			Scope model.ScopeVector `json:"scope"`
		}
		raw := query("status", "{}", 0)
		if err := json.Unmarshal(raw, &result); err != nil || len(result.Scope.Members) != 1 {
			t.Fatalf("expected published scope: %s %v", raw, err)
		}
		return result.Scope.Members[0]
	}
	slot := filepath.Join(root, "control", "worktrees", worktree)
	fileHash := func(rel string) [32]byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(slot, rel))
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(raw)
	}
	records := []string{"records-wal/records.sqlite", "records-wal/wal-authority.log", "records-control/wal-head.log"}
	unchanged := map[string][32]byte{}
	for _, path := range records {
		unchanged[path] = fileHash(path)
	}
	indexName := filepath.Join(slot, "index-wal", "index.sqlite")
	uri := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(indexName), RawQuery: "mode=ro&immutable=1"}
	indexVersion := func() int {
		t.Helper()
		db, err := sql.Open("sqlite", uri.String())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var version int
		if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			t.Fatal(err)
		}
		return version
	}
	recoverBase := []string{"store", "recover", "--target", "worktree", "--worktree-id", worktree}
	report := append(append([]string{}, recoverBase...), "--report-only")
	apply := append(append([]string{}, recoverBase...), "--apply", "--confirm", "--wait-ms", "4000")
	start := indexVersion()
	last := member()
	env := []string{"MCE4D_WINDOWS_INDEX_TEST_ROOT=" + root, "MCE4D_WINDOWS_INDEX_TEST_WORKTREE=" + worktree}
	for _, boundary := range []string{"direct apply", "torn receipt", "PREPARED", "retired-wal", "index_main", "VERIFYING"} {
		var page struct {
			Pagination model.Pagination `json:"pagination"`
		}
		raw := query("locate", `{"mode":"file","query":"marker","limit":1}`, 6)
		if err := json.Unmarshal(raw, &page); err != nil || page.Pagination.NextCursor == nil {
			t.Fatalf("index recovery did not get a product-issued cursor: %v", err)
		}
		cursor := fmt.Sprintf(`{"mode":"file","query":"marker","limit":1,"cursor":%q}`, *page.Pagination.NextCursor)
		workerEnv := append([]string{}, env...)
		if boundary == "direct apply" {
			workerEnv = append(workerEnv, "MCE4D_WINDOWS_INDEX_TEST_CHECKPOINT=1")
		}
		if boundary == "torn receipt" {
			workerEnv = append(workerEnv, "MCE4D_WINDOWS_INDEX_TEST_TORN_RECEIPT=1")
		}
		run("journal.test.exe", "", 79, workerEnv, "-test.run=^TestWindowsInterruptedIndexWorker$")
		sidecar := "index-wal/index.sqlite-wal"
		retained := fileHash(sidecar)
		if boundary == "direct apply" && indexVersion() != 42 {
			t.Fatal("fixture did not stop after SQLite checkpoint changed the live main file")
		}
		if boundary == "torn receipt" {
			run("state-profile.exe", "", 3, nil, "check-pools", "--root", root, "--worktree-id", worktree, "--profile-id", profile)
		} else {
			if got := member(); got != last {
				t.Fatal("reader exposed the interrupted index operation before confirmed recovery")
			}
			query("locate", cursor, 0)
		}
		var interrupted sqlite.StoreRecoveryReport
		if boundary != "direct apply" && boundary != "torn receipt" {
			run("journal.test.exe", "", 79, append(append([]string{}, env...), "MCE4D_WINDOWS_INDEX_TEST_RECOVERY_BOUNDARY="+boundary),
				"-test.run=^TestWindowsIndexRollbackCrashWorker$")
			raw := cli("", 0, report...)
			phase := boundary
			if boundary == "retired-wal" || boundary == "index_main" {
				phase = "REPLACING"
			}
			if err := json.Unmarshal(raw, &interrupted); err != nil || interrupted.Phase != phase || !interrupted.ReportOnly ||
				interrupted.ActualStateVerified || !model.ValidUUID(interrupted.OperationID) {
				t.Fatalf("recovery journal after %s: %s %v", boundary, raw, err)
			}
			if again := cli("", 0, report...); !bytes.Equal(raw, again) {
				t.Fatal("report-only modified the recovery plan")
			}
			run("state-profile.exe", "", 3, nil, "check-pools", "--root", root, "--worktree-id", worktree, "--profile-id", profile)
		}
		var restored sqlite.ProtectedRestoreSummary
		raw = cli("", 0, apply...)
		if err := json.Unmarshal(raw, &restored); err != nil || restored.Outcome != "restored" ||
			(interrupted.OperationID != "" && restored.OperationID != interrupted.OperationID) || len(restored.CompletedSteps) != 4 {
			t.Fatalf("index recovery after %s: %s %v", boundary, raw, err)
		}
		if again := cli("", 0, apply...); !bytes.Equal(raw, again) {
			t.Fatal("repeat recovery changed the verified outcome")
		}
		if fileHash("index-wal/retired-"+restored.OperationID+"-wal") != retained {
			t.Fatal("retired WAL bytes changed")
		}
		if _, err := os.Stat(filepath.Join(slot, sidecar)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale WAL remains at the live name: %v", err)
		}
		for name, digest := range unchanged {
			if fileHash(name) != digest {
				t.Fatalf("index recovery modified the records store: %s", name)
			}
		}
		now := member()
		if now.GenerationID != last.GenerationID || now.SnapshotDigest != last.SnapshotDigest || now.RecordsIncarnation != last.RecordsIncarnation ||
			now.IndexIncarnation == last.IndexIncarnation || indexVersion() != start {
			t.Fatal("recovery did not restore the last acknowledged index and rotate only its incarnation")
		}
		if failure := query("locate", cursor, 3); !bytes.Contains(failure, []byte("CURSOR_INVALID")) && !bytes.Contains(failure, []byte("STORE_REPLACED")) {
			t.Fatalf("old cursor returned unexpected outcome: %s", failure)
		}
		last = now
		t.Logf("Windows index recovered after %s with SQLite WAL retained, index incarnation rotated, records identical and old cursor refused", boundary)
	}
}
