//go:build linux

package lsp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

const fixtureWorktree = "11111111-1111-4111-8111-111111111111"
const fixtureGeneration = "22222222-2222-4222-8222-222222222222"

func resultFixture(t *testing.T, count int) (model.ScopeVector, RenameResultPayload, string) {
	t.Helper()
	h := strings.Repeat("a", 64)
	member := model.ScopeMember{WorktreeID: fixtureWorktree, ProfileID: h, GenerationID: fixtureGeneration, SnapshotDigest: h, PolicyDigest: h, PolicyEpoch: 1, BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: fixtureGeneration, RecordsIncarnation: fixtureGeneration}
	scope := model.ScopeVector{Kind: "single", Members: []model.ScopeMember{member}, CatalogIncarnation: fixtureGeneration, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	pre := model.RenameFilePrecondition{WorktreeID: fixtureWorktree, ProfileID: h, GenerationID: fixtureGeneration, Path: "a.go", ContentHash: h}
	edits := make([]model.ProposedEdit, count)
	for i := range edits {
		at := int64((count - 1 - i) * 2)
		edits[i] = model.ProposedEdit{SourceRef: model.SourceRef{WorktreeID: pre.WorktreeID, ProfileID: pre.ProfileID, GenerationID: pre.GenerationID, Path: pre.Path, ContentHash: pre.ContentHash, Range: model.ByteRange{StartByte: at, EndByte: at + 1}}, NewText: fmt.Sprintf("n%04d", i)}
	}
	payload := RenameResultPayload{InitialRequest: model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: h, ByteOffset: 0}, NewName: "new_name", PreserveContracts: true}, ProviderDigest: h, FilePreconditions: []model.RenameFilePrecondition{pre}, Edits: edits, Conflicts: []model.Gap{}}
	return scope, payload, strings.Repeat("b", 64)
}

func makeResultStore(t *testing.T, dir string, quota int64) (*ResultStore, *runtime.Lifecycle) {
	t.Helper()
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lease, err := runtime.AcquireLifecycle(ctx, root, fixtureWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(int(resultHardBytes))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewResultStore(root, lease, fixtureWorktree, scanner, 0, quota)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(); _ = root.Close() })
	return store, lease
}

func resultFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "scratch"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "scratch", "rename"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func allowResult(context.Context, RenameStoredResult) error { return nil }

func TestResultStoreRejectsLeaseFromDifferentRoot(t *testing.T) {
	store, lease := makeResultStore(t, resultFixtureDir(t), 0)
	other := resultFixtureDir(t)
	root, err := paths.OpenRoot(other)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	misbound, err := NewResultStore(root, lease, fixtureWorktree, store.scanner, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	scope, payload, principal := resultFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if header, err := misbound.Create(ctx, scope, principal, payload, allowResult); err == nil || header.ResultID != "" {
		t.Fatalf("accepted lifecycle from another root: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(other, "scratch", "rename")); err != nil || len(entries) != 0 {
		t.Fatal("wrong-root lifecycle created rename files")
	}
}

func TestResultStoreCrossProcessFixture(t *testing.T) {
	if dir := os.Getenv("MCE4D_RESULT_FIXTURE"); dir != "" {
		store, _ := makeResultStore(t, dir, 0)
		scope, _, principal := resultFixture(t, 0)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := store.Read(ctx, os.Getenv("MCE4D_RESULT_ID"), principal, scope, allowResult)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("edits=%d digest=%s\n", len(result.Payload.Edits), result.Header.ResultDigest)
		return
	}
	dir := resultFixtureDir(t)
	store, lease := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 1025)
	firstInput := payload.Edits[0]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	guard := func(context.Context, RenameStoredResult) error {
		if err := lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
			return errors.New("shared lease was not held")
		}
		return nil
	}
	// The first guard runs before the operation opens the spool. Hold the lease
	// independently only for that check, then prove the store holds it on read.
	header, err := store.Create(ctx, scope, principal, payload, allowResult)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Edits[0] != firstInput {
		t.Fatal("input edits were mutated")
	}
	result, err := store.Read(ctx, header.ResultID, principal, scope, guard)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Payload.Edits) != 1025 || result.Header.ResultDigest != header.ResultDigest {
		t.Fatal("result missing edits or digest")
	}
	for i := 1; i < len(result.Payload.Edits); i++ {
		if result.Payload.Edits[i-1].SourceRef.Range.StartByte >= result.Payload.Edits[i].SourceRef.Range.StartByte {
			t.Fatal("edits not sorted")
		}
	}
	ctxChild, cancelChild := context.WithTimeout(ctx, 10*time.Second)
	defer cancelChild()
	cmd := exec.CommandContext(ctxChild, os.Args[0], "-test.run=^TestResultStoreCrossProcessFixture$")
	cmd.Env = append(os.Environ(), "MCE4D_RESULT_FIXTURE="+dir, "MCE4D_RESULT_ID="+header.ResultID)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), fmt.Sprintf("edits=1025 digest=%s", header.ResultDigest)) {
		t.Fatalf("independent reader failed: %v %s", err, out)
	}
	if _, err = store.Read(ctx, header.ResultID, strings.Repeat("c", 64), scope, allowResult); !errors.Is(err, ErrResultInvalid) {
		t.Fatalf("wrong principal accepted: %v", err)
	}
	if _, err = store.Read(ctx, "99999999-9999-4999-8999-999999999999", principal, scope, allowResult); !errors.Is(err, ErrResultExpired) {
		t.Fatalf("missing result recomputed: %v", err)
	}
	store.now = func() time.Time { return time.Unix(header.ExpiresAt, 0) }
	if _, err = store.Read(ctx, header.ResultID, principal, scope, allowResult); !errors.Is(err, ErrResultExpired) {
		t.Fatalf("expired result returned: %v", err)
	}
}

