package http

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func fixtureRef(name, text string) model.SourceRef {
	return model.SourceRef{Path: name, WorktreeID: "01234567-0123-4123-8123-012345678901",
		ProfileID: strings.Repeat("b", 64), GenerationID: "11234567-0123-4123-8123-012345678901",
		ContentHash: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), Range: model.ByteRange{EndByte: int64(len(text))}}
}

func fixture(security string) (schema.Profile, []schema.Capture, []CodeCapture) {
	content := `{"openapi":"3.1.1","info":{"version":"v1"},"servers":[{"url":"https://inventory.invalid/v1"}],` + security +
		`"paths":{"/stock":{"get":{"operationId":"stock","responses":{"200":{"description":"OK"}}}}}}`
	src := "package fixture\n// Stock is a synthetic handler\nfunc Stock() {}\n"
	return schema.Profile{ServiceID: "inventory", Environment: "fixture", Namespace: "inventory.local"},
		[]schema.Capture{{Ref: fixtureRef("api.json", content), Kind: "openapi", Version: "v1", Bytes: []byte(content)}},
		[]CodeCapture{{Ref: fixtureRef("stock.go", src), Bytes: []byte(src)}}
}

func hasGap(out schema.Analysis, code string) bool {
	for _, gap := range out.Coverage.Gaps {
		if gap.Code == code {
			return true
		}
	}
	return false
}

func proof(op schema.Operation, code CodeCapture, id string) Binding {
	evidence := code.Ref
	evidence.Range.StartByte = int64(strings.Index(string(code.Bytes), "func"))
	return Binding{Operation: op.Entity.Ref, Symbol: model.SymbolRef{Kind: "symbol", GraphIdentity: op.Entity.Ref.GraphIdentity, SymbolID: id},
		Evidence: evidence, Role: "handler", Method: op.Method, Server: op.Server, Path: op.Path}
}

func TestC060OpenAPIWithoutHandlerAndExplicitTypedBinding(t *testing.T) {
	ctx := context.Background()
	profile, docs, code := fixture("")
	declared, err := AnalyzeCaptured(ctx, profile, docs, code, nil)
	if err != nil || len(declared.Operations) != 1 || len(declared.Batch.Links) != 0 ||
		!hasGap(declared, "CONTRACT_HTTP_HANDLER_UNVERIFIED") {
		t.Fatalf("same-name-only fixture bound: %+v, %v", declared, err)
	}
	binding := proof(declared.Operations[0], code[0], "verified-stock")
	bound, err := AnalyzeCaptured(ctx, profile, docs, code, []Binding{binding, binding})
	if err != nil || len(bound.Batch.Links) != 1 || len(bound.Coverage.Gaps) != 0 {
		t.Fatalf("exact binding not emitted: %+v, %v", bound, err)
	}
	link := bound.Batch.Links[0]
	if bound.Operations[0].Entity.Plane != "intended" || link.Kind != "http_handler_binding" ||
		link.Evidence.Kind != "type_resolved" || link.ResolutionStatus != "exact_in_supported_scope" ||
		len(link.Evidence.SourceRefs) != 2 || link.Evidence.SourceRefs[1] != binding.Evidence {
		t.Fatalf("binding lost typed evidence: %+v", link)
	}
	client := binding
	client.Role = "client"
	bound, err = AnalyzeCaptured(ctx, profile, docs, code, []Binding{client})
	if err != nil || len(bound.Batch.Links) != 1 || bound.Batch.Links[0].Kind != "http_client_binding" ||
		!hasGap(bound, "CONTRACT_HTTP_HANDLER_UNVERIFIED") {
		t.Fatalf("client misclassified: %+v, %v", bound, err)
	}
}

func TestC060HTTPRouteScopeAmbiguityAndMissingCodeBytes(t *testing.T) {
	ctx := context.Background()
	profile, docs, code := fixture("")
	declared, err := schema.AnalyzeCaptured(ctx, profile, docs)
	if err != nil || len(declared.Operations) != 1 {
		t.Fatalf("failed to read fixture: %+v %v", declared, err)
	}
	valid := proof(declared.Operations[0], code[0], "handler-stock")
	wrong := valid
	wrong.Server = "https://inventory.invalid/v2"
	wrongScope := valid
	wrongScope.Symbol.GenerationID = "21234567-0123-4123-8123-012345678901"
	out, err := AnalyzeCaptured(ctx, profile, docs, code, []Binding{wrong, wrongScope})
	if err != nil || len(out.Batch.Links) != 0 || !hasGap(out, "CONTRACT_HTTP_BINDING_MISMATCH") {
		t.Fatalf("different route or generation linked: %+v %v", out, err)
	}
	alternate := valid
	alternate.Symbol.SymbolID = "alternate-handler"
	out, err = AnalyzeCaptured(ctx, profile, docs, code, []Binding{valid, alternate})
	if err != nil || len(out.Batch.Links) != 0 || !hasGap(out, "CONTRACT_HTTP_HANDLER_AMBIGUOUS") {
		t.Fatalf("ambiguous handler linked: %+v %v", out, err)
	}
	if _, err := AnalyzeCaptured(ctx, profile, docs, nil, []Binding{valid}); !errors.Is(err, schema.ErrCapture) {
		t.Fatalf("binding accepted without source bytes: %v", err)
	}
	wrong = valid
	wrong.Evidence.ContentHash = strings.Repeat("0", 64)
	if _, err := AnalyzeCaptured(ctx, profile, docs, code, []Binding{wrong}); !errors.Is(err, schema.ErrCapture) {
		t.Fatalf("stale binding accepted: %v", err)
	}
}

func TestC060HTTPAuthPolicyDoesNotBecomeVerifiedImplementation(t *testing.T) {
	ctx := context.Background()
	profile, docs, code := fixture(`"security":[{"factory":["stock"]}],`)
	declared, err := schema.AnalyzeCaptured(ctx, profile, docs)
	if err != nil || len(declared.Operations) != 1 || declared.Operations[0].Bindable {
		t.Fatalf("unverified policy declared bindable: %+v %v", declared, err)
	}
	out, err := AnalyzeCaptured(ctx, profile, docs, code, []Binding{proof(declared.Operations[0], code[0], "stock-handler")})
	if err != nil || len(out.Batch.Links) != 0 || !hasGap(out, "CONTRACT_HTTP_BINDING_CONTEXT_UNRESOLVED") {
		t.Fatalf("protected route policy inferred: %+v %v", out, err)
	}
}
