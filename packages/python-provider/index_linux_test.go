//go:build linux

package pythonprovider

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"reflect"
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

func verifyPythonPublication(t *testing.T, ctx context.Context, job ports.ProviderJob, captured *snapshot.Snapshot, batches []model.ProviderBatch, done model.ProviderDone) {
	t.Helper()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	manifest := captured.Manifest()
	state := t.TempDir()
	root, err := paths.OpenRoot(state)
	check(err)
	defer root.Close()
	lease, err := runtime.AcquireLifecycle(ctx, root, manifest.WorktreeID, ports.ExclusiveLease, time.Second)
	check(err)
	defer lease.Close()
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(state, "index.sqlite")}).String()
	db, err := sql.Open("sqlite", dsn)
	check(err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = index.Initialize(ctx, db, manifest.WorktreeID, lease)
	check(err)
	writer, err := runtime.AcquireWriter(ctx, lease, job.Start.JobID, time.Second)
	check(err)
	defer writer.Close()
	store, err := index.Open(ctx, db, db, manifest.WorktreeID, lease)
	check(err)
	scanner, err := policy.NewScanner(65536)
	check(err)
	authorize := func(ctx context.Context, worktree, profile, policy string, epoch int64) error {
		if worktree != manifest.WorktreeID || profile != manifest.ProfileID || policy != manifest.PolicyDigest || epoch != 1 {
			return index.ErrCaptureMismatch
		}
		return job.Recheck(ctx)
	}
	var classes []index.FileClassification
	capturedPaths := map[string]bool{}
	for _, file := range manifest.Files {
		classes = append(classes, index.FileClassification{Path: file.Path, Kind: "text"})
		capturedPaths[file.Path] = true
	}
	authorizePath := func(ctx context.Context, worktree, path, policy string, epoch int64) error {
		if !capturedPaths[path] {
			return index.ErrCaptureMismatch
		}
		return authorize(ctx, worktree, manifest.ProfileID, policy, epoch)
	}
	stage := index.CaptureWrite{GenerationID: job.Start.GenerationID, JobID: job.Start.JobID, PolicyEpoch: 1,
		Snapshot: captured, Scanner: scanner, Authorize: authorizePath, Writer: writer, MaxManifestBytes: 65536, MaxSourceBytes: 65536}
	check(store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: stage.GenerationID, JobID: stage.JobID,
		PolicyEpoch: 1, Snapshot: captured, Writer: writer, Authorize: authorize, MaxManifestBytes: stage.MaxManifestBytes}))
	check(store.StageFiles(ctx, index.StageFilesRequest{Capture: stage, Files: classes}))
	stream, err := store.BeginProviderStage(ctx, stage, authorize)
	check(err)
	for _, batch := range batches {
		_, err := store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: stage, Stream: stream, Batch: batch, MaxFrameBytes: 65536})
		check(err)
	}
	check(store.SealProviderStage(ctx, index.SealProviderRequest{Capture: stage, Stream: stream, Done: done,
		MaxFrameBytes: 65536, AuthorizeGeneration: authorize}))
	check(store.PublishProvider(ctx, index.PublishProviderRequest{Capture: stage, Stream: stream, MaxFrameBytes: 65536, MaxFacts: 100,
		AuthorizeGeneration: authorize,
		Revalidate: func(ctx context.Context, fresh *snapshot.Snapshot) error {
			if fresh.Manifest().SnapshotDigest != manifest.SnapshotDigest {
				return index.ErrCaptureMismatch
			}
			return job.Recheck(ctx)
		},
		VerifyProvider: func(ctx context.Context, fresh *snapshot.Snapshot, terminal model.ProviderDone) error {
			if !reflect.DeepEqual(terminal, done) || terminal.VerifyInputDigest(fresh.Manifest().SnapshotDigest) != nil {
				return index.ErrCaptureMismatch
			}
			return job.Recheck(ctx)
		}}))
	check(store.ReleaseProviderStage(ctx, stage, stream, authorize))
	head, err := store.ReadGeneration(ctx, manifest.ProfileID, "", authorize, 65536)
	check(err)
	if head.State != "READY_PARTIAL" || head.GenerationID != stage.GenerationID {
		t.Fatalf("unexpected Python generation: %+v", head)
	}
	var typed, notebooks, relations int
	check(db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbols WHERE generation_id=? AND signature LIKE 'literal_expression_type:%'`, stage.GenerationID).Scan(&typed))
	check(db.QueryRowContext(ctx, `SELECT COUNT(*) FROM diagnostics WHERE generation_id=? AND code='PYTHON_NOTEBOOK_DECLARED' AND path='src/model.ipynb'`, stage.GenerationID).Scan(&notebooks))
	check(db.QueryRowContext(ctx, `SELECT COUNT(*) FROM relations WHERE generation_id=?`, stage.GenerationID).Scan(&relations))
	if typed != 2 || notebooks != 1 || relations != 0 {
		t.Fatalf("published evidence mismatch: typed=%d notebook=%d relations=%d", typed, notebooks, relations)
	}
}
