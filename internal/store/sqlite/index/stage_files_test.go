package index

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func stageFixture(t *testing.T) (*Store, *sql.DB, StageFilesRequest) {
	t.Helper()
	store, db, begin := beginFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	return store, db, StageFilesRequest{Capture: CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, PolicyEpoch: begin.PolicyEpoch, Snapshot: begin.Snapshot, Writer: begin.Writer, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 1024, Authorize: func(context.Context, string, string, string, int64) error { return nil }}, Files: []FileClassification{{Path: "main.go", Kind: "go", Generated: true}}}
}

func TestStageFilesBindsCaptureAndRejectsReplay(t *testing.T) {
	store, db, request := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, request); err != nil {
		t.Fatal(err)
	}
	var kind, status, hash string
	var generated int
	if err := db.QueryRow(`SELECT kind,status,content_hash,generated FROM files WHERE generation_id=? AND path=?`, fixtureGeneration, "main.go").Scan(&kind, &status, &hash, &generated); err != nil {
		t.Fatal(err)
	}
	file, _ := request.Capture.Snapshot.File("main.go")
	if kind != "go" || status != "indexed" || generated != 1 || hash != file.ContentHash() || blobCount(t, db) != 1 {
		t.Fatalf("stage mismatch: %s %s %s %d", kind, status, hash, generated)
	}
	if err := store.StageFiles(ctx, request); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("stage replay accepted: %v", err)
	}
	if blobCount(t, db) != 1 {
		t.Fatal("replay changed blob count")
	}
}

func TestStageFilesRollsBackWhenPathAuthorityIsRevoked(t *testing.T) {
	store, db, request := stageFixture(t)
	calls := 0
	request.Capture.Authorize = func(context.Context, string, string, string, int64) error {
		calls++
		if calls == 2 {
			return errFixtureLease
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.StageFiles(ctx, request); !errors.Is(err, errFixtureLease) {
		t.Fatalf("revocation accepted: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM files`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 || blobCount(t, db) != 0 {
		t.Fatal("revoked batch left rows")
	}
}

func TestStageFilesRejectsMismatchedCaptureAndBatch(t *testing.T) {
	store, db, request := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request.Files[0].Path = "different.go"
	if err := store.StageFiles(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("wrong manifest path accepted: %v", err)
	}
	request.Files[0].Path = "main.go"
	request.Files[0].Kind = "Invalid Kind"
	if err := store.StageFiles(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("invalid kind accepted: %v", err)
	}
	request.Files[0].Kind = "go"
	other, err := snapshot.NewFile("main.go", []byte("different content\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest := request.Capture.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, length := other.ContentHash(), int64(other.Len())
	manifest.Files[0].ContentHash = &hash
	manifest.Files[0].ByteLength = &length
	request.Capture.Snapshot, err = snapshot.Freeze(manifest, []snapshot.File{other}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StageFiles(ctx, request); !errors.Is(err, ErrCaptureMismatch) {
		t.Fatalf("different snapshot accepted: %v", err)
	}
	if blobCount(t, db) != 0 {
		t.Fatal("invalid stage wrote a blob")
	}
}

func TestStageFilesSupportsBoundedManifestBatches(t *testing.T) {
	store, db, begin := beginFixture(t)
	a, err := snapshot.NewFile("a.go", []byte("a\n"), 100)
	if err != nil {
		t.Fatal(err)
	}
	b, err := snapshot.NewFile("b.go", []byte("b\n"), 100)
	if err != nil {
		t.Fatal(err)
	}
	ah, al, bh, bl := a.ContentHash(), int64(a.Len()), b.ContentHash(), int64(b.Len())
	reason := "FILE_MISSING"
	manifest := begin.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	manifest.Files = []model.ManifestFile{{Path: "a.go", Status: "captured", ContentHash: &ah, ByteLength: &al}, {Path: "b.go", Status: "captured", ContentHash: &bh, ByteLength: &bl}, {Path: "gone.go", Status: "missing", ReasonCode: &reason}}
	begin.Snapshot, err = snapshot.Freeze(manifest, []snapshot.File{a, b}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	request := StageFilesRequest{Capture: CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, PolicyEpoch: 1, Snapshot: begin.Snapshot, Writer: begin.Writer, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 1024, Authorize: func(context.Context, string, string, string, int64) error { return nil }}, Files: []FileClassification{{Path: "a.go", Kind: "go"}, {Path: "b.go", Kind: "go"}}}
	if err := store.StageFiles(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.Offset = 2
	request.Files = []FileClassification{{Path: "gone.go", Kind: "go"}}
	if err := store.StageFiles(ctx, request); err != nil {
		t.Fatal(err)
	}
	var status, gotReason string
	if err := db.QueryRow(`SELECT status,reason_code FROM files WHERE path='gone.go'`).Scan(&status, &gotReason); err != nil {
		t.Fatal(err)
	}
	if status != "missing" || gotReason != reason || blobCount(t, db) != 2 {
		t.Fatalf("missing coverage lost: %s %s", status, gotReason)
	}
	request.Files = make([]FileClassification, 257)
	if err := store.StageFiles(ctx, request); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("oversized batch accepted: %v", err)
	}
}

func TestStageFilesRejectsSecretBeforePersistence(t *testing.T) {
	store, db, begin := beginFixture(t)
	file, err := snapshot.NewFile("main.go", []byte("api_key = abcdefghijklmnopqrstuvwxyz\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	manifest := begin.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	hash, length := file.ContentHash(), int64(file.Len())
	manifest.Files[0].ContentHash = &hash
	manifest.Files[0].ByteLength = &length
	begin.Snapshot, err = snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.BeginGeneration(ctx, begin); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	request := StageFilesRequest{Capture: CaptureWrite{GenerationID: begin.GenerationID, JobID: begin.JobID, PolicyEpoch: 1, Snapshot: begin.Snapshot, Writer: begin.Writer, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 1024, Authorize: func(context.Context, string, string, string, int64) error { return nil }}, Files: []FileClassification{{Path: "main.go", Kind: "go"}}}
	if err := store.StageFiles(ctx, request); !errors.Is(err, policy.ErrSecretBlocked) {
		t.Fatalf("secret accepted: %v", err)
	}
	if blobCount(t, db) != 0 {
		t.Fatal("secret was persisted")
	}
}
