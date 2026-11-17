package contextpack

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func fragmentFixture(t *testing.T) (context.Context, *TaskSlice, TaskEvidence, []byte) {
	t.Helper()
	ctx, in, p, refs := taskFixture(t)
	manifest := in.Snapshot.Manifest()
	manifest.SnapshotDigest = ""
	files := []snapshot.File{}
	for i := range manifest.Files {
		entry := &manifest.Files[i]
		file, ok := in.Snapshot.File(entry.Path)
		if !ok {
			t.Fatal("missing fixture file")
		}
		if entry.Path == "a.go" {
			body := append(file.Bytes(), []byte("\r\n// "+strings.Repeat("ລ😀", 2000)+"\r\n")...)
			var err error
			file, err = snapshot.NewFile(entry.Path, body, 65536)
			if err != nil {
				t.Fatal(err)
			}
			hash, size := file.ContentHash(), int64(file.Len())
			entry.ContentHash, entry.ByteLength = &hash, &size
		}
		files = append(files, file)
	}
	var err error
	in.Snapshot, err = snapshot.Freeze(manifest, files, 65536, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	in.Scope.Members[0].SnapshotDigest = in.Snapshot.Manifest().SnapshotDigest
	for i := range in.Packages {
		in.Packages[i].InputDigest = in.Scope.Members[0].SnapshotDigest
	}
	in.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(in.Scope, 65536)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := Select(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	s, err := BuildTaskSlice(ctx, SliceInput{Selection: selected, Policy: p, TaskKind: "behavior_change", RuleAnchors: refs[:2], VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }, MaxBytes: 4 * 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range s.report.Items {
		if item.Why == "target_source" && item.Kind == "source_range" {
			file, _ := in.Snapshot.File(item.SourceRefs[0].Path)
			return ctx, s, item, file.Bytes()
		}
	}
	t.Fatal("missing mandatory source")
	return nil, nil, TaskEvidence{}, nil
}

func TestSourceFragmentsReassembleWithVariableBudgets(t *testing.T) {
	ctx, slice, item, source := fragmentFixture(t)
	parent := item.SourceRefs[0]
	var fragments []model.EvidenceItem
	var assembled bytes.Buffer
	offset := parent.Range.StartByte
	for i := 0; offset < parent.Range.EndByte; i++ {
		budget := []int{19, 47, 23, 101}[i%4]
		fragment, next, complete, err := NextSourceFragment(ctx, slice, item.ID, offset, budget)
		if err != nil {
			t.Fatal(err)
		}
		if next <= offset || next-offset > int64(budget) || complete != (next == parent.Range.EndByte) || fragment.FragmentOf == nil || *fragment.FragmentOf != parent || fragment.SourceRefs[0].Range.StartByte != offset {
			t.Fatal("incorrect fragment progress or parent")
		}
		if err := fragment.ValidateFragment(source, 65536); err != nil {
			t.Fatal(err)
		}
		assembled.WriteString(*fragment.Content)
		fragments = append(fragments, fragment)
		offset = next
	}
	if len(fragments) < 2 || !bytes.Equal(assembled.Bytes(), source[parent.Range.StartByte:parent.Range.EndByte]) {
		t.Fatal("source bytes were lost or duplicated")
	}
	if err := model.ValidateCompleteFragmentSequence(parent, fragments, source, 65536); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(*fragments[0].Content, "\n") {
		t.Fatal("newline was not preferred")
	}
}

func TestSourceFragmentsRejectInvalidProgressAndRevocation(t *testing.T) {
	ctx, slice, item, source := fragmentFixture(t)
	parent := item.SourceRefs[0]
	emoji := int64(bytes.Index(source, []byte("😀")))
	if emoji < 0 {
		t.Fatal("missing Unicode fixture")
	}
	for _, tc := range []struct {
		name   string
		id     string
		offset int64
		limit  int
		want   error
	}{
		{"unknown item", "unknown", parent.Range.StartByte, 20, ErrInput},
		{"split rune offset", item.ID, emoji + 1, 20, ErrInput},
		{"completed parent", item.ID, parent.Range.EndByte, 20, ErrInput},
		{"zero budget", item.ID, emoji, 0, ErrInput},
		{"cannot fit rune", item.ID, emoji, 1, ErrBudgetTooSmall},
		{"whole parent is not a fragment", item.ID, parent.Range.StartByte, len(source), ErrInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := NextSourceFragment(ctx, slice, tc.id, tc.offset, tc.limit); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	calls := 0
	slice.verifyNoRecords = func(context.Context, model.ScopeVector) (bool, error) {
		calls++
		return calls == 1, nil
	}
	if _, _, _, err := NextSourceFragment(ctx, slice, item.ID, parent.Range.StartByte, 20); !errors.Is(err, ErrAuthority) || calls != 2 {
		t.Fatalf("missing final authorization check: calls=%d err=%v", calls, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, _, err := NextSourceFragment(cancelled, slice, item.ID, parent.Range.StartByte, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}
