//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

type checkpointFixture struct {
	ctx       context.Context
	root      *paths.Root
	protected *paths.Root
	lease     *runtime.Lifecycle
	state     string
	target    string
}

func newCheckpointFixture(t *testing.T, mode ports.LeaseMode) checkpointFixture {
	t.Helper()
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA journal_mode=WAL; CREATE TABLE fixture(n INTEGER NOT NULL); INSERT INTO fixture VALUES(7)"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(state, "catalog.sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := paths.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	lease, err := runtime.AcquireLifecycle(ctx, root, "catalog", mode, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(); _ = protected.Close(); _ = root.Close(); cancel() })
	return checkpointFixture{ctx, root, protected, lease, state, target}
}

func (f checkpointFixture) open(authorize func(context.Context) error) (*CheckpointedSnapshot, error) {
	return OpenCheckpointedSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 1<<20, authorize, f.proof())
}

// The test fixture supplies a digest recorded after its writer closed; this
// is not a production checkpoint authority or evidence of crash recovery.
func (f checkpointFixture) proof() func(context.Context, [sha256.Size]byte) error {
	data, readErr := os.ReadFile(filepath.Join(f.state, "catalog.sqlite"))
	expected := sha256.Sum256(data)
	return func(_ context.Context, digest [sha256.Size]byte) error {
		if readErr != nil || digest != expected {
			return errors.New("checkpoint proof mismatch")
		}
		return nil
	}
}

func TestCheckpointedSnapshotReadOnlyAndLifecycle(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	database := filepath.Join(f.state, "catalog.sqlite")
	before, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.open(allowState)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snapshot.Close() })
	for pragma, want := range map[string]int{"query_only": 1, "foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2, "temp_store": 2} {
		var got int
		if err := snapshot.db.QueryRowContext(f.ctx, "PRAGMA "+pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("connection setting %s = %d, want %d: %v", pragma, got, want, err)
		}
	}
	if err := f.lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("snapshot did not hold exclusive lifecycle: %v", err)
	}
	var retained *SnapshotQuery
	var retainedRows *sql.Rows
	outside := filepath.Join(f.target, "created.sqlite")
	err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		retained = q
		for _, statement := range []string{
			"INSERT INTO fixture VALUES(8)",
			"PRAGMA query_only=OFF",
			"ATTACH DATABASE 'file:" + outside + "?mode=rwc&vfs=unix' AS other",
			"SELECT randomblob(4)",
		} {
			if rows, err := q.Query(statement); err == nil {
				_ = rows.Close()
				t.Errorf("unsafe SQL was accepted: %s", statement)
			}
		}
		var queryErr error
		retainedRows, queryErr = q.Query("SELECT n FROM fixture WHERE n=?", 7)
		if queryErr != nil {
			return queryErr
		}
		if !retainedRows.Next() {
			return errors.New("fixture row missing")
		}
		var n int
		if err := retainedRows.Scan(&n); err != nil || n != 7 {
			return errors.New("fixture row mismatch")
		}
		return nil // The wrapper must close an unclosed row set.
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = retained.Query("SELECT n FROM fixture"); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("query escaped callback: %v", err)
	}
	if retainedRows.Next() {
		t.Fatal("rows escaped callback")
	}
	if _, err = os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ATTACH created an outside file: %v", err)
	}
	after, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read changed source database: %v", err)
	}
	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.lease.Close(); err != nil {
		t.Fatalf("snapshot did not release lifecycle: %v", err)
	}
	if err = snapshot.Read(f.ctx, func(*SnapshotQuery) error { return nil }); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("closed snapshot accepted read: %v", err)
	}
}

func TestCheckpointedSnapshotRejectsNonWALWithoutChangingIt(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	database := filepath.Join(f.state, "catalog.sqlite")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	if err = db.QueryRowContext(f.ctx, "PRAGMA journal_mode=DELETE").Scan(&mode); err != nil || mode != "delete" {
		t.Fatalf("fixture rollback mode: %s %v", mode, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := f.open(allowState); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("non-WAL database accepted: %v", err)
	}
	after, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected database was modified: %v", err)
	}
}

func TestCheckpointedSnapshotValidatesReplacementConnections(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	snapshot, err := f.open(allowState)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	snapshot.db.SetMaxIdleConns(0) // Each Read must initialize a new physical connection.
	for i := 0; i < 2; i++ {
		err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
			rows, err := q.Query("SELECT n FROM fixture WHERE n=7")
			if err != nil {
				return err
			}
			if !rows.Next() {
				return errors.New("fixture row missing")
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("replacement connection %d: %v", i, err)
		}
	}
}

func TestCheckpointedSnapshotRejectsEverySidecar(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			f := newCheckpointFixture(t, ports.ExclusiveLease)
			if err := os.WriteFile(filepath.Join(f.state, "catalog.sqlite"+suffix), nil, 0600); err != nil {
				t.Fatal(err)
			}
			snapshot, err := f.open(allowState)
			if snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
				t.Fatalf("sidecar accepted: %v", err)
			}
			if err = f.lease.Close(); err != nil {
				t.Fatalf("failed open retained lifecycle: %v", err)
			}
		})
	}
}

