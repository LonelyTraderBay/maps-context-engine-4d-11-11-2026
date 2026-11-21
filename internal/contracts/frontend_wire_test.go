package contracts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
)

type frontendFixtureCapture struct{ hash string }

func (c frontendFixtureCapture) Manifest() model.CaptureManifest {
	return model.CaptureManifest{WorktreeID: worktree, SnapshotDigest: c.hash}
}

func (c frontendFixtureCapture) CheckCapturedRange(path string, span model.ByteRange) (string, error) {
	if path != "index.tsx" || span.StartByte < 0 || span.EndByte > 20 {
		return "", ErrDomainRecord
	}
	return c.hash, nil
}

func TestFrontendWireScopeAndSchema(t *testing.T) {
	schema := sealedSchema(t, "frontend-analysis.v1.json", "MCE4D_FRONTEND_ANALYSIS_V1")
	hash := strings.Repeat("e", 64)
	job := ports.ProviderJob{Start: model.ProviderStart{
		JobID: worktree, GenerationID: generation, ProfileID: hash, MaxFrameBytes: 65536,
		Profile: &model.TSProfile{TsconfigPaths: []string{"tsconfig.json"}},
	}, Capture: frontendFixtureCapture{hash}}
	raw := fmt.Sprintf(`{"frontend_protocol":1,"job_id":"%s","input_digest":"%s","routes":[],"clients":[{"config":"tsconfig.json","path":"/items","method":"GET","site":{"path":"index.tsx","start_byte":1,"end_byte":10}}],"gaps":[]}`, worktree, hash)
	shape := func(data string) error {
		var wire any
		if err := json.Unmarshal([]byte(data), &wire); err != nil {
			t.Fatal(err)
		}
		return schema.Validate(wire)
	}
	if err := shape(raw); err != nil {
		t.Fatal(err)
	}
	parsed, err := DecodeFrontendAnalysis([]byte(raw), job)
	if err != nil || len(parsed.Clients) != 1 || parsed.Clients[0].Site.GenerationID != generation {
		t.Fatalf("valid case did not bind capture: %v", err)
	}
	for _, invalid := range []string{
		strings.Replace(raw, `"GET"`, `"TRACE"`, 1),
		strings.Replace(raw, `"gaps":[]`, `"gaps":[],"extra":true`, 1),
		strings.Replace(raw, `"path":"/items"`, `"path":"/items?query=yes"`, 1),
	} {
		if _, err := DecodeFrontendAnalysis([]byte(invalid), job); err == nil || shape(invalid) == nil {
			t.Fatal("frontend response shape accepted invalid item")
		}
	}
	for _, invalid := range []string{
		strings.Replace(raw, hash, strings.Repeat("d", 64), 1),
		strings.Replace(raw, `"end_byte":10`, `"end_byte":40`, 1),
		strings.Replace(raw, `"index.tsx"`, `"missing.tsx"`, 1),
		strings.Replace(raw, `"tsconfig.json"`, `"other.json"`, 1),
	} {
		if _, err := DecodeFrontendAnalysis([]byte(invalid), job); err == nil {
			t.Fatal("frontend response escaped current capture")
		}
	}
}
