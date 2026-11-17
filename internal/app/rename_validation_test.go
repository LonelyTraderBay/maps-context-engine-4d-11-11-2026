package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/lsp"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/snapshot"
)

func renameValidationFixture(t *testing.T, uses int) (context.Context, lsp.RenameStoredResult, ValidateCapturedInput, *snapshot.Snapshot) {
	t.Helper()
	ctx, input, _ := validationFixture(t)
	member := input.Scope.Members[0]
	a := "package p\n// ພາສາ 😀\r\ntype Thing struct { Value int }\n"
	b := "package p\nfunc use(t Thing) {\n" + strings.Repeat("_ = t.Value\n", uses) + "}\n"
	manifest := model.CaptureManifest{SchemaVersion: 1, WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, PolicyDigest: member.PolicyDigest, ProviderDigest: strings.Repeat("c", 64), Files: []model.ManifestFile{}, DependencyInputDigests: []string{}}
	files := []snapshot.File{}
	byPath := map[string]snapshot.File{}
	for _, item := range []struct{ name, text string }{{"a.go", a}, {"b.go", b}, {"AGENTS.md", "synthetic rule; target remains read-only\n"}} {
		file, err := snapshot.NewFile(item.name, []byte(item.text), 65536)
		if err != nil {
			t.Fatal(err)
		}
		hash, size := file.ContentHash(), int64(file.Len())
		manifest.Files = append(manifest.Files, model.ManifestFile{Path: item.name, Status: "captured", ContentHash: &hash, ByteLength: &size})
		files = append(files, file)
		byPath[item.name] = file
	}
	captured, err := snapshot.Freeze(manifest, files, 8192, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	input.Scope.Members[0].SnapshotDigest = captured.Manifest().SnapshotDigest
	input.Scope.VectorDigest, err = model.ComputeScopeVectorDigest(input.Scope, 8192)
	if err != nil {
		t.Fatal(err)
	}
	input.CurrentScope = func(context.Context) (model.ScopeVector, error) { return input.Scope, nil }
	input.ResolveCapture = func(context.Context, model.ScopeMember) (*snapshot.Snapshot, error) { return captured, nil }
	input.AuthorizeSource = func(_ context.Context, _ model.ScopeMember, ref model.SourceRef) error {
		file, ok := captured.File(ref.Path)
		if !ok || file.ContentHash() != ref.ContentHash {
			return ErrValidationPrecondition
		}
		return nil
	}
	input.VerifyWorkspace = func(_ context.Context, _ model.ScopeMember, actual *snapshot.Snapshot) (WorkspaceObservation, error) {
		if actual != captured || len(actual.Manifest().Files) != 3 {
			return WorkspaceUnknown, ErrValidationAuthority
		}
		return WorkspaceObservedCurrent, nil // coordinator fixture, not disk evidence
	}
	ref := func(name string, start, end int) model.SourceRef {
		return model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: name, ContentHash: byPath[name].ContentHash(), Range: model.ByteRange{StartByte: int64(start), EndByte: int64(end)}}
	}
	selected := strings.Index(a, "Value")
	payload := lsp.RenameResultPayload{InitialRequest: model.RenameInitialRequest{Selector: model.PositionSelector{Kind: "source_position", Path: "a.go", ContentHash: byPath["a.go"].ContentHash(), ByteOffset: int64(selected)}, NewName: "Renamed", PreserveContracts: true}, ProviderDigest: strings.Repeat("d", 64), FilePreconditions: []model.RenameFilePrecondition{}, Edits: []model.ProposedEdit{}, Conflicts: []model.Gap{}}
	documents := []lsp.CapturedDocument{}
	for _, name := range []string{"a.go", "b.go"} {
		file := byPath[name]
		payload.FilePreconditions = append(payload.FilePreconditions, model.RenameFilePrecondition{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: name, ContentHash: file.ContentHash()})
		documents = append(documents, lsp.CapturedDocument{File: file, Ref: ref(name, 0, file.Len())})
		body := string(file.Bytes())
		for from := 0; ; {
			at := strings.Index(body[from:], "Value")
			if at < 0 {
				break
			}
			at += from
			payload.Edits = append(payload.Edits, model.ProposedEdit{SourceRef: ref(name, at, at+len("Value")), NewText: "Renamed"})
			from = at + len("Value")
		}
	}
	payload.Conflicts, err = lsp.InspectRenameContracts(ctx, documents, payload.Edits, true)
	if err != nil {
		t.Fatal(err)
	}
	result := lsp.RenameStoredResult{Header: lsp.RenameResultHeader{FormatVersion: 1, EngineSchema: 1, WorktreeID: member.WorktreeID, Scope: input.Scope}, Payload: payload}
	stampRenameValidation(t, &result)
	return ctx, result, input, captured
}

