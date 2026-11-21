package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	worktree        = "11111111-1111-4111-8111-111111111111"
	generation      = "22222222-2222-4222-8222-222222222222"
	otherGeneration = "33333333-3333-4333-8333-333333333333"
)

func adapterSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/domain-adapter.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile("../../Maps-Context-Engine-4D_SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(bytes.ReplaceAll(spec, []byte("\r\n"), []byte("\n")))
	block := strings.Split(text, "<!-- BEGIN MCE4D_DOMAIN_ADAPTER_V1 -->\n```json\n")
	if len(block) != 2 || strings.TrimSpace(strings.SplitN(block[1], "\n```\n<!-- END MCE4D_DOMAIN_ADAPTER_V1 -->", 2)[0]) != strings.TrimSpace(string(raw)) {
		t.Fatal("domain adapter schema differs from S43")
	}
	var schema, core jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../contracts/mce4d.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &core); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{Loader: func(uri *url.URL) (*jsonschema.Schema, error) {
		if uri.String() != core.ID {
			return nil, fmt.Errorf("unknown schema reference %s", uri)
		}
		return &core, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func adapterFixture(t *testing.T, protocol, namespace, identifier string) []byte {
	t.Helper()
	id := model.GraphIdentity{WorktreeID: worktree, ProfileID: strings.Repeat("a", 64), GenerationID: generation}
	ref := model.EntityRef{Kind: "entity", GraphIdentity: id, EntityID: "operation"}
	source := model.SourceRef{WorktreeID: worktree, ProfileID: id.ProfileID, GenerationID: generation, Path: "decl.txt",
		ContentHash: strings.Repeat("b", 64), Range: model.ByteRange{StartByte: 2, EndByte: 11}}
	unit, currency := "credit", "USD"
	precision := int64(4)
	item := Batch{SchemaVersion: 1,
		Entities: []Entity{{Ref: ref, Kind: "operation", Key: Key{Environment: "fixture", Protocol: protocol, Namespace: namespace,
			Identifier: identifier, Version: "v1"}, Plane: "intended", Declaration: source}},
		Fields: []Field{{Entity: ref, FieldPath: "amount", Direction: "input", Type: "decimal", Unit: &unit, Currency: &currency,
			Precision: &precision, Declaration: source}},
		Links: []Link{{LinkID: "binding", Source: &model.SymbolRef{Kind: "symbol", GraphIdentity: id, SymbolID: "caller"}, Target: &ref,
			Kind: "uses", Evidence: model.EvidenceRef{EvidenceID: "declaration", Origin: "static_analyzer", Kind: "explicit_document",
				SourceRefs: []model.SourceRef{source}}, ResolutionStatus: "exact_in_supported_scope"}},
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAdapterDomainsAndIdentity(t *testing.T) {
	schema := adapterSchema(t)
	keys := map[string]bool{}
	for _, test := range [][3]string{{"mysql", "innodb-main", "account"}, {"mysql", "ndb-other", "account"},
		{"http", "catalog-api", "GET /items"}, {"http", "billing-api", "GET /items"},
		{"json_schema", "catalog-api", "request"}, {"protobuf", "catalog-api", "request"},
		{"asyncapi", "broker-a", "orders"}, {"asyncapi", "broker-b", "orders"}, {"cache", "redis-a", "orders"}} {
		raw := adapterFixture(t, test[0], test[1], test[2])
		var wire any
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(wire); err != nil {
			t.Fatal(err)
		}
		batch, err := DecodeBatch(raw)
		if err != nil {
			t.Fatal(err)
		}
		key, err := batch.Entities[0].QualifiedKey()
		if err != nil || keys[key] {
			t.Fatalf("scope collision: %v %v", test, err)
		}
		keys[key] = true
		field := batch.Fields[0]
		if field.Direction != "input" || field.Nullable != nil || *field.Unit != "credit" ||
			*field.Currency != "USD" || *field.Precision != 4 {
			t.Fatal("declared field metadata changed")
		}
		entity := batch.Entities[0]
		entity.Declaration.ContentHash = strings.Repeat("c", 64)
		changedKey, err := entity.QualifiedKey()
		if err != nil || changedKey == key {
			t.Fatal("source revision did not change contract identity")
		}
	}
}

func TestAdapterShapeAndEvidenceDenials(t *testing.T) {
	schema := adapterSchema(t)
	raw := string(adapterFixture(t, "http", "public-api", "GET /items"))
	for _, bad := range []string{
		strings.Replace(raw, `"schema_version":1`, `"schema_version":2`, 1),
		strings.Replace(raw, `"schema_version":1`, `"schema_version":1,"extra":1`, 1),
		strings.Replace(raw, `"kind":"symbol"`, `"kind":"file"`, 1),
		strings.Replace(raw, `"namespace":"public-api"`, `"namespace":"https://invalid.example/path"`, 1),
		strings.Replace(raw, `"nullable":null,`, "", 1),
	} {
		var wire any
		if err := json.Unmarshal([]byte(bad), &wire); err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeBatch([]byte(bad)); err == nil || schema.Validate(wire) == nil {
			t.Fatal("closed adapter shape accepted an invalid value")
		}
	}
	for _, bad := range []string{
		strings.Replace(raw, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(raw, `"generation_id":"`+generation+`"`, `"generation_id":"`+otherGeneration+`"`, 1),
		strings.Replace(raw, `"kind":"explicit_document"`, `"kind":"text_match"`, 1),
		strings.Replace(raw, `"origin":"static_analyzer","kind":"explicit_document"`, `"origin":"runtime_observation","kind":"runtime_observed"`, 1),
		strings.Replace(raw, `"identifier":"GET /items"`, `"identifier":"GET /items\nmutated"`, 1),
	} {
		if _, err := DecodeBatch([]byte(bad)); err == nil {
			t.Fatal("accepted ambiguous identity or unsupported exact evidence")
		}
	}
}

func TestAdapterPinnedEntitySelection(t *testing.T) {
	id := model.GraphIdentity{WorktreeID: worktree, ProfileID: strings.Repeat("a", 64), GenerationID: generation}
	service, otherService, missing := "first", "second", "missing"
	member := model.ScopeMember{WorktreeID: id.WorktreeID, ProfileID: id.ProfileID, GenerationID: id.GenerationID,
		SnapshotDigest: id.ProfileID, PolicyDigest: id.ProfileID, PolicyEpoch: 1, BindingRevision: 1,
		IndexIncarnation: generation, RecordsIncarnation: generation, ServiceID: &service}
	other := member
	other.ServiceID = &otherService
	revision := int64(1)
	vector := model.ScopeVector{Kind: "federated", GroupID: &id.WorktreeID, GroupRevision: &revision, CatalogIncarnation: worktree,
		Members: []model.ScopeMember{member, other}, UnavailableMembers: []model.MemberUnavailable{
			{ServiceID: &missing, WorktreeID: &id.WorktreeID, ProfileID: &id.ProfileID, Status: "index_missing", ReasonCode: "INDEX_MISSING"},
			{Status: "access_denied", ReasonCode: "ACCESS_DENIED"},
		}}
	digest := func(vector model.ScopeVector) model.ScopeVector {
		var err error
		vector.VectorDigest, err = model.ComputeScopeVectorDigest(vector, 65536)
		if err != nil {
			t.Fatal(err)
		}
		return vector
	}
	vector = digest(vector)
	selector := model.RefSelector{Kind: "ref", Ref: &model.EntityRef{Kind: "entity", GraphIdentity: id, EntityID: "same"}, ServiceID: &otherService}
	selected, err := MatchPinnedEntity(vector, vector, selector)
	if err != nil || *selected.ServiceID != otherService {
		t.Fatalf("exact selection %v", err)
	}
	selector.ServiceID = nil
	if _, err := MatchPinnedEntity(vector, vector, selector); !errors.Is(err, model.ErrAmbiguousMember) {
		t.Fatalf("service ambiguity %v", err)
	}
	unavailable := vector
	unavailable.Members = []model.ScopeMember{}
	unavailable = digest(unavailable)
	if _, err := MatchPinnedEntity(unavailable, unavailable, selector); !errors.Is(err, model.ErrMemberNotInScope) {
		t.Fatalf("unavailable selection %v", err)
	}
	selector.ServiceID = &otherService
	for _, name := range []string{"active", "catalog", "group", "newly-indexed", "digest"} {
		t.Run(name, func(t *testing.T) {
			observed := vector
			observed.Members = append([]model.ScopeMember{}, vector.Members...)
			switch name {
			case "active":
				observed.Members[0].GenerationID = otherGeneration
			case "catalog":
				observed.CatalogIncarnation = otherGeneration
			case "group":
				rev := revision + 1
				observed.GroupRevision = &rev
			case "newly-indexed":
				added := member
				added.ServiceID = &missing
				observed.Members = append(observed.Members, added)
				observed.UnavailableMembers = vector.UnavailableMembers[1:]
			}
			if name == "digest" {
				observed.VectorDigest = strings.Repeat("d", 64)
			} else {
				observed = digest(observed)
			}
			if _, err := MatchPinnedEntity(vector, observed, selector); err == nil {
				t.Fatal("accepted changed vector")
			}
		})
	}
}
