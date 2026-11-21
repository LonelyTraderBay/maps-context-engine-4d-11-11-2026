package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func uid(i int) string { return fmt.Sprintf("81000000-0000-4000-8000-%012d", i) }

func hash(raw []byte) string {
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:])
}

func pointer[T any](value T) *T { return &value }

func lineageFixture(after bool) declaration {
	digest := hash([]byte("captured schema"))
	nodes := []Node{
		{ID: "training-data", Kind: "dataset", Metadata: Metadata{Revision: "r1", Digest: hash([]byte("dataset one")), SchemaDigest: digest}},
		{ID: "split", Kind: "split", Metadata: Metadata{Digest: hash([]byte("train split")), SplitRole: "train"}},
		{ID: "prepare", Kind: "preprocessing", Metadata: Metadata{Revision: "r1", Digest: digest}},
		{ID: "features", Kind: "features", Metadata: Metadata{SchemaDigest: digest, FeatureNames: []string{"price", "quantity"}}},
		{ID: "train", Kind: "training", Metadata: Metadata{Revision: "r1", Digest: digest}},
		{ID: "prompt", Kind: "prompt", Metadata: Metadata{Revision: "r1", Digest: hash([]byte("template one"))}},
		{ID: "token", Kind: "tokenizer", Metadata: Metadata{Revision: "tok1"}},
		{ID: "chunk", Kind: "chunker", Metadata: Metadata{ChunkSize: pointer(int64(256)), Overlap: pointer(int64(16)), Unit: "tokens"}},
		{ID: "embed", Kind: "embedding", Metadata: Metadata{Revision: "r1", Dimension: pointer(int64(384)), Metric: "cosine"}},
		{ID: "index", Kind: "vector_index", Metadata: Metadata{Dimension: pointer(int64(384)), Metric: "cosine", SchemaDigest: digest}},
		{ID: "retriever", Kind: "retrieval", Metadata: Metadata{Revision: "r1"}},
		{ID: "rerank", Kind: "reranker", Metadata: Metadata{Revision: "r1"}},
		{ID: "model", Kind: "model", Metadata: Metadata{Provider: "fixture/model", ArtifactID: "fixture-artifact"}},
		{ID: "infer", Kind: "inference", Metadata: Metadata{Revision: "r1", InputSchemaDigest: digest, OutputSchemaDigest: digest}},
		{ID: "output", Kind: "output_schema", Metadata: Metadata{SchemaDigest: digest}},
		{ID: "eval", Kind: "evaluation", Metadata: Metadata{MetricName: "recall_at_5", Threshold: "0.80"}},
		{ID: "serve", Kind: "serving", Metadata: Metadata{Revision: "r1", InputSchemaDigest: digest, OutputSchemaDigest: digest, TimeoutMS: pointer(int64(900))}},
	}
	links := []Link{
		{"training-data", "split", "dependency"}, {"split", "prepare", "transformation"},
		{"prepare", "features", "transformation"}, {"features", "train", "training_input"}, {"train", "model", "dependency"},
		{"prompt", "token", "dependency"}, {"token", "chunk", "transformation"}, {"chunk", "embed", "transformation"},
		{"embed", "index", "transformation"}, {"index", "retriever", "dependency"}, {"retriever", "rerank", "dependency"},
		{"rerank", "infer", "dependency"}, {"model", "infer", "dependency"}, {"infer", "output", "dependency"},
		{"output", "serve", "serves"}, {"infer", "eval", "evaluation_input"}, {"eval", "serve", "serves"},
	}
	if after {
		nodes[0].Metadata.Digest = hash([]byte("dataset two"))
		nodes[5].Metadata.Digest = hash([]byte("template two"))
		nodes[6].Metadata.Revision = "tok2"
		nodes[8].Metadata.Dimension = pointer(int64(768))
		nodes[15].Metadata.Threshold = "0.86"
		links = append(links, Link{"split", "missing-eval-data", "evaluation_input"})
	}
	return declaration{Format: lineageFormat, Nodes: nodes, Links: links}
}

func selection(t *testing.T, content []byte, i int, path string) Selection {
	t.Helper()
	member := model.ScopeMember{WorktreeID: uid(1), ProfileID: hash([]byte("fixture profile")), GenerationID: uid(i + 3),
		PolicyDigest: hash([]byte("fixture policy")), SnapshotDigest: hash(content), PolicyEpoch: 1,
		BindingRevision: 1, RecordEpoch: 1, IndexIncarnation: uid(2), RecordsIncarnation: uid(3)}
	scope := model.ScopeVector{Kind: "single", CatalogIncarnation: uid(4), Members: []model.ScopeMember{member}, UnavailableMembers: []model.MemberUnavailable{}}
	var err error
	scope.VectorDigest, err = model.ComputeScopeVectorDigest(scope, scopeBytes)
	if err != nil {
		t.Fatal(err)
	}
	return Selection{Scope: scope, Source: model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID,
		GenerationID: member.GenerationID, Path: path, ContentHash: hash(content), Range: model.ByteRange{EndByte: int64(len(content))}}}
}