func stampRenameValidation(t *testing.T, result *lsp.RenameStoredResult) {
	t.Helper()
	raw, err := json.Marshal(result.Payload)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	result.Header.PayloadHash = hex.EncodeToString(hash[:])
	result.Header.ResultDigest, err = model.RenameResultDigest(result.Header.Scope, result.Payload.InitialRequest, result.Payload.ProviderDigest, result.Payload.FilePreconditions, result.Payload.Edits, result.Payload.Conflicts, 32<<20)
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRenameWorkspaceCovers1025EditsAndUneditedRules(t *testing.T) {
	ctx, result, input, captured := renameValidationFixture(t, 1024)
	if len(result.Payload.Edits) != 1025 {
		t.Fatal("fixture must exercise every occurrence across two files")
	}
	if result.Payload.ProviderDigest == captured.Manifest().ProviderDigest {
		t.Fatal("rename and index providers must be distinct in this fixture")
	}
	before, _ := captured.File("a.go")
	validated, err := ValidateRenameWorkspace(ctx, result, result.Payload.ProviderDigest, input, 32<<20)
	if err != nil || !validated.Data.EditPreconditionValidated || validated.Freshness.Status != "observed_current" {
		t.Fatalf("full rename precondition validation: %+v %v", validated, err)
	}
	after, _ := captured.File("a.go")
	if string(before.Bytes()) != string(after.Bytes()) {
		t.Fatal("validation mutated source")
	}
}

func TestValidateRenameWorkspaceRejectsMissingContractBoundary(t *testing.T) {
	ctx, result, input, _ := renameValidationFixture(t, 1)
	gaps := result.Payload.Conflicts[:0]
	for _, gap := range result.Payload.Conflicts {
		if gap.Code != "IMPLICIT_SERIALIZATION_NAME_CHANGE" {
			gaps = append(gaps, gap)
		}
	}
	result.Payload.Conflicts = gaps
	stampRenameValidation(t, &result) // a matching digest alone is insufficient
	if got, err := ValidateRenameWorkspace(ctx, result, result.Payload.ProviderDigest, input, 32<<20); !errors.Is(err, ErrValidationPrecondition) || got.Data.EditPreconditionValidated {
		t.Fatalf("missing implicit JSON boundary accepted: %v", err)
	}
}

func TestValidateRenameWorkspaceRejectsCapturedMismatchAndSplitUTF8(t *testing.T) {
	for _, name := range []string{"captured_hash", "utf8"} {
		t.Run(name, func(t *testing.T) {
			ctx, result, input, captured := renameValidationFixture(t, 1)
			if name == "captured_hash" {
				hash := strings.Repeat("d", 64)
				result.Payload.FilePreconditions[0].ContentHash = hash
				for i := range result.Payload.Edits {
					if result.Payload.Edits[i].SourceRef.Path == "a.go" {
						result.Payload.Edits[i].SourceRef.ContentHash = hash
					}
				}
			} else {
				file, _ := captured.File("a.go")
				at := strings.Index(string(file.Bytes()), "😀") + 1
				result.Payload.Edits[0].SourceRef.Range = model.ByteRange{StartByte: int64(at), EndByte: int64(at + 1)}
			}
			stampRenameValidation(t, &result)
			if got, err := ValidateRenameWorkspace(ctx, result, result.Payload.ProviderDigest, input, 32<<20); err == nil || got.Data.EditPreconditionValidated {
				t.Fatalf("invalid captured precondition accepted: %v", err)
			}
		})
	}
}

func TestValidateRenameWorkspaceRejectsUneditedChangeAndLateRevocation(t *testing.T) {
	for _, changed := range []bool{false, true} {
		ctx, result, input, _ := renameValidationFixture(t, 1)
		allowed := true
		input.AuthorizeSource = func(context.Context, model.ScopeMember, model.SourceRef) error {
			if !allowed {
				return ErrValidationAuthority
			}
			return nil
		}
		input.VerifyWorkspace = func(context.Context, model.ScopeMember, *snapshot.Snapshot) (WorkspaceObservation, error) {
			if changed {
				return WorkspaceChanged, nil // e.g. an unedited rule/config file
			}
			allowed = false
			return WorkspaceObservedCurrent, nil
		}
		if got, err := ValidateRenameWorkspace(ctx, result, result.Payload.ProviderDigest, input, 32<<20); err == nil || got.Data.EditPreconditionValidated {
			t.Fatal("changed workspace or late authority loss validated a rename")
		}
	}
}

func TestValidateRenameWorkspaceChecksEditsBeyondFirstPage(t *testing.T) {
	ctx, result, input, captured := renameValidationFixture(t, 1024)
	last := len(result.Payload.Edits) - 1
	file, _ := captured.File(result.Payload.Edits[last].SourceRef.Path)
	result.Payload.Edits[last].SourceRef.Range.EndByte = int64(file.Len()) + 1
	stampRenameValidation(t, &result)
	if got, err := ValidateRenameWorkspace(ctx, result, result.Payload.ProviderDigest, input, 32<<20); !errors.Is(err, ErrValidationPrecondition) || got.Data.EditPreconditionValidated {
		t.Fatal("an invalid edit after the 512-edit page boundary was ignored")
	}
}

func TestValidateRenameWorkspacePreservesLiteralConstraint(t *testing.T) {
	ctx, result, input, _ := renameValidationFixture(t, 1)
	result.Payload.Edits[0].NewText = "\"Renamed\""
	stampRenameValidation(t, &result)
	if got, err := ValidateRenameWorkspace(ctx, result, result.Payload.ProviderDigest, input, 32<<20); !errors.Is(err, ErrValidationPrecondition) || got.Data.EditPreconditionValidated {
		t.Fatal("preserve-contracts accepted a literal edit proposed under a valid digest")
	}
}
