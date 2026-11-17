package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func freshnessRestoreFixture(t *testing.T, f *freshnessFixture, token string, archive []byte) FreshnessPackRestoreInput {
	t.Helper()
	snap, err := f.input.Validation.ResolveCapture(f.ctx, f.scope.Members[0])
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := policy.NewScanner(65536)
	if err != nil {
		t.Fatal(err)
	}
	restored := contextpack.PackRestoreInput{Codec: f.input.Codec, Token: token, PrincipalDigest: f.input.Binding.PrincipalDigest,
		Snapshot: snap, Scanner: scanner, Guard: f.input.Guard, MaxArchiveBytes: 4 * 1024 * 1024,
		AuthorizeScope: func(_ context.Context, scope model.ScopeVector, _ model.ScopeMember) error {
			return model.ComparePinnedScope(scope, f.scope, 65536)
		},
		AuthorizePath: func(context.Context, model.ScopeMember, string) error {
			if f.denied {
				return ErrValidationAuthority
			}
			return nil
		},
		VerifyFacts:             func(context.Context, model.ScopeMember, string) error { f.factChecks++; return nil },
		VerifyNoApprovedRecords: func(context.Context, model.ScopeVector) (bool, error) { return true, nil }}
	return FreshnessPackRestoreInput{Archive: archive, Pack: restored, RequestJSON: f.input.RequestJSON,
		PrincipalID: f.input.PrincipalID, Validation: f.input.Validation, CaptureLive: f.input.CaptureLive, RequestLimit: f.input.RequestLimit}
}