func TestResultStoreRejectsTamperQuotaAndRevocation(t *testing.T) {
	t.Run("tamper", func(t *testing.T) {
		dir := resultFixtureDir(t)
		store, _ := makeResultStore(t, dir, 0)
		scope, payload, principal := resultFixture(t, 3)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h, err := store.Create(ctx, scope, principal, payload, allowResult)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "scratch", "rename", h.ResultID+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		changed := strings.Replace(string(raw), `"new_text":"n0000"`, `"new_text":"n9999"`, 1)
		if changed == string(raw) {
			t.Fatal("fixture replacement failed")
		}
		if err = os.WriteFile(path, []byte(changed), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Read(ctx, h.ResultID, principal, scope, allowResult); !errors.Is(err, ErrResultInvalid) {
			t.Fatalf("tamper accepted: %v", err)
		}
	})
	t.Run("quota", func(t *testing.T) {
		dir := resultFixtureDir(t)
		store, _ := makeResultStore(t, dir, 500)
		scope, payload, principal := resultFixture(t, 3)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := store.Create(ctx, scope, principal, payload, allowResult); !errors.Is(err, ErrResultQuota) {
			t.Fatalf("quota ignored: %v", err)
		}
	})
	t.Run("revocation after write", func(t *testing.T) {
		dir := resultFixtureDir(t)
		store, _ := makeResultStore(t, dir, 0)
		scope, payload, principal := resultFixture(t, 3)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		denied := errors.New("revoked")
		calls := 0
		h, err := store.Create(ctx, scope, principal, payload, func(context.Context, RenameStoredResult) error {
			calls++
			if calls == 3 {
				return denied
			}
			return nil
		})
		if !errors.Is(err, denied) || h.ResultID != "" || calls != 3 {
			t.Fatalf("revocation exposed a result: %v calls=%d", err, calls)
		}
	})
	t.Run("secret rejection", func(t *testing.T) {
		dir := resultFixtureDir(t)
		store, _ := makeResultStore(t, dir, 0)
		scope, payload, principal := resultFixture(t, 1)
		payload.Edits[0].NewText = "password=" + strings.Repeat("x", 20)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := store.Create(ctx, scope, principal, payload, allowResult); !errors.Is(err, policy.ErrSecretBlocked) {
			t.Fatalf("secret marker accepted: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(dir, "scratch", "rename"))
		if err != nil || len(entries) != 0 {
			t.Fatal("secret was persisted")
		}
	})
}

func TestResultStoreDigestHelperIsStable(t *testing.T) {
	scope, payload, _ := resultFixture(t, 4)
	a, err := model.RenameResultDigest(scope, payload.InitialRequest, payload.ProviderDigest, payload.FilePreconditions, payload.Edits, payload.Conflicts, 65536)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := normalizeRenamePayload(payload, 65536)
	if err != nil {
		t.Fatal(err)
	}
	b, err := model.RenameResultDigest(scope, ordered.InitialRequest, ordered.ProviderDigest, ordered.FilePreconditions, ordered.Edits, ordered.Conflicts, 65536)
	if err != nil || a != b {
		t.Fatalf("result digest changed: %v", err)
	}
	h := sha256.Sum256([]byte(a))
	if len(hex.EncodeToString(h[:])) != 64 {
		t.Fatal("invalid digest")
	}
}

func TestResultQuotaAcrossProcesses(t *testing.T) {
	if dir := os.Getenv("MCE4D_RESULT_QUOTA_FIXTURE"); dir != "" {
		quota, err := strconv.ParseInt(os.Getenv("MCE4D_RESULT_QUOTA_BYTES"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		store, _ := makeResultStore(t, dir, quota)
		scope, payload, principal := resultFixture(t, 4)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err = store.Create(ctx, scope, principal, payload, allowResult)
		if err == nil {
			fmt.Println("accepted")
			return
		}
		if errors.Is(err, ErrResultQuota) {
			fmt.Println("quota")
			return
		}
		t.Fatal(err)
	}
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 4)
	_, raw, err := store.prepare(scope, principal, payload)
	if err != nil {
		t.Fatal(err)
	}
	quota := int64(len(raw) + 16)
	outputs := make([][]byte, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range outputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResultQuotaAcrossProcesses$")
			cmd.Env = append(os.Environ(), "MCE4D_RESULT_QUOTA_FIXTURE="+dir, "MCE4D_RESULT_QUOTA_BYTES="+strconv.FormatInt(quota, 10))
			outputs[i], errs[i] = cmd.CombinedOutput()
		}(i)
	}
	wg.Wait()
	accepted, rejected := 0, 0
	for i, output := range outputs {
		if errs[i] != nil {
			t.Fatalf("quota child failed: %v %s", errs[i], output)
		}
		if strings.Contains(string(output), "accepted") {
			accepted++
		}
		if strings.Contains(string(output), "quota") {
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("quota coordination failed: accepted=%d quota=%d", accepted, rejected)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "scratch", "rename"))
	if err != nil {
		t.Fatal(err)
	}
	jsonCount := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			jsonCount++
		}
		if strings.HasSuffix(entry.Name(), ".pending") {
			t.Fatal("quota failure left a pending file")
		}
	}
	if jsonCount != 1 {
		t.Fatalf("unexpected installed result count: %d", jsonCount)
	}
}

func TestResultStoreCrashBeforePublish(t *testing.T) {
	if dir := os.Getenv("MCE4D_RESULT_CRASH_FIXTURE"); dir != "" {
		store, _ := makeResultStore(t, dir, 0)
		scope, payload, principal := resultFixture(t, 4)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		calls := 0
		_, err := store.Create(ctx, scope, principal, payload, func(context.Context, RenameStoredResult) error {
			calls++
			if calls == 2 {
				if err := os.WriteFile(filepath.Join(dir, "fixture-ready"), []byte("ready"), 0600); err != nil {
					return err
				}
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := resultFixtureDir(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestResultStoreCrashBeforePublish$")
	cmd.Env = append(os.Environ(), "MCE4D_RESULT_CRASH_FIXTURE="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "fixture-ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not reach the pre-publish boundary")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child was not interrupted")
	}
	finished = true
	renameDir := filepath.Join(dir, "scratch", "rename")
	entries, err := os.ReadDir(renameDir)
	if err != nil {
		t.Fatal(err)
	}
	pending := ""
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			t.Fatal("partial result was published")
		}
		if strings.HasSuffix(entry.Name(), ".pending") {
			pending = entry.Name()
		}
	}
	if pending == "" {
		t.Fatal("expected owned pending file was missing")
	}
	before, err := os.ReadFile(filepath.Join(renameDir, pending))
	if err != nil {
		t.Fatal(err)
	}
	store, _ := makeResultStore(t, dir, 0)
	scope, payload, principal := resultFixture(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err = store.Create(ctx, scope, principal, payload, allowResult); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(renameDir, pending))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("crash residue was changed")
	}
}

func TestResultQuotaLockWaitIsBounded(t *testing.T) {
	dir := resultFixtureDir(t)
	store, _ := makeResultStore(t, dir, 0)
	lock, err := os.OpenFile(filepath.Join(dir, "scratch", "rename", resultQuotaLock), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	scope, payload, principal := resultFixture(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = store.Create(ctx, scope, principal, payload, allowResult)
	wait := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) || wait < 100*time.Millisecond || wait > 2*time.Second {
		t.Fatalf("unexpected lock wait: %v %v", wait, err)
	}
	t.Logf("fixture lock wait: %d ms", wait.Milliseconds())
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err = store.Create(ctx2, scope, principal, payload, allowResult); err != nil {
		t.Fatalf("lock did not recover: %v", err)
	}
}
