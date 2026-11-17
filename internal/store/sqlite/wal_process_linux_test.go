//go:build linux

package sqlite

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	modsqlite "modernc.org/sqlite"
)

const walOracleScope = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const walOracleJob = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

// This is an independent SQLite/OS oracle on test-owned files, not a secure
// state opener. A candidate opener must pass the same cross-process behavior
// in addition to its own path, sidecar, ownership and lifecycle tests.
func TestWALCrossProcessSnapshotOracle(t *testing.T) {
	for _, tc := range []struct {
		vfs     string
		blocked bool
	}{{"unix", false}, {"unix-excl", true}} {
		t.Run(tc.vfs, func(t *testing.T) {
			runWALSnapshotOracle(t, tc.vfs, tc.blocked)
		})
	}
}

func runWALSnapshotOracle(t *testing.T, vfsName string, wantBlocked bool) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	marker := hex.EncodeToString(nonce[:])
	if err := os.WriteFile(filepath.Join(state, "wal-oracle.owner"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, root, walOracleScope, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	writerLease, err := runtime.AcquireWriter(ctx, lease, walOracleJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writerLease.Close()
	path := filepath.Join(state, "index.sqlite")
	db, err := sql.Open("sqlite", walOracleDSN(path, vfsName, "rwc"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	var mode string
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL setup failed: %v", err)
	}
	if _, err = db.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0; CREATE TABLE oracle(n INTEGER NOT NULL); INSERT INTO oracle VALUES(7)"); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWALOracleReaderProcess$", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "MCE4D_WAL_ORACLE_CHILD=1", "MCE4D_WAL_ORACLE_STATE="+state, "MCE4D_WAL_ORACLE_VFS="+vfsName, "MCE4D_WAL_ORACLE_NONCE="+marker)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	output := bufio.NewReaderSize(stdout, 256)
	first, err := output.ReadString('\n')
	if err != nil {
		t.Fatalf("reader process did not report readiness: %v", err)
	}
	if wantBlocked {
		if first != "BUSY\n" {
			t.Fatalf("exclusive VFS allowed another process: %q", first)
		}
		if err = cmd.Wait(); err != nil {
			t.Fatalf("blocked reader did not exit cleanly: %v", err)
		}
		return
	}
	if first != "READY\n" {
		t.Fatalf("normal WAL reader did not open: %q", first)
	}
	if err = writerLease.CheckWriter(ctx, walOracleScope, walOracleJob); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO oracle VALUES(8)"); err != nil {
		t.Fatalf("writer failed while reader held a snapshot: %v", err)
	}
	if _, err = fmt.Fprintln(stdin, "COMMITTED"); err != nil {
		t.Fatal(err)
	}
	second, err := output.ReadString('\n')
	if err != nil || second != "SNAPSHOT_OK\n" {
		t.Fatalf("reader snapshot isolation failed: %q %v", second, err)
	}
	if err = stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("reader process failed: %v", err)
	}
}

func walOracleDSN(path, vfsName, mode string) string {
	query := url.Values{"mode": {mode}, "vfs": {vfsName}}
	for _, pragma := range []string{"busy_timeout(250)", "foreign_keys(1)", "synchronous(FULL)"} {
		query.Add("_pragma", pragma)
	}
	if mode == "ro" {
		query.Add("_pragma", "query_only(1)")
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func TestWALOracleReaderRejectsUnownedFixture(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCE4D_WAL_ORACLE_STATE", state)
	t.Setenv("MCE4D_WAL_ORACLE_VFS", "unix")
	t.Setenv("MCE4D_WAL_ORACLE_NONCE", strings.Repeat("01", 16))
	if err := runWALOracleReader(); err == nil {
		t.Fatal("accepted fixture without ownership marker")
	}
	if err := os.WriteFile(filepath.Join(state, "wal-oracle.owner"), []byte(strings.Repeat("02", 16)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runWALOracleReader(); err == nil {
		t.Fatal("accepted fixture with wrong ownership marker")
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 1 || entries[0].Name() != "wal-oracle.owner" {
		t.Fatalf("unowned fixture was mutated: %v", err)
	}
}

// Runs only as a child of the oracle; inputs refer to the parent's TempDir.
func TestWALOracleReaderProcess(t *testing.T) {
	if os.Getenv("MCE4D_WAL_ORACLE_CHILD") != "1" {
		return
	}
	if err := runWALOracleReader(); err != nil {
		var sqliteErr *modsqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == 5 {
			fmt.Fprintln(os.Stdout, "BUSY")
			return
		}
		fmt.Fprintln(os.Stdout, "ERROR")
	}
}

func runWALOracleReader() error {
	state, vfsName := os.Getenv("MCE4D_WAL_ORACLE_STATE"), os.Getenv("MCE4D_WAL_ORACLE_VFS")
	nonce := os.Getenv("MCE4D_WAL_ORACLE_NONCE")
	decoded, decodeErr := hex.DecodeString(nonce)
	rel, relErr := filepath.Rel(os.TempDir(), state)
	if !filepath.IsAbs(state) || filepath.Base(state) != "state" || relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || decodeErr != nil || len(decoded) != 16 || (vfsName != "unix" && vfsName != "unix-excl") {
		return errors.New("invalid oracle fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root, err := paths.OpenRoot(state)
	if err != nil {
		return err
	}
	defer root.Close()
	marker, err := root.OpenRegular("wal-oracle.owner")
	if err != nil {
		return err
	}
	if stateOwner(marker, false) != nil {
		_ = marker.Close()
		return errors.New("invalid oracle marker")
	}
	contents, readErr := io.ReadAll(io.LimitReader(marker, 33))
	closeErr := marker.Close()
	if readErr != nil || closeErr != nil || string(contents) != nonce {
		return errors.New("invalid oracle marker")
	}
	lease, err := runtime.AcquireLifecycle(ctx, root, walOracleScope, ports.SharedLease, time.Second)
	if err != nil {
		return err
	}
	defer lease.Close()
	db, err := sql.Open("sqlite", walOracleDSN(filepath.Join(state, "index.sqlite"), vfsName, "ro"))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM oracle WHERE n=7").Scan(&before); err != nil {
		return err
	}
	if before != 1 {
		return errors.New("old row missing")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO oracle VALUES(9)"); err == nil {
		return errors.New("reader was allowed to write")
	}
	fmt.Fprintln(os.Stdout, "READY")
	line, err := bufio.NewReaderSize(os.Stdin, 256).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "COMMITTED" {
		return errors.New("writer did not commit")
	}
	var during int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM oracle WHERE n=8").Scan(&during); err != nil || during != 0 {
		return errors.New("reader snapshot changed before transaction ended")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var after int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM oracle WHERE n=8").Scan(&after); err != nil || after != 1 {
		return errors.New("reader did not see committed row")
	}
	if err = lease.CheckRoot(ctx, root, walOracleScope, ports.SharedLease); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "SNAPSHOT_OK")
	return nil
}