func TestFreshnessPackRestoreKeepsModeAndChecksWorkspace(t *testing.T) {
	for _, mode := range []string{"analysis_snapshot", "checked_workspace"} {
		t.Run(mode, func(t *testing.T) {
			f := newFreshnessFixture(t, mode)
			pack, token, err := NewFreshnessPack(f.ctx, f.input)
			if err != nil {
				t.Fatal(err)
			}
			first, err := pack.First(f.ctx, freshnessOptions())
			if err != nil || first.Response.Pagination.NextCursor == nil {
				t.Fatalf("missing first-page cursor: %v", err)
			}
			cursor := *first.Response.Pagination.NextCursor
			expected, err := pack.Continue(f.ctx, token, cursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions())
			if err != nil {
				t.Fatal(err)
			}
			archive, err := pack.Archive(f.ctx, 4*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			in := freshnessRestoreFixture(t, f, token, archive)
			wrong := in
			wrong.PrincipalID = "another actor"
			if _, err := RestoreFreshnessPack(f.ctx, wrong); !errors.Is(err, ErrValidationInput) {
				t.Fatalf("different request principal accepted: %v", err)
			}
			wrong = in
			otherMode := "analysis_snapshot"
			if mode == otherMode {
				otherMode = "checked_workspace"
			}
			wrong.RequestJSON = bytes.Replace(in.RequestJSON, []byte(mode), []byte(otherMode), 1)
			if bytes.Equal(wrong.RequestJSON, in.RequestJSON) {
				t.Fatal("fixture mode missing from request")
			}
			if _, err := RestoreFreshnessPack(f.ctx, wrong); err == nil {
				t.Fatal("modified mode accepted")
			}
			if mode == "checked_workspace" {
				wrong = in
				wrong.CaptureLive = nil
				if _, err := RestoreFreshnessPack(f.ctx, wrong); !errors.Is(err, ErrFreshnessUnknown) {
					t.Fatalf("checked mode without live reader accepted: %v", err)
				}
			}
			restored, err := RestoreFreshnessPack(f.ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := restored.Continue(f.ctx, token, cursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions())
			if err != nil {
				t.Fatal(err)
			}
			if actual.Response.Freshness.Status != expected.Response.Freshness.Status || !reflect.DeepEqual(actual.Response.Data.(model.PackData).Items, expected.Response.Data.(model.PackData).Items) {
				t.Fatal("restoration changed materialization or freshness mode")
			}
			if mode == "analysis_snapshot" && (f.fullChecks != 0 || f.liveChecks != 0) {
				t.Fatal("analysis continuation unexpectedly used live workspace")
			}
			f.changed = true
			if _, err := restored.Validate(f.ctx, token, f.input.Binding.PrincipalDigest, f.input.Guard); !errors.Is(err, ErrFreshnessChanged) {
				t.Fatalf("restored validation reused an old workspace verdict: %v", err)
			}
			f.revoked = true
			if _, err := restored.Continue(f.ctx, token, cursor, f.input.Binding.PrincipalDigest, f.input.Guard, freshnessOptions()); !errors.Is(err, ErrValidationAuthority) {
				t.Fatalf("revoked restored pack accepted: %v", err)
			}
		})
	}
}

func TestFreshnessPackEnvelopeBindsRequestAndArchive(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	p, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ArchiveEnvelope(f.ctx, "wrong actor", f.input.RequestJSON, 60, 4*1024*1024); !errors.Is(err, ErrValidationInput) {
		t.Fatalf("wrong actor could create envelope: %v", err)
	}
	envelope, err := p.ArchiveEnvelope(f.ctx, f.input.PrincipalID, f.input.RequestJSON, 60, 4*1024*1024)
	if err != nil || envelope.Token != token || envelope.PrincipalDigest != f.input.Binding.PrincipalDigest {
		t.Fatalf("failed to create envelope: %v", err)
	}
	in := freshnessRestoreFixture(t, f, token, nil)
	restored, err := RestoreFreshnessEnvelope(f.ctx, envelope.Bytes, in, 4*1024*1024)
	if err != nil || restored.RecheckRetained(f.ctx, token, envelope.PrincipalDigest, f.input.Guard) != nil {
		t.Fatalf("valid envelope did not restore: %v", err)
	}
	var stored packEnvelope
	if err := json.Unmarshal(envelope.Bytes, &stored); err != nil {
		t.Fatal(err)
	}
	stored.Request = bytes.Replace(stored.Request, []byte("analysis_snapshot"), []byte("checked_workspace"), 1)
	changed, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreFreshnessEnvelope(f.ctx, changed, in, 4*1024*1024); !errors.Is(err, ErrValidationInput) {
		t.Fatalf("different request accepted: %v", err)
	}
	if err := json.Unmarshal(envelope.Bytes, &stored); err != nil {
		t.Fatal(err)
	}
	stored.Archive[len(stored.Archive)-1] ^= 1
	changed, _ = json.Marshal(stored)
	if _, err := RestoreFreshnessEnvelope(f.ctx, changed, in, 4*1024*1024); !errors.Is(err, contextpack.ErrCursorInvalid) {
		t.Fatalf("modified archive accepted: %v", err)
	}
	if _, err := RestoreFreshnessEnvelope(f.ctx, append(envelope.Bytes, ' '), in, 4*1024*1024); !errors.Is(err, ErrValidationInput) {
		t.Fatalf("noncanonical envelope accepted: %v", err)
	}
}

func TestFreshnessPackRestoreExpiresRetainedHandle(t *testing.T) {
	f := newFreshnessFixture(t, "checked_workspace")
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	first, err := pack.First(f.ctx, freshnessOptions())
	if err != nil || first.Response.Pagination.NextCursor == nil {
		t.Fatalf("missing cursor: %v", err)
	}
	archive, err := pack.ArchiveForRetention(f.ctx, 4*1024*1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	in := freshnessRestoreFixture(t, f, token, archive)
	restored, err := RestoreFreshnessPack(f.ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.input.Codec.VerifyPrincipal(f.ctx, token, "pack", f.input.Binding.PrincipalDigest, f.input.Guard)
	if err != nil {
		t.Fatal(err)
	}
	if wait := time.Until(time.Unix(claims.IssuedAt+2, 0)) + 20*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if _, err := restored.Validate(f.ctx, token, in.Pack.PrincipalDigest, f.input.Guard); !errors.Is(err, contextpack.ErrPackExpired) {
		t.Fatalf("validation outlived retention TTL: %v", err)
	}
	if _, err := restored.Continue(f.ctx, token, *first.Response.Pagination.NextCursor, in.Pack.PrincipalDigest, f.input.Guard, freshnessOptions()); !errors.Is(err, contextpack.ErrPackExpired) {
		t.Fatalf("continuation outlived retention TTL: %v", err)
	}
}

func TestFreshnessPackRestoreChecksPositionAgainstPinnedGeneration(t *testing.T) {
	f := newFreshnessFixture(t, "analysis_snapshot")
	report, err := f.input.Slice.Read(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	request, err := model.DecodePackRequestJSON(f.input.RequestJSON, f.input.RequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	request.Selector = &model.PositionSelector{Kind: "source_position", Path: "main.go", ContentHash: f.live.ContentHash(), ByteOffset: 15}
	f.input.RequestJSON, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	f.input.Binding.RequestDigest, err = model.RequestDigest("context_pack", f.input.PrincipalID, "PackRequest", request, f.input.RequestLimit, 4)
	if err != nil {
		t.Fatal(err)
	}
	f.input.ResolvePosition = func(_ context.Context, _ model.PositionSelector, scope model.ScopeVector, _ *string) (model.SymbolRef, error) {
		if model.ComparePinnedScope(scope, f.scope, 65536) != nil {
			return model.SymbolRef{}, ErrValidationAuthority
		}
		return report.Target, nil
	}
	pack, token, err := NewFreshnessPack(f.ctx, f.input)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := pack.Archive(f.ctx, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	in := freshnessRestoreFixture(t, f, token, archive)
	if _, err := RestoreFreshnessPack(f.ctx, in); !errors.Is(err, ErrValidationAuthority) {
		t.Fatalf("missing position proof accepted: %v", err)
	}
	in.ResolvePosition = func(context.Context, model.PositionSelector, model.ScopeVector, *string) (model.SymbolRef, error) {
		return model.SymbolRef{}, nil
	}
	if _, err := RestoreFreshnessPack(f.ctx, in); !errors.Is(err, ErrValidationPrecondition) {
		t.Fatalf("different position target accepted: %v", err)
	}
	in.ResolvePosition = f.input.ResolvePosition
	if _, err := RestoreFreshnessPack(f.ctx, in); err != nil {
		t.Fatalf("pinned position proof rejected: %v", err)
	}
	in.ResolvePosition = func(_ context.Context, _ model.PositionSelector, scope model.ScopeVector, _ *string) (model.SymbolRef, error) {
		scope.Members[0].PolicyEpoch++
		return report.Target, nil
	}
	restored, err := RestoreFreshnessPack(f.ctx, in)
	if err != nil {
		t.Fatalf("position callback mutated retained scope: %v", err)
	}
	if model.ComparePinnedScope(restored.validation.Scope, f.scope, 65536) != nil {
		t.Fatal("position callback changed validation scope")
	}
}
