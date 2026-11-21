package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func TestMain(m *testing.M) {
	if handled, code := sqlite.RunProtectedWorker(context.Background(), os.Args[1:], os.Stdout); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func scopedWindowsToken(t *testing.T, ctx context.Context, installation, worktree, profile, mode, token string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 16*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsScopedRenameStorageWorker$", "-test.timeout=15s")
	cmd.Env = append(os.Environ(), "MCE4D_WINDOWS_RESULT_MODE="+mode, "MCE4D_WINDOWS_RESULT_ROOT="+installation,
		"MCE4D_WINDOWS_RESULT_WORKTREE="+worktree, "MCE4D_WINDOWS_RESULT_PROFILE="+profile, "MCE4D_WINDOWS_RESULT_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scoped Windows rename store %s: %v %s", mode, err, out)
	}
	if mode != "issue" {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if value, ok := strings.CutPrefix(line, "fixture-token "); ok {
			return strings.TrimSpace(value)
		}
	}
	t.Fatal("protected rename spool did not issue a token")
	return ""
}

func TestWindowsScopedRenameStorageWorker(t *testing.T) {
	mode := os.Getenv("MCE4D_WINDOWS_RESULT_MODE")
	if mode == "" {
		t.Skip("requires the disposable Windows result fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	defer cancel()
	root, worktree, profile := os.Getenv("MCE4D_WINDOWS_RESULT_ROOT"), os.Getenv("MCE4D_WINDOWS_RESULT_WORKTREE"), os.Getenv("MCE4D_WINDOWS_RESULT_PROFILE")
	state, err := sqlite.OpenWindowsScopedPools(ctx, root, worktree, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := state.Close(); err != nil {
			t.Error(err)
		}
	}()
	digest := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	actor := app.CorePrincipal{ID: "windows-result-fixture:" + state.Summary.InstallationID,
		Digest: digest("windows-result-actor:" + state.Summary.InstallationID), BindingDigest: digest(worktree + "/" + profile)}
	reader := compose.ScopeReader{Catalog: state.Catalog, Index: state.Index, Records: state.Records, WorktreeRoot: state.Target,
		CommonRoot: state.CommonGit, WorktreeID: worktree, ProfileID: profile, MaxHeads: 1000, MaxManifestBytes: 1 << 20,
		Access: func(ctx context.Context, p app.CorePrincipal, w, id string) error {
			if p != actor || w != worktree || id != profile {
				return compose.ErrAuthority
			}
			return state.Check(ctx)
		}}
	resultRoot, lease, err := state.PrepareRenameStorage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	store, err := lsp.NewResultStore(resultRoot, lease, worktree, scanner, 128<<10, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	key, err := state.CursorKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := contextpack.NewCursorCodec(key)
	clear(key[:])
	if err != nil {
		t.Fatal(err)
	}
	var sourceErr error
	check := func(ctx context.Context, scope model.ScopeVector) error {
		_, err := reader.Recheck(ctx, actor, scope)
		sourceErr = err
		return err
	}
	cursorGuard := func(ctx context.Context, binding contextpack.CursorBinding) error {
		if binding.BindingDigest != actor.BindingDigest || binding.PrincipalDigest != actor.Digest {
			return compose.ErrAuthority
		}
		return check(ctx, binding.Scope)
	}
	resultGuard := func(ctx context.Context, result lsp.RenameStoredResult) error {
		return check(ctx, result.Header.Scope)
	}
	switch mode {
	case "issue":
		observed, err := reader.Resolve(ctx, actor, "")
		if err != nil {
			t.Fatal(err)
		}
		m := observed.Vector.Members[0]
		input, err := state.Target.OpenRegular("marker.go")
		if err != nil {
			t.Fatal(err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(input, 4096))
		if err := errors.Join(readErr, input.Close()); err != nil {
			t.Fatal(err)
		}
		name := []byte("PublishMarker")
		at := bytes.Index(raw, name)
		if at < 0 {
			t.Fatal("synthetic marker was missing")
		}
		hash := digest(string(raw))
		ref := model.SourceRef{WorktreeID: worktree, ProfileID: profile, GenerationID: m.GenerationID, Path: "marker.go",
			ContentHash: hash, Range: model.ByteRange{StartByte: int64(at), EndByte: int64(at + len(name))}}
		payload := lsp.RenameResultPayload{ProviderDigest: observed.Binding.Profile.ProviderDigest,
			InitialRequest: model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: ref.Path,
				ContentHash: hash, ByteOffset: int64(at)}, NewName: "FixtureRename", PreserveContracts: true},
			FilePreconditions: []model.RenameFilePrecondition{{WorktreeID: worktree, ProfileID: profile, GenerationID: m.GenerationID, Path: ref.Path, ContentHash: hash}},
			Edits:             []model.ProposedEdit{{SourceRef: ref, NewText: "FixtureRename"}}, Conflicts: []model.Gap{}}
		header, err := store.Create(ctx, observed.Vector, actor.Digest, payload, resultGuard)
		if err != nil {
			t.Fatal(err)
		}
		binding := contextpack.CursorBinding{Purpose: "rename-result", PrincipalDigest: actor.Digest, BindingDigest: actor.BindingDigest, OrderVersion: "rename-v1",
			RequestDigest: digest("windows-synthetic-rename"), Scope: observed.Vector, ResultID: header.ResultID, ResultDigest: header.ResultDigest}
		token, err := store.IssueResultToken(ctx, codec, binding, cursorGuard, resultGuard)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("fixture-token", token)
	case "validate", "reject":
		token := os.Getenv("MCE4D_WINDOWS_RESULT_TOKEN")
		header, err := store.ValidateResultToken(ctx, codec, token, actor.Digest, cursorGuard, resultGuard)
		if mode == "reject" {
			if err == nil || header.ResultID != "" || !errors.Is(sourceErr, compose.ErrScopeChanged) {
				t.Fatalf("expected authenticated scope change after restore: %v, %v", err, sourceErr)
			}
			return
		}
		if err != nil || header.ResultID == "" {
			t.Fatalf("fresh process could not validate the spool: %v", err)
		}
		page, err := store.ReadResultPage(ctx, codec, token, "", actor.Digest, cursorGuard, resultGuard, 16, 8192)
		if err != nil || len(page.Data.Edits) != 1 || page.Data.Edits[0].NewText != "FixtureRename" || page.Data.Applied {
			t.Fatalf("fresh process did not read the complete stored preview: %v", err)
		}
	default:
		t.Fatal("unknown result fixture action")
	}
}
