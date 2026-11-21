// Package ai reads declared AI and ML metadata from verified captures.
package ai

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	lineageFormat = "mce4d-ai-lineage-v1"
	metadataBytes = 64 << 10
	scopeBytes    = 256 << 10
)

var (
	ErrMetadata    = errors.New("invalid AI lineage metadata")
	ErrScope       = errors.New("AI lineage scope changed")
	ErrUnavailable = errors.New("approved AI lineage metadata is unavailable")
)

//go:embed lineage.v1.schema.json
var lineageSchema []byte

var schema = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	var document jsonschema.Schema
	if err := json.Unmarshal(lineageSchema, &document); err != nil {
		return nil, err
	}
	return document.Resolve(nil)
})

type Metadata struct {
	Revision           string   `json:"revision,omitempty"`
	Digest             string   `json:"digest,omitempty"`
	SchemaDigest       string   `json:"schema_digest,omitempty"`
	InputSchemaDigest  string   `json:"input_schema_digest,omitempty"`
	OutputSchemaDigest string   `json:"output_schema_digest,omitempty"`
	SplitRole          string   `json:"split_role,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	ArtifactID         string   `json:"artifact_id,omitempty"`
	Dimension          *int64   `json:"dimension,omitempty"`
	Metric             string   `json:"metric,omitempty"`
	MetricName         string   `json:"metric_name,omitempty"`
	Threshold          string   `json:"threshold,omitempty"`
	FeatureNames       []string `json:"feature_names,omitempty"`
	ChunkSize          *int64   `json:"chunk_size,omitempty"`
	Overlap            *int64   `json:"overlap,omitempty"`
	Unit               string   `json:"unit,omitempty"`
	TimeoutMS          *int64   `json:"timeout_ms,omitempty"`
	RetryLimit         *int64   `json:"retry_limit,omitempty"`
	ToolScopes         []string `json:"tool_scopes,omitempty"`
}

type Node struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Metadata Metadata `json:"metadata"`
}

type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type declaration struct {
	Format string `json:"format"`
	Nodes  []Node `json:"nodes"`
	Links  []Link `json:"links"`
}

type Selection struct {
	Scope     model.ScopeVector
	ServiceID *string
	Source    model.SourceRef
}

type Report struct {
	Scope     model.ScopeVector
	Source    model.SourceRef
	ServiceID *string
	Nodes     []Node
	Links     []Link
	Gaps      []model.Gap
	Partial   bool
}

// Reader accepts only exact authorized metadata source bytes. No callback is
// supplied to resolve artifact IDs or run models, tools or repository code.
type Reader struct {
	Observe    func(context.Context, model.ScopeVector) (model.ScopeVector, error)
	ReadSource func(context.Context, model.ScopeMember, model.SourceRef, int) ([]byte, error)
}

func (r Reader) Resolve(ctx context.Context, selection Selection) (Report, error) {
	if ctx == nil || r.Observe == nil || r.ReadSource == nil ||
		!strings.HasSuffix(selection.Source.Path, ".lineage.json") || selection.Source.Range.StartByte != 0 ||
		selection.Source.Range.EndByte < 1 || selection.Source.Range.EndByte > metadataBytes ||
		selection.Scope.Kind == "federated" && selection.ServiceID == nil {
		return Report{}, ErrMetadata
	}
	if model.VerifyScopeVectorDigest(selection.Scope, scopeBytes) != nil {
		return Report{}, ErrScope
	}
	scope, err := model.NormalizeScopeVector(selection.Scope, scopeBytes)
	if err != nil {
		return Report{}, ErrScope
	}
	if err := r.recheck(ctx, scope); err != nil {
		return Report{}, err
	}
	member, err := model.MatchPinnedSource(scope, selection.Source, selection.ServiceID, scopeBytes)
	if err != nil {
		return Report{}, ErrUnavailable
	}
	raw, err := r.ReadSource(ctx, member, selection.Source, metadataBytes)
	if err != nil {
		return Report{}, err
	}
	if len(raw) != int(selection.Source.Range.EndByte) || selection.Source.ValidateCapturedBytes(raw) != nil {
		return Report{}, ErrMetadata
	}
	body, err := decode(raw)
	if err != nil {
		return Report{}, err
	}
	out := Report{Scope: scope, Source: selection.Source, ServiceID: member.ServiceID,
		Nodes: body.Nodes, Links: []Link{}, Gaps: []model.Gap{}}
	if err := out.validateLinks(body.Links); err != nil {
		return Report{}, err
	}
	if len(scope.UnavailableMembers) != 0 {
		out.gap("LINEAGE_MEMBER_UNAVAILABLE", "Some registered scope members are unavailable.")
	}
	if err := r.recheck(ctx, scope); err != nil {
		return Report{}, err
	}
	return out, nil
}

func decode(raw []byte) (declaration, error) {
	if _, err := model.DecodeIdentityJSON(raw, metadataBytes); err != nil {
		return declaration{}, ErrMetadata
	}
	resolved, err := schema()
	if err != nil {
		return declaration{}, ErrMetadata
	}
	var body declaration
	var value any
	if json.Unmarshal(raw, &value) != nil || resolved.Validate(value) != nil ||
		json.Unmarshal(raw, &body) != nil || body.Format != lineageFormat {
		return declaration{}, ErrMetadata
	}
	slices.SortFunc(body.Nodes, func(a, b Node) int { return strings.Compare(a.ID, b.ID) })
	for i := 1; i < len(body.Nodes); i++ {
		if body.Nodes[i-1].ID == body.Nodes[i].ID {
			return declaration{}, ErrMetadata
		}
	}
	return body, nil
}

func (r Reader) recheck(ctx context.Context, scope model.ScopeVector) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	seen, err := r.Observe(ctx, scope)
	if err != nil {
		return err
	}
	if model.ComparePinnedScope(scope, seen, scopeBytes) != nil {
		return ErrScope
	}
	return ctx.Err()
}

func (out *Report) gap(code, reason string) {
	for _, item := range out.Gaps {
		if item.Code == code {
			return
		}
	}
	out.Partial = true
	out.Gaps = append(out.Gaps, model.Gap{Code: code, Reason: reason, SourceRefs: []model.SourceRef{out.Source}})
}

func (out *Report) validateLinks(links []Link) error {
	nodes := map[string]Node{}
	connected := map[string]bool{}
	for _, node := range out.Nodes {
		nodes[node.ID] = node
		if missingMetadata(node) {
			out.gap("LINEAGE_METADATA_MISSING", "A declaration omitted metadata required for full lineage coverage.")
		}
		if node.Kind == "model" && node.Metadata.Revision == "" {
			out.gap("LINEAGE_MODEL_UNVERSIONED", "The declaration names a model without a verified revision.")
		}
		if node.Kind == "chunker" && node.Metadata.ChunkSize != nil && node.Metadata.Overlap != nil &&
			*node.Metadata.Overlap >= *node.Metadata.ChunkSize {
			out.gap("LINEAGE_CHUNK_SETTINGS_INCONSISTENT", "The declared overlap is not smaller than the chunk size.")
		}
	}
	seen := map[Link]bool{}
	for _, link := range links {
		if seen[link] || link.From == link.To {
			return ErrMetadata
		}
		seen[link] = true
		from, fromOK := nodes[link.From]
		to, toOK := nodes[link.To]
		if !fromOK || !toOK {
			out.gap("LINEAGE_ENDPOINT_UNKNOWN", "A declared dependency has no captured metadata for an endpoint.")
			continue
		}
		if !slices.Contains(compatibleTargets[from.Kind], to.Kind) || !validLinkRole(link.Kind, to.Kind) {
			out.gap("LINEAGE_LINK_UNSUPPORTED", "A declaration has a dependency outside the supported component pairs.")
			continue
		}
		if from.Kind == "embedding" && to.Kind == "vector_index" {
			a, b := from.Metadata, to.Metadata
			if a.Dimension != nil && b.Dimension != nil && *a.Dimension != *b.Dimension {
				out.gap("LINEAGE_DIMENSION_MISMATCH", "The declared embedding and vector index have different dimensions.")
			}
			if a.Metric != "" && b.Metric != "" && a.Metric != b.Metric {
				out.gap("LINEAGE_METRIC_MISMATCH", "The declared embedding and vector index use different metrics.")
			}
		}
		out.Links = append(out.Links, link)
		connected[link.From], connected[link.To] = true, true
	}
	for _, node := range out.Nodes {
		if !connected[node.ID] {
			out.gap("LINEAGE_NODE_UNLINKED", "Some captured metadata has no verified dependency declaration.")
			break
		}
	}
	if hasCycle(out.Nodes, out.Links) {
		return ErrMetadata
	}
	slices.SortFunc(out.Links, compareLink)
	return nil
}

func validLinkRole(kind, target string) bool {
	switch kind {
	case "dependency":
		return true
	case "transformation":
		return target == "preprocessing" || target == "features" || target == "chunker" || target == "embedding" || target == "vector_index"
	case "training_input":
		return target == "training"
	case "evaluation_input":
		return target == "evaluation"
	case "serves":
		return target == "serving"
	case "fallback":
		return target == "fallback"
	default:
		return false
	}
}

var compatibleTargets = map[string][]string{
	"dataset":       {"split", "preprocessing", "training", "embedding", "evaluation"},
	"split":         {"preprocessing", "training", "embedding", "evaluation"},
	"preprocessing": {"features", "training", "inference"},
	"features":      {"training", "inference"},
	"training":      {"model"},
	"model":         {"inference", "serving", "evaluation", "fallback"},
	"inference":     {"serving", "evaluation", "output_schema", "fallback"},
	"prompt":        {"tokenizer", "chunker", "model", "inference"},
	"tokenizer":     {"chunker", "inference"},
	"chunker":       {"embedding", "retrieval"},
	"embedding":     {"vector_index", "retrieval", "evaluation"},
	"vector_index":  {"retrieval"},
	"retrieval":     {"reranker", "model", "inference", "evaluation"},
	"reranker":      {"model", "inference", "evaluation"},
	"output_schema": {"serving", "evaluation"},
	"evaluation":    {"serving", "fallback"},
	"tool_scope":    {"inference", "serving"},
	"fallback":      {"serving"},
}

func missingMetadata(node Node) bool {
	md := node.Metadata
	switch node.Kind {
	case "dataset":
		return md.Revision == "" || md.Digest == "" || md.SchemaDigest == ""
	case "split":
		return md.SplitRole == "" || md.Digest == ""
	case "features":
		return len(md.FeatureNames) == 0 || md.SchemaDigest == ""
	case "model":
		return md.Provider == "" || md.Revision == ""
	case "prompt":
		return md.Digest == ""
	case "chunker":
		return md.ChunkSize == nil || md.Overlap == nil || md.Unit == ""
	case "embedding":
		return md.Revision == "" || md.Dimension == nil || md.Metric == ""
	case "vector_index":
		return md.Dimension == nil || md.Metric == "" || md.SchemaDigest == ""
	case "evaluation":
		return md.MetricName == "" || md.Threshold == ""
	case "serving", "inference":
		return md.Revision == "" || md.InputSchemaDigest == "" || md.OutputSchemaDigest == ""
	case "output_schema":
		return md.SchemaDigest == ""
	case "tool_scope":
		return len(md.ToolScopes) == 0
	default:
		return md.Revision == ""
	}
}

func compareLink(a, b Link) int {
	for _, pair := range [][2]string{{a.From, b.From}, {a.To, b.To}, {a.Kind, b.Kind}} {
		if order := strings.Compare(pair[0], pair[1]); order != 0 {
			return order
		}
	}
	return 0
}

func hasCycle(nodes []Node, links []Link) bool {
	degrees := map[string]int{}
	edges := map[string][]string{}
	for _, link := range links {
		degrees[link.To]++
		edges[link.From] = append(edges[link.From], link.To)
	}
	queue := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if degrees[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, next := range edges[queue[i]] {
			degrees[next]--
			if degrees[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	return len(queue) != len(nodes)
}
