// Package http binds captured OpenAPI declarations to explicit static code evidence.
package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contracts/schema"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type CodeCapture struct {
	Ref   model.SourceRef
	Bytes []byte
}

// Binding comes from the owner of a static provider that already verified
// symbol membership and the explicit handler or client site. The analyzer
// checks the route tuple, symbol scope and bytes at that site. A caller may
// not use labels or a name search as the source of bindings.
type Binding struct {
	Operation            model.EntityRef
	Symbol               model.SymbolRef
	Evidence             model.SourceRef
	Role                 string // handler or client
	Method, Server, Path string
}

func AnalyzeCaptured(ctx context.Context, profile schema.Profile, documents []schema.Capture, code []CodeCapture, bindings []Binding) (schema.Analysis, error) {
	if len(code) > 32 || len(bindings) > 512 {
		return schema.Analysis{}, schema.ErrLimit
	}
	out, err := schema.AnalyzeCaptured(ctx, profile, documents)
	if err != nil {
		return schema.Analysis{}, err
	}
	allCode := make(map[string]CodeCapture)
	bytes := 0
	for _, c := range code {
		if err := ctx.Err(); err != nil {
			return schema.Analysis{}, err
		}
		bytes += len(c.Bytes)
		if bytes > 4<<20 {
			return schema.Analysis{}, schema.ErrLimit
		}
		if _, exists := allCode[c.Ref.Path]; exists || scope(c.Ref) != scope(documents[0].Ref) {
			return schema.Analysis{}, schema.ErrCapture
		}
		if err := schema.ValidateCapture(c.Ref, c.Bytes); err != nil {
			return schema.Analysis{}, err
		}
		allCode[c.Ref.Path] = c
	}
	operations := make(map[model.EntityRef]schema.Operation)
	type identity struct{ identifier, version string }
	count := make(map[identity]int)
	for _, op := range out.Operations {
		operations[op.Entity.Ref] = op
		count[identity{op.Entity.Key.Identifier, op.Entity.Key.Version}]++
	}
	gap := func(code string, ref model.SourceRef) error {
		if len(out.Coverage.Gaps) >= 512 {
			return schema.ErrLimit
		}
		out.Coverage.Gaps = append(out.Coverage.Gaps, model.Gap{Code: code,
			Reason:     "The declaration and a uniquely verified static code site could not be matched.",
			SourceRefs: []model.SourceRef{ref}})
		return nil
	}
	byID := make(map[string]Binding)
	for _, binding := range bindings {
		if err := ctx.Err(); err != nil {
			return schema.Analysis{}, err
		}
		capture, ok := allCode[binding.Evidence.Path]
		if !ok || capture.Ref.ContentHash != binding.Evidence.ContentHash || scope(capture.Ref) != scope(binding.Evidence) ||
			binding.Evidence.Range.EndByte <= binding.Evidence.Range.StartByte ||
			binding.Evidence.ValidateCapturedBytes(capture.Bytes) != nil {
			return schema.Analysis{}, schema.ErrCapture
		}
		op, found := operations[binding.Operation]
		if !found || !op.Bindable || count[identity{op.Entity.Key.Identifier, op.Entity.Key.Version}] != 1 ||
			binding.Symbol.GraphIdentity != op.Entity.Ref.GraphIdentity ||
			binding.Symbol.Kind != "symbol" || binding.Symbol.SymbolID == "" ||
			(binding.Role != "handler" && binding.Role != "client") ||
			op.Method != binding.Method || op.Path != binding.Path || op.Server != binding.Server {
			if err := gap("CONTRACT_HTTP_BINDING_MISMATCH", binding.Evidence); err != nil {
				return schema.Analysis{}, err
			}
			continue
		}
		id := bindingID(binding)
		byID[id] = binding
	}
	ordered := make([]string, 0, len(byID))
	handlers := make(map[model.EntityRef]int)
	for id, b := range byID {
		ordered = append(ordered, id)
		if b.Role == "handler" {
			handlers[b.Operation]++
		}
	}
	slices.Sort(ordered)
	for _, id := range ordered {
		b := byID[id]
		if b.Role == "handler" && handlers[b.Operation] > 1 {
			continue
		}
		if len(out.Batch.Links) >= 4096 {
			return schema.Analysis{}, schema.ErrLimit
		}
		op := operations[b.Operation]
		link := contracts.Link{LinkID: id, Kind: "http_" + b.Role + "_binding", Source: &b.Operation, Target: &b.Symbol,
			ResolutionStatus: "exact_in_supported_scope", Evidence: model.EvidenceRef{EvidenceID: id,
				Origin: "static_analyzer", Kind: "type_resolved", SourceRefs: []model.SourceRef{op.Entity.Declaration, b.Evidence}}}
		out.Batch.Links = append(out.Batch.Links, link)
	}
	for _, op := range out.Operations {
		switch handlers[op.Entity.Ref] {
		case 0:
			if err := gap("CONTRACT_HTTP_HANDLER_UNVERIFIED", op.Entity.Declaration); err != nil {
				return schema.Analysis{}, err
			}
		case 1:
		default:
			if err := gap("CONTRACT_HTTP_HANDLER_AMBIGUOUS", op.Entity.Declaration); err != nil {
				return schema.Analysis{}, err
			}
		}
	}
	if len(out.Coverage.Gaps) != 0 {
		out.Coverage.Status = "partial"
	}
	status := "supported_scope_complete"
	if out.Coverage.Status == "partial" {
		status = "partial"
	}
	out.Coverage.Capabilities = append(out.Coverage.Capabilities, model.Capability{Name: "explicit_http_code_binding",
		ProviderID: "mce4d.contracts", ProviderVersion: "1", Status: status})
	if err := schema.ValidateBatch(out.Batch); err != nil {
		return schema.Analysis{}, err
	}
	return out, ctx.Err()
}

func scope(ref model.SourceRef) model.GraphIdentity {
	return model.GraphIdentity{WorktreeID: ref.WorktreeID, ProfileID: ref.ProfileID, GenerationID: ref.GenerationID}
}

func bindingID(binding Binding) string {
	ref := binding.Evidence
	values := []string{"http-code-binding-v1", binding.Operation.EntityID, binding.Role, binding.Symbol.SymbolID, ref.Path, ref.ContentHash,
		strconv.FormatInt(ref.Range.StartByte, 10), strconv.FormatInt(ref.Range.EndByte, 10)}
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}