func TestC067LineageDiffReadsOnlyCapturedMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	before, err := json.Marshal(lineageFixture(false))
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(lineageFixture(true))
	if err != nil {
		t.Fatal(err)
	}
	opaque := []byte("inert synthetic checkpoint contents; a lineage reader must never open this file")
	files := map[string][]byte{"before.lineage.json": before, "after.lineage.json": after, "checkpoint.pkl": opaque}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	left := selection(t, before, 1, "before.lineage.json")
	right := selection(t, after, 2, "after.lineage.json")
	checkpoint := selection(t, opaque, 2, "checkpoint.pkl")
	var opened []string
	reader := Reader{
		Observe: func(ctx context.Context, pinned model.ScopeVector) (model.ScopeVector, error) {
			return pinned, ctx.Err()
		},
		ReadSource: func(ctx context.Context, member model.ScopeMember, src model.SourceRef, limit int) ([]byte, error) {
			opened = append(opened, src.Path)
			if src != left.Source && src != right.Source || src.GenerationID != member.GenerationID || limit != metadataBytes {
				return nil, ErrUnavailable
			}
			return os.ReadFile(filepath.Join(root, src.Path))
		},
	}
	out, err := reader.Compare(ctx, left, right)
	if err != nil || !out.Partial || len(out.Changes) != 5 || len(out.Before.Nodes) != 17 || len(out.LinkChanges) != 0 ||
		!reflect.DeepEqual(opened, []string{"before.lineage.json", "after.lineage.json"}) {
		t.Fatalf("lineage diff: %+v %v, opened %v", out, err, opened)
	}
	changed := map[string][]string{}
	for _, item := range out.Changes {
		changed[item.NodeID] = item.Fields
	}
	want := map[string][]string{"training-data": {"digest"}, "prompt": {"digest"}, "token": {"revision"}, "embed": {"dimension"}, "eval": {"threshold"}}
	if !reflect.DeepEqual(changed, want) {
		t.Fatalf("declared changes: %+v", changed)
	}
	gaps := map[string]bool{}
	for _, gap := range out.After.Gaps {
		gaps[gap.Code] = true
		if !reflect.DeepEqual(gap.SourceRefs, []model.SourceRef{right.Source}) {
			t.Fatalf("gap lost capture provenance: %+v", gap)
		}
	}
	for _, code := range []string{"LINEAGE_MODEL_UNVERSIONED", "LINEAGE_DIMENSION_MISMATCH", "LINEAGE_ENDPOINT_UNKNOWN"} {
		if !gaps[code] {
			t.Fatalf("missing diagnostic %s", code)
		}
	}
	for _, id := range []string{"index", "retriever", "rerank", "infer", "serve", "train"} {
		if !slices.ContainsFunc(out.Affected, func(item Affected) bool {
			return item.Side == "after" && item.NodeID == id && item.Source == right.Source
		}) {
			t.Fatalf("missing known consumer %s: %+v", id, out.Affected)
		}
	}
	if report, err := reader.Resolve(ctx, checkpoint); !errors.Is(err, ErrMetadata) || len(report.Nodes) != 0 || len(opened) != 2 {
		t.Fatalf("non-metadata artifact reached reader: %+v %v, opened %+v", report, err, opened)
	}
	for name, original := range files {
		live, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !reflect.DeepEqual(live, original) {
			t.Fatalf("fixture bytes changed %s: %v", name, err)
		}
	}
	t.Log("C067: five metadata changes through retained declarations, verified consumers and dimension gap, zero checkpoint/model reads")
}

func TestC067ClosedShapeAndFinalAuthority(t *testing.T) {
	ctx := context.Background()
	raw, err := json.Marshal(lineageFixture(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range []struct{ from, to string }{
		{`"nodes":`, `"checkpoint_path":"checkpoint.pkl","nodes":`},
		{`"schema_digest":`, `"rows":[{"value":1}],"schema_digest":`},
		{`"dimension":384`, `"dimension":384,"dimension":384`},
		{`"metric":"cosine"`, `"metric":"unknown-metric"`},
	} {
		changed := strings.Replace(string(raw), patch.from, patch.to, 1)
		if changed == string(raw) {
			t.Fatal("test did not modify the input")
		}
		if _, err := decode([]byte(changed)); !errors.Is(err, ErrMetadata) {
			t.Fatalf("out-of-contract payload accepted: %v", err)
		}
	}
	choices := 0
	reader := Reader{
		Observe: func(_ context.Context, pinned model.ScopeVector) (model.ScopeVector, error) {
			choices++
			if choices == 2 {
				return model.ScopeVector{}, ErrScope
			}
			return pinned, nil
		},
		ReadSource: func(context.Context, model.ScopeMember, model.SourceRef, int) ([]byte, error) { return raw, nil },
	}
	request := selection(t, raw, 5, "capture.lineage.json")
	if out, err := reader.Resolve(ctx, request); !errors.Is(err, ErrScope) || len(out.Nodes) != 0 {
		t.Fatalf("metadata leaked when scope changed after capture: %+v %v", out, err)
	}
	request.Scope.VectorDigest = hash([]byte("forged vector"))
	if _, err := reader.Resolve(ctx, request); !errors.Is(err, ErrScope) || choices != 2 {
		t.Fatalf("mismatched digest contacted owner: %v", err)
	}
}
