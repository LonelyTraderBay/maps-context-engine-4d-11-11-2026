package index

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
)

var errInjectedCommit = errors.New("injected unknown commit result")

type faultConnector struct {
	dsn  string
	mode *atomic.Int32
}

func (c faultConnector) Connect(context.Context) (driver.Conn, error) {
	inner, err := (&sqlite.Driver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &faultConn{Conn: inner, mode: c.mode}, nil
}
func (c faultConnector) Driver() driver.Driver { return &sqlite.Driver{} }

type faultConn struct {
	driver.Conn
	mode *atomic.Int32
}

func (c *faultConn) Begin() (driver.Tx, error) {
	inner, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return faultTx{Tx: inner, mode: c.mode}, nil
}

type faultTx struct {
	driver.Tx
	mode *atomic.Int32
}

func (tx faultTx) Commit() error {
	mode := tx.mode.Swap(0)
	if mode == 1 {
		return errors.Join(errInjectedCommit, tx.Tx.Rollback())
	}
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if mode == 2 {
		return errInjectedCommit
	}
	return nil
}

func TestPublishResolvesUnknownCommitOutcomeByJob(t *testing.T) {
	for _, test := range []struct {
		name           string
		mode           int32
		outcome, state string
	}{
		{"rolled_back", 1, "building", "BUILDING"},
		{"committed", 2, "published_active", "READY_PARTIAL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, db, stage := stageFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := store.StageFiles(ctx, stage); err != nil {
				t.Fatal(err)
			}
			old := insertPreviousActive(t, db, stage)
			var path string
			if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
				t.Fatal(err)
			}
			var mode atomic.Int32
			faultDB := sql.OpenDB(faultConnector{dsn: (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String(), mode: &mode})
			faultDB.SetMaxOpenConns(1)
			defer faultDB.Close()
			publisher, err := Open(ctx, db, faultDB, fixtureWorktree, store.lease)
			if err != nil {
				t.Fatal(err)
			}
			mode.Store(test.mode)
			err = publisher.PublishCapturedOnly(ctx, publishRequest(stage))
			if !errors.Is(err, ErrCommitUnknown) || !errors.Is(err, errInjectedCommit) {
				t.Fatalf("commit uncertainty lost: %v", err)
			}
			outcome, err := store.PublicationOutcomeByJob(ctx, PublicationOutcomeRequest{GenerationID: fixtureGeneration, JobID: fixtureJob, PolicyEpoch: 1, Snapshot: stage.Capture.Snapshot, MaxManifestBytes: 8192, Authorize: func(context.Context, string, string, string, int64) error { return nil }})
			if err != nil || outcome.Status != test.outcome {
				t.Fatalf("bad reconciled outcome %+v: %v", outcome, err)
			}
			var active, state string
			if err := db.QueryRow(`SELECT generation_id FROM active_generations`).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT state FROM generations WHERE generation_id=?`, fixtureGeneration).Scan(&state); err != nil {
				t.Fatal(err)
			}
			wantActive := old
			if test.mode == 2 {
				wantActive = fixtureGeneration
			}
			if active != wantActive || state != test.state {
				t.Fatalf("inconsistent outcome active=%s state=%s", active, state)
			}
		})
	}
}