func TestCheckpointedSnapshotRejectsLiveWAL(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	writer, err := sql.Open("sqlite", filepath.Join(f.state, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.ExecContext(f.ctx, "PRAGMA wal_autocheckpoint=0; INSERT INTO fixture VALUES(8)"); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := f.open(allowState); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("live WAL accepted: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.open(allowState)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var found bool
	err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		rows, err := q.Query("SELECT n FROM fixture WHERE n=8")
		if err != nil {
			return err
		}
		found = rows.Next()
		return rows.Err()
	})
	if err != nil || !found {
		t.Fatalf("checkpointed row missing: found=%t err=%v", found, err)
	}
}

func TestCheckpointedSnapshotRejectsSharedLeaseAndLimits(t *testing.T) {
	f := newCheckpointFixture(t, ports.SharedLease)
	if snapshot, err := f.open(allowState); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("shared lease accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if snapshot, err := OpenCheckpointedSnapshot(ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 1<<20, allowState, f.proof()); snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if snapshot, err := OpenCheckpointedSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", checkpointedSnapshotMaxBytes+1, allowState, f.proof()); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("unsupported size accepted: %v", err)
	}
}

func TestCheckpointedSnapshotRejectsWrongRootAndOversizedSource(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	other := newCheckpointFixture(t, ports.ExclusiveLease)
	if snapshot, err := OpenCheckpointedSnapshot(f.ctx, other.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 1<<20, allowState, other.proof()); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("lease from a different state root accepted: %v", err)
	}
	if snapshot, err := OpenCheckpointedSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 64, allowState, f.proof()); snapshot != nil || !errors.Is(err, ErrStateLimit) {
		t.Fatalf("oversized source accepted: %v", err)
	}
	if snapshot, err := OpenCheckpointedSnapshot(f.ctx, f.root, []*paths.Root{f.root}, f.lease, "catalog", "catalog.sqlite", 1<<20, allowState, f.proof()); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("protected root accepted as state: %v", err)
	}
}

func TestCheckpointedSnapshotRequiresIndependentProof(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	if snapshot, err := OpenCheckpointedSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 1<<20, allowState, nil); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("missing checkpoint proof accepted: %v", err)
	}
	wrong := func(context.Context, [sha256.Size]byte) error { return errors.New("untrusted checkpoint") }
	if snapshot, err := OpenCheckpointedSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 1<<20, allowState, wrong); snapshot != nil || !errors.Is(err, ErrStateUnsafe) {
		t.Fatalf("untrusted checkpoint proof accepted: %v", err)
	}
	if err := f.lease.Close(); err != nil {
		t.Fatalf("failed proof leaked lifecycle: %v", err)
	}
}

func TestCheckpointedSnapshotCancellationAndQueryLimit(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	snapshot, err := f.open(allowState)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	if err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
		for i := 0; i < 64; i++ {
			rows, err := q.Query("SELECT n FROM fixture")
			if err != nil {
				return err
			}
			if err = rows.Close(); err != nil {
				return err
			}
		}
		if rows, err := q.Query("SELECT n FROM fixture"); rows != nil || !errors.Is(err, ErrStateLimit) {
			return errors.New("query limit not enforced")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	err = snapshot.Read(ctx, func(*SnapshotQuery) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during callback was lost: %v", err)
	}
	other, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = other.Exec("CREATE TABLE unaffected(n INTEGER); INSERT INTO unaffected VALUES(1)"); err != nil {
		t.Fatalf("snapshot authorizer affected unrelated SQLite connection: %v", err)
	}
}

func TestCheckpointedSnapshotInvalidDatabaseReleasesLease(t *testing.T) {
	f := newCheckpointFixture(t, ports.ExclusiveLease)
	if err := os.WriteFile(filepath.Join(f.state, "catalog.sqlite"), []byte("invalid database fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := f.open(allowState); snapshot != nil || err == nil {
		t.Fatalf("invalid SQLite database accepted: %v", err)
	}
	if err := f.lease.Close(); err != nil {
		t.Fatalf("failed open leaked lifecycle: %v", err)
	}
}

func TestCheckpointedSnapshotDetectsChangeAndRevocation(t *testing.T) {
	for _, change := range []string{"content", "sidecar", "hardlink", "root", "authority", "checkpoint_proof", "read_permission"} {
		t.Run(change, func(t *testing.T) {
			if change == "read_permission" && os.Geteuid() == 0 {
				t.Skip("root can bypass ordinary file read permissions")
			}
			f := newCheckpointFixture(t, ports.ExclusiveLease)
			authorized := true
			proofValid := true
			authorize := func(context.Context) error {
				if !authorized {
					return errors.New("revoked")
				}
				return nil
			}
			baseProof := f.proof()
			proof := func(ctx context.Context, digest [sha256.Size]byte) error {
				if !proofValid {
					return errors.New("checkpoint proof revoked")
				}
				return baseProof(ctx, digest)
			}
			snapshot, err := OpenCheckpointedSnapshot(f.ctx, f.root, []*paths.Root{f.protected}, f.lease, "catalog", "catalog.sqlite", 1<<20, authorize, proof)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			database := filepath.Join(f.state, "catalog.sqlite")
			err = snapshot.Read(f.ctx, func(q *SnapshotQuery) error {
				rows, err := q.Query("SELECT n FROM fixture")
				if err != nil || !rows.Next() {
					return errors.New("positive read failed")
				}
				switch change {
				case "content":
					file, err := os.OpenFile(database, os.O_WRONLY, 0)
					if err != nil {
						return err
					}
					_, err = file.WriteAt([]byte{0xff}, 100)
					return errors.Join(err, file.Close())
				case "sidecar":
					return os.WriteFile(database+"-wal", nil, 0600)
				case "hardlink":
					return os.Link(database, filepath.Join(f.target, "alias"))
				case "root":
					return os.Rename(f.state, f.state+"-moved")
				case "authority":
					authorized = false
				case "checkpoint_proof":
					proofValid = false
				case "read_permission":
					return os.Chmod(database, 0000)
				}
				return nil
			})
			if err == nil {
				t.Fatal("changed source or revoked authority was accepted")
			}
		})
	}
}
