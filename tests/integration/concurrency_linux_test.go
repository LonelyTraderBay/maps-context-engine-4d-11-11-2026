//go:build linux

package integration

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/catalog"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const concurrencyJob = "11111111-1111-4111-8111-111111111111"
const concurrencyProvider = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type concurrencyStore struct {
	root  *paths.Root
	lease *runtime.Lifecycle
	owner *sqliteowner.IndexOwner
	store *index.Store
}

func concurrencyDB(t *testing.T, name string, limit int) *sql.DB {
	t.Helper()
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(name)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(limit)
	return db
}

func openConcurrencyStore(t *testing.T, ctx context.Context, dir, worktree string, initialize bool) concurrencyStore {
	t.Helper()
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if initialize {
		lease, err := runtime.AcquireLifecycle(ctx, root, worktree, ports.ExclusiveLease, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		writer, err := runtime.AcquireWriter(ctx, lease, concurrencyJob, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := sqliteowner.OpenMaintenanceOwner(ctx, lease, concurrencyDB(t, filepath.Join(dir, "index.sqlite"), 1), worktree)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.InitializeIndex(ctx, writer, concurrencyJob); err != nil {
			t.Fatal(err)
		}
		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}
	lease, err := runtime.AcquireLifecycle(ctx, root, worktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := sqliteowner.OpenIndexOwner(ctx, lease, concurrencyDB(t, filepath.Join(dir, "index.sqlite"), 4), concurrencyDB(t, filepath.Join(dir, "index.sqlite"), 1), worktree)
	if err != nil {
		t.Fatal(err)
	}
	store, err := owner.Store()
	if err != nil {
		t.Fatal(err)
	}
	return concurrencyStore{root: root, lease: lease, owner: owner, store: store}
}

func (s concurrencyStore) close() error {
	return errors.Join(s.owner.Close(), s.lease.Close(), s.root.Close())
}

func concurrencySnapshot(t *testing.T, worktree, profile, policyDigest, name string) *snapshot.Snapshot {
	t.Helper()
	file, err := snapshot.NewFile("main.go", []byte("package "+name+"\n"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	hash, length := file.ContentHash(), int64(file.Len())
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: worktree, ProfileID: profile, PolicyDigest: policyDigest, ProviderDigest: concurrencyProvider, Files: []model.ManifestFile{{Path: "main.go", Status: "captured", ContentHash: &hash, ByteLength: &length}}, DependencyInputDigests: []string{}}
	captured, err := snapshot.Freeze(manifest, []snapshot.File{file}, 8192, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return captured
}

func concurrencyAuthorize(context.Context, string, string, string, int64) error { return nil }

func stageConcurrencyGeneration(t *testing.T, ctx context.Context, s concurrencyStore, writer *runtime.Writer, generation string, captured *snapshot.Snapshot, name string) (index.CaptureWrite, index.ProviderStageStream) {
	t.Helper()
	if err := s.store.BeginGeneration(ctx, index.BeginGenerationRequest{GenerationID: generation, JobID: concurrencyJob, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Authorize: concurrencyAuthorize, MaxManifestBytes: 8192}); err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	capture := index.CaptureWrite{GenerationID: generation, JobID: concurrencyJob, PolicyEpoch: 1, Snapshot: captured, Writer: writer, Scanner: scanner, MaxManifestBytes: 8192, MaxSourceBytes: 1024, Authorize: concurrencyAuthorize}
	if err := s.store.StageFiles(ctx, index.StageFilesRequest{Capture: capture, Files: []index.FileClassification{{Path: "main.go", Kind: "go"}}}); err != nil {
		t.Fatal(err)
	}
	stream, err := s.store.BeginProviderStage(ctx, capture, concurrencyAuthorize)
	if err != nil {
		t.Fatal(err)
	}
	path := "main.go"
	batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: concurrencyJob, Sequence: 1, Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{{SymbolID: "fixture-symbol", Path: &path, Kind: "package", Name: name, QualifiedName: name, ScopeKey: "fixture-scope", Range: &model.ByteRange{StartByte: 8, EndByte: int64(8 + len(name))}, Signature: ""}}, Relations: []model.RelationFact{}, Diagnostics: []model.DiagnosticFact{}}
	if _, err := s.store.StageProviderBatch(ctx, index.StageProviderRequest{Capture: capture, Stream: stream, Batch: batch, MaxFrameBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	done := model.ProviderDone{ProviderProtocol: 1, Type: "analyze_done", JobID: concurrencyJob, Sequence: 2, Status: "partial", InputDigest: captured.Manifest().SnapshotDigest, Capabilities: []model.Capability{{Name: "go_semantics", ProviderID: "fixture", ProviderVersion: "1", Status: "partial"}}, Gaps: []model.Gap{{Code: "FIXTURE_ONLY", Reason: "Synthetic provider output.", SourceRefs: []model.SourceRef{}}}}
	if err := s.store.SealProviderStage(ctx, index.SealProviderRequest{Capture: capture, Stream: stream, Done: done, MaxFrameBytes: 65536, AuthorizeGeneration: concurrencyAuthorize}); err != nil {
		t.Fatal(err)
	}
	return capture, stream
}

func publishConcurrencyGeneration(t *testing.T, ctx context.Context, s concurrencyStore, capture index.CaptureWrite, stream index.ProviderStageStream) {
	t.Helper()
	request := index.PublishProviderRequest{Capture: capture, Stream: stream, AuthorizeGeneration: concurrencyAuthorize, Revalidate: func(context.Context, *snapshot.Snapshot) error { return nil }, VerifyProvider: func(context.Context, *snapshot.Snapshot, model.ProviderDone) error { return nil }, MaxFacts: 32, MaxFrameBytes: 65536}
	if err := s.store.PublishProvider(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := s.store.ReleaseProviderStage(ctx, capture, stream, concurrencyAuthorize); err != nil {
		t.Fatal(err)
	}
}

func concurrencyRead(ctx context.Context, s concurrencyStore, profile string, scanner *policy.Scanner) (index.PublishedFileSymbols, error) {
	return s.store.ReadPublishedFileSymbols(ctx, index.PublishedFileSymbolsRequest{File: index.PublishedFileRequest{ProfileID: profile, Path: "main.go", Scanner: scanner, MaxManifestBytes: 8192, MaxFileBytes: 1024, AuthorizeGeneration: concurrencyAuthorize, AuthorizePath: concurrencyAuthorize}, MaxSymbols: 4, MaxFactsBytes: 65536})
}

func TestConcurrencyWriterHelper(t *testing.T) {
	dir := os.Getenv("MCE4D_TEST_CONCURRENCY_DIR")
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	worktree, profile, policyDigest := os.Getenv("MCE4D_TEST_WORKTREE"), os.Getenv("MCE4D_TEST_PROFILE"), os.Getenv("MCE4D_TEST_POLICY")
	s := openConcurrencyStore(t, ctx, dir, worktree, false)
	defer s.close()
	writer, err := runtime.AcquireWriter(ctx, s.lease, concurrencyJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	data, err := os.ReadFile(filepath.Join(os.Getenv("MCE4D_TEST_REPO"), "main.go"))
	if err != nil || string(data) != "package anew\n" {
		t.Fatalf("unexpected fixture source: %q %v", data, err)
	}
	captured := concurrencySnapshot(t, worktree, profile, policyDigest, "anew")
	capture, stream := stageConcurrencyGeneration(t, ctx, s, writer, "22222222-2222-4222-8222-222222222222", captured, "anew")
	fmt.Fprintln(os.Stdout, "A_BUILDING")
	if _, err := bufio.NewReader(os.Stdin).ReadByte(); err != nil {
		t.Fatal(err)
	}
	publishConcurrencyGeneration(t, ctx, s, capture, stream)
	fmt.Fprintln(os.Stdout, "A_PUBLISHED")
}

func TestConcurrentWorktreesProfilesAndReaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	base := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	gitEnv := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + base, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"}
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Env = gitEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git: %s %v", out, err)
		}
		return strings.TrimSpace(string(out))
	}
	for _, dir := range []string{"a", "b", "c", "template", "state-a", "state-b"} {
		if err := os.Mkdir(filepath.Join(base, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	repoA, repoB, repoC := filepath.Join(base, "a", "repo"), filepath.Join(base, "b", "repo"), filepath.Join(base, "c", "repo")
	runGit("init", "-q", "--template="+filepath.Join(base, "template"), repoA)
	if err := os.WriteFile(filepath.Join(repoA, "main.go"), []byte("package old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("-C", repoA, "add", "main.go")
	runGit("-C", repoA, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
	runGit("clone", "-q", "--no-hardlinks", "--template="+filepath.Join(base, "template"), repoA, repoB)
	runGit("-C", repoA, "remote", "add", "origin", "https://example.invalid/shared.git")
	runGit("-C", repoB, "remote", "set-url", "origin", "https://example.invalid/shared.git")
	if runGit("-C", repoA, "rev-parse", "HEAD") != runGit("-C", repoB, "rev-parse", "HEAD") {
		t.Fatal("fixture HEAD mismatch")
	}
	runGit("-C", repoA, "worktree", "add", "--detach", repoC, "HEAD")
	if runGit("-C", repoA, "rev-parse", "HEAD") != runGit("-C", repoC, "rev-parse", "HEAD") {
		t.Fatal("linked worktree HEAD mismatch")
	}
	if err := os.WriteFile(filepath.Join(repoC, "main.go"), []byte("package linked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoB, "main.go"), []byte("package binit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(repoA) != filepath.Base(repoB) || runGit("-C", repoA, "remote", "get-url", "origin") != runGit("-C", repoB, "remote", "get-url", "origin") {
		t.Fatal("fixture identity case not established")
	}
	catalogDB := concurrencyDB(t, filepath.Join(base, "catalog.sqlite"), 1)
	defer catalogDB.Close()
	admin, err := catalog.Open(ctx, catalogDB)
	if err != nil {
		t.Fatal(err)
	}
	policyConfig := model.PolicyConfig{SchemaVersion: 1, AllowedRoots: []string{base}, DeniedGlobs: []string{}, TrustedRulePaths: []string{}, DependencyRoots: []string{}, AllowedExecutables: []model.AllowedExecutable{}, MaxFileBytes: 1024, MaxSourceBytes: 8192, MaxOutputBytes: 4096, SecretRulesVersion: "v1"}
	profileConfig := model.GoProfile{SchemaVersion: 1, Language: "go", Goos: "linux", Goarch: "amd64", BuildTags: []string{}, ModuleRoots: []string{"."}, ToolchainDigest: strings.Repeat("a", 64), ProviderDigest: concurrencyProvider, ModuleMode: "readonly", AnalysisMode: "syntax", DependencyRoots: []string{}}
	policyJSON, err := json.Marshal(policyConfig)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, err := json.Marshal(profileConfig)
	if err != nil {
		t.Fatal(err)
	}
	register := func(repo string) catalog.Registration {
		t.Helper()
		root, err := paths.OpenRoot(repo)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		common, err := runtime.OpenLocalGitMetadata(root)
		if err != nil {
			t.Fatal(err)
		}
		defer common.Close()
		reg, err := admin.Register(ctx, root, common, "", "default", policyJSON, profileJSON)
		if err != nil {
			t.Fatal(err)
		}
		return reg
	}
	a, b := register(repoA), register(repoB)
	if a.RepoID == b.RepoID || a.WorktreeID == b.WorktreeID || a.ProfileID != b.ProfileID || a.PolicyDigest != b.PolicyDigest {
		t.Fatalf("incorrect clone identity: A=%+v B=%+v", a, b)
	}
	linkedRoot, err := paths.OpenRoot(repoC)
	if err != nil {
		t.Fatal(err)
	}
	defer linkedRoot.Close()
	rootA, err := paths.OpenRoot(repoA)
	if err != nil {
		t.Fatal(err)
	}
	defer rootA.Close()
	commonA, err := runtime.OpenLocalGitMetadata(rootA)
	if err != nil {
		t.Fatal(err)
	}
	defer commonA.Close()
	linkedGitDir := runGit("-C", repoC, "rev-parse", "--absolute-git-dir")
	linkedRelative, err := filepath.Rel(filepath.Join(repoA, ".git"), linkedGitDir)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := admin.Register(ctx, linkedRoot, commonA, filepath.ToSlash(linkedRelative), "default", policyJSON, profileJSON)
	if err != nil || linked.RepoID != a.RepoID || linked.WorktreeID == a.WorktreeID || linked.WorktreeID == b.WorktreeID || linked.ProfileID != a.ProfileID {
		t.Fatalf("incorrect linked worktree identity: %+v %v", linked, err)
	}
	if concurrencySnapshot(t, a.WorktreeID, a.ProfileID, a.PolicyDigest, "old").Manifest().SnapshotDigest == concurrencySnapshot(t, linked.WorktreeID, linked.ProfileID, linked.PolicyDigest, "linked").Manifest().SnapshotDigest {
		t.Fatal("linked snapshots collapsed to one identity")
	}
	profileConfig.TestsEnabled = true
	otherProfile, err := model.ProfileDigest(&profileConfig, 8192)
	if err != nil || otherProfile == a.ProfileID {
		t.Fatalf("profile identity: %s %v", otherProfile, err)
	}
	storeA := openConcurrencyStore(t, ctx, filepath.Join(base, "state-a"), a.WorktreeID, true)
	defer storeA.close()
	storeB := openConcurrencyStore(t, ctx, filepath.Join(base, "state-b"), b.WorktreeID, true)
	defer storeB.close()
	writerA, err := runtime.AcquireWriter(ctx, storeA.lease, concurrencyJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	oldA, streamA := stageConcurrencyGeneration(t, ctx, storeA, writerA, "33333333-3333-4333-8333-333333333333", concurrencySnapshot(t, a.WorktreeID, a.ProfileID, a.PolicyDigest, "old"), "old")
	publishConcurrencyGeneration(t, ctx, storeA, oldA, streamA)
	if err := writerA.Close(); err != nil {
		t.Fatal(err)
	}
	writerB, err := runtime.AcquireWriter(ctx, storeB.lease, concurrencyJob, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer writerB.Close()
	for i, profile := range []string{b.ProfileID, otherProfile} {
		name := "binit"
		captured := concurrencySnapshot(t, b.WorktreeID, profile, b.PolicyDigest, name)
		capture, stream := stageConcurrencyGeneration(t, ctx, storeB, writerB, fmt.Sprintf("%08x-4444-4444-8444-444444444444", i+1), captured, name)
		publishConcurrencyGeneration(t, ctx, storeB, capture, stream)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConcurrencyWriterHelper$")
	if err := os.WriteFile(filepath.Join(repoA, "main.go"), []byte("package anew\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "MCE4D_TEST_CONCURRENCY_DIR="+filepath.Join(base, "state-a"), "MCE4D_TEST_REPO="+repoA, "MCE4D_TEST_WORKTREE="+a.WorktreeID, "MCE4D_TEST_PROFILE="+a.ProfileID, "MCE4D_TEST_POLICY="+a.PolicyDigest)
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
	lines := bufio.NewReader(stdout)
	if line, err := lines.ReadString('\n'); err != nil || line != "A_BUILDING\n" {
		t.Fatalf("child did not stage: %q %v", line, err)
	}
	if _, err := runtime.AcquireWriter(ctx, storeA.lease, concurrencyJob, 120*time.Millisecond); !errors.Is(err, runtime.ErrWorktreeBusy) {
		t.Fatalf("A writer was not exclusive: %v", err)
	}
	scanner, err := policy.NewScanner(1024)
	if err != nil {
		t.Fatal(err)
	}
	oldView, err := concurrencyRead(ctx, storeA, a.ProfileID, scanner)
	if err != nil || oldView.File.GenerationID != oldA.GenerationID || len(oldView.Symbols) != 1 || oldView.Symbols[0].Name != "old" {
		t.Fatalf("A old snapshot unavailable: %+v %v", oldView, err)
	}
	stop := make(chan struct{})
	errorsSeen := make(chan error, 3)
	started := make(chan int, 3)
	changed := make(chan int, 3)
	var readers sync.WaitGroup
	for number, item := range []struct {
		store             concurrencyStore
		profile, worktree string
	}{{storeA, a.ProfileID, a.WorktreeID}, {storeB, b.ProfileID, b.WorktreeID}, {storeB, otherProfile, b.WorktreeID}} {
		readers.Add(1)
		go func(number int, item struct {
			store             concurrencyStore
			profile, worktree string
		}) {
			defer readers.Done()
			firstGeneration := ""
			sawChange := false
			for {
				select {
				case <-stop:
					return
				default:
				}
				view, err := concurrencyRead(ctx, item.store, item.profile, scanner)
				if err != nil {
					errorsSeen <- err
					return
				}
				if view.File.WorktreeID != item.worktree || view.File.ProfileID != item.profile || len(view.Symbols) != 1 || string(view.File.Bytes) != "package "+view.Symbols[0].Name+"\n" {
					errorsSeen <- fmt.Errorf("mixed generation/profile source")
					return
				}
				if firstGeneration == "" {
					firstGeneration = view.File.GenerationID
					started <- number
				} else if !sawChange && firstGeneration != view.File.GenerationID {
					sawChange = true
					changed <- number
				}
			}
		}(number, item)
	}
	stopped := false
	defer func() {
		if !stopped {
			close(stop)
			readers.Wait()
		}
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case err := <-errorsSeen:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	for i := 0; i < 8; i++ {
		profile := b.ProfileID
		if i%2 != 0 {
			profile = otherProfile
		}
		name := fmt.Sprintf("bnext%d", i)
		if err := os.WriteFile(filepath.Join(repoB, "main.go"), []byte("package "+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		capture, stream := stageConcurrencyGeneration(t, ctx, storeB, writerB, fmt.Sprintf("%08x-4444-4444-8444-444444444444", i+10), concurrencySnapshot(t, b.WorktreeID, profile, b.PolicyDigest, name), name)
		publishConcurrencyGeneration(t, ctx, storeB, capture, stream)
	}
	if _, err := io.WriteString(stdin, "p"); err != nil {
		t.Fatal(err)
	}
	if line, err := lines.ReadString('\n'); err != nil || line != "A_PUBLISHED\n" {
		t.Fatalf("child did not publish: %q %v", line, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	newView, err := concurrencyRead(ctx, storeA, a.ProfileID, scanner)
	if err != nil || len(newView.Symbols) != 1 || newView.Symbols[0].Name != "anew" {
		t.Fatalf("A new snapshot unavailable: %+v %v", newView, err)
	}
	for _, item := range []struct{ profile, want string }{{b.ProfileID, "bnext6"}, {otherProfile, "bnext7"}} {
		view, err := concurrencyRead(ctx, storeB, item.profile, scanner)
		if err != nil || len(view.Symbols) != 1 || view.Symbols[0].Name != item.want {
			t.Fatalf("wrong profile view: %+v %v", view, err)
		}
	}
	for _, item := range []struct{ root, expected string }{{repoA, "package anew\n"}, {repoB, "package bnext7\n"}, {repoC, "package linked\n"}} {
		data, err := os.ReadFile(filepath.Join(item.root, "main.go"))
		if err != nil || string(data) != item.expected {
			t.Fatalf("fixture source changed unexpectedly: %q %v", data, err)
		}
	}
	seen := map[int]bool{}
	for len(seen) < 3 {
		select {
		case number := <-changed:
			seen[number] = true
		case err := <-errorsSeen:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatalf("readers did not observe all profile updates: %v", ctx.Err())
		}
	}
	close(stop)
	readers.Wait()
	stopped = true
	select {
	case err := <-errorsSeen:
		t.Fatal(err)
	default:
	}
}
