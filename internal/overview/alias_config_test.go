package overview

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

func configFixture(t *testing.T, config string, filename string) (context.Context, AliasConfigInput) {
	t.Helper()
	ctx, plan := planFixtureWithFiles(t, false, "", map[string]string{filename: config})
	return ctx, AliasConfigInput{Overview: plan.Overview, Snapshot: plan.Snapshot, Policy: plan.Policy, ConfigPath: filename, Scanner: plan.Scanner, AuthorizeConfig: func(context.Context, Scope, string) error { return nil }, AuthorizeAlias: func(context.Context, Scope, AliasDeclaration) error { return nil }, MaxBytes: 65536}
}

func testAliasConfig(t *testing.T) string {
	t.Helper()
	type entry struct {
		Alias    string          `json:"alias"`
		EntityID string          `json:"entity_id"`
		Path     string          `json:"path"`
		Range    model.ByteRange `json:"range"`
	}
	values := []entry{}
	for _, pair := range [][2]string{{"PaymentService", "billing"}, {"PaymentService", "orders"}, {"số dư khả dụng", "balance"}, {"ຍອດເງິນ", "balance"}} {
		start := strings.Index(fixturePlanDocument, pair[0])
		values = append(values, entry{Alias: pair[0], EntityID: pair[1], Path: "spec.md", Range: model.ByteRange{StartByte: int64(start), EndByte: int64(start + len(pair[0]))}})
	}
	raw, err := json.Marshal(struct {
		SchemaVersion int     `json:"schema_version"`
		Entries       []entry `json:"entries"`
	}{1, values})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestApprovedAliasConfigAndRevocation(t *testing.T) {
	ctx, in := configFixture(t, testAliasConfig(t), "vocabulary.json")
	approved := true
	in.AuthorizeConfig = func(context.Context, Scope, string) error {
		if !approved {
			return errors.New("revoked")
		}
		return nil
	}
	index, err := BuildAliasIndexFromConfig(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	result, err := index.Lookup(ctx, "PaymentService")
	if err != nil || !result.Ambiguous || len(result.Candidates) != 2 {
		t.Fatal("approved config lookup lost ambiguity")
	}
	result, err = index.Lookup(ctx, "ຍອດເງິນ")
	if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Entity.EntityID != "balance" {
		t.Fatal("multilingual config lookup")
	}
	approved = false
	if result, err = index.Lookup(ctx, "PaymentService"); !errors.Is(err, ErrAuthority) || len(result.Candidates) != 0 {
		t.Fatal("revoked config leaked")
	}
}

func TestAliasConfigRejectsUntrustedMalformedAndUnanchored(t *testing.T) {
	valid := testAliasConfig(t)
	ctx, in := configFixture(t, valid, "untrusted.json")
	if _, err := BuildAliasIndexFromConfig(ctx, in); !errors.Is(err, ErrAuthority) {
		t.Fatal("untrusted config accepted")
	}
	for _, bad := range []string{
		strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1),
		strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"extra":true`, 1),
		strings.Replace(valid, `"alias":"PaymentService"`, `"alias":"Invented"`, 1),
		strings.Replace(valid, `"entity_id":"billing"`, `"entity_id":null`, 1),
	} {
		ctx, in = configFixture(t, bad, "vocabulary.json")
		if _, err := BuildAliasIndexFromConfig(ctx, in); err == nil {
			t.Fatal("malformed or unanchored config accepted")
		}
	}
}
