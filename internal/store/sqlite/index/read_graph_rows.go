package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// readGraphFacts uses a single transaction and bounded keyset pages. The SQL
// preflight rejects corrupt oversized rows before the driver materializes them.
func readGraphFacts(ctx context.Context, tx *sql.Tx, r PublishedGraphRequest, g *PublishedGraph) error {
	id := r.Member.GenerationID
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM packages WHERE generation_id=?)+(SELECT count(*) FROM symbols WHERE generation_id=?)+(SELECT count(*) FROM relations WHERE generation_id=?)+(SELECT count(*) FROM diagnostics WHERE generation_id=?)`, id, id, id, id).Scan(&count); err != nil {
		return err
	}
	if count < 0 || count > int64(r.MaxFacts) {
		return ErrCaptureLimit
	}
	var found int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM packages WHERE generation_id=? AND (package_key='' OR length(CAST(package_key AS BLOB))+length(CAST(import_path AS BLOB))+length(CAST(module_key AS BLOB))+length(CAST(variant AS BLOB))+length(CAST(input_digest AS BLOB))+length(CAST(semantic_status AS BLOB))>262144) UNION ALL SELECT 1 FROM symbols WHERE generation_id=? AND (symbol_id='' OR length(CAST(symbol_id AS BLOB))+coalesce(length(CAST(path AS BLOB)),0)+coalesce(length(CAST(package_key AS BLOB)),0)+length(CAST(kind AS BLOB))+length(CAST(name AS BLOB))+length(CAST(qualified_name AS BLOB))+length(CAST(scope_key AS BLOB))+length(CAST(signature AS BLOB))>262144) UNION ALL SELECT 1 FROM relations WHERE generation_id=? AND (relation_id='' OR length(CAST(relation_id AS BLOB))+length(CAST(source_symbol_id AS BLOB))+coalesce(length(CAST(target_symbol_id AS BLOB)),0)+length(CAST(kind AS BLOB))+length(CAST(evidence_kind AS BLOB))+length(CAST(site_path AS BLOB))+length(CAST(reason AS BLOB))+coalesce(length(CAST(unresolved_label AS BLOB)),0)>262144) UNION ALL SELECT 1 FROM diagnostics WHERE generation_id=? AND (diagnostic_id='' OR length(CAST(diagnostic_id AS BLOB))+coalesce(length(CAST(path AS BLOB)),0)+coalesce(length(CAST(package_key AS BLOB)),0)+length(CAST(code AS BLOB))+length(CAST(severity AS BLOB))+length(CAST(sanitized_message AS BLOB))+length(CAST(scope_json AS BLOB))>262144) LIMIT 1`, id, id, id, id).Scan(&found)
	if err == nil {
		return ErrPublishIntegrity
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var coverageCount, coverageLen, coverageMetadataLen int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(length(CAST(summary_json AS BLOB))),0),coalesce(max(length(CAST(capability_key AS BLOB))+length(CAST(scope_key AS BLOB))+length(CAST(status AS BLOB))),0) FROM capability_results WHERE generation_id=?`, id).Scan(&coverageCount, &coverageLen, &coverageMetadataLen); err != nil {
		return err
	}
	if coverageCount != 1 || coverageLen < 1 || coverageMetadataLen > 2048 {
		return ErrPublishIntegrity
	}
	if coverageLen > int64(r.MaxFactBytes) {
		return ErrCaptureLimit
	}
	var key, scope, status string
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT capability_key,scope_key,status,summary_json FROM capability_results WHERE generation_id=?`, id).Scan(&key, &scope, &status, &raw); err != nil {
		return err
	}
	if int64(len(raw)) != coverageLen || scope != "workspace" {
		return ErrPublishIntegrity
	}
	if err := r.Scanner.CheckContent(raw); err != nil {
		return err
	}
	switch key {
	case "provider_coverage":
		g.Coverage, err = model.DecodeCoverageJSON(raw, r.MaxFactBytes)
		if err != nil || g.Coverage.Status != status || g.Coverage.ScopeDescription != "workspace" || g.state == "READY" && status != "supported_scope_complete" || g.state == "READY_PARTIAL" && status != "partial" {
			return ErrPublishIntegrity
		}
	case "go_semantics":
		var cap model.Capability
		cap, err = model.DecodeCapabilityJSON(raw, r.MaxFactBytes)
		if err != nil || status != "unknown" || g.state != "READY_PARTIAL" || cap.Name != "go_semantics" || cap.ProviderID != "not_run" || cap.ProviderVersion != "not-run" || cap.Status != "unknown" {
			return ErrPublishIntegrity
		}
		g.Coverage = model.Coverage{Status: "unknown", Capabilities: []model.Capability{cap}, Gaps: []model.Gap{{Code: "PROVIDER_NOT_RUN", Reason: "No semantic provider result was published.", SourceRefs: []model.SourceRef{}}}, ScopeDescription: "workspace"}
	default:
		return ErrPublishIntegrity
	}
	coverageRaw, err := json.Marshal(g.Coverage)
	if err != nil || len(coverageRaw) > r.MaxFactBytes {
		return ErrCaptureLimit
	}
	if _, err := model.DecodeCoverageJSON(coverageRaw, r.MaxFactBytes); err != nil {
		return ErrPublishIntegrity
	}
	used, observed := len(coverageRaw), int64(0)
	for _, kind := range []string{"packages", "symbols", "relations", "diagnostics"} {
		cursor := ""
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			query := strings.TrimSuffix(providerPageQuery(kind), "LIMIT 128") + "LIMIT 16"
			rows, err := tx.QueryContext(ctx, query, id, cursor)
			if err != nil {
				return err
			}
			n := 0
			var scanErr error
			for rows.Next() {
				batch := model.ProviderBatch{}
				last, e := scanProviderFact(rows, kind, &batch)
				if e != nil || last <= cursor {
					scanErr = ErrPublishIntegrity
					break
				}
				cursor = last
				var value any
				switch kind {
				case "packages":
					value = batch.Packages[0]
				case "symbols":
					value = batch.Symbols[0]
				case "relations":
					value = batch.Relations[0]
				default:
					value = batch.Diagnostics[0]
				}
				item, e := json.Marshal(value)
				if e != nil || len(item) > r.MaxFactBytes-used {
					scanErr = ErrCaptureLimit
					break
				}
				used += len(item)
				if e = r.Scanner.CheckContent(item); e != nil {
					scanErr = e
					break
				}
				switch kind {
				case "packages":
					var fact model.PackageFact
					fact, e = model.DecodePackageFactJSON(item, r.MaxFactBytes)
					g.Packages = append(g.Packages, fact)
				case "symbols":
					var fact model.SymbolFact
					fact, e = model.DecodeSymbolFactJSON(item, r.MaxFactBytes)
					g.Symbols = append(g.Symbols, fact)
				case "relations":
					var fact model.RelationFact
					fact, e = model.DecodeRelationFactJSON(item, r.MaxFactBytes)
					if e == nil && !validRelationEvidence(fact) {
						e = ErrPublishIntegrity
					}
					g.Relations = append(g.Relations, fact)
				default:
					var fact model.DiagnosticFact
					fact, e = model.DecodeDiagnosticFactJSON(item, r.MaxFactBytes)
					g.Diagnostics = append(g.Diagnostics, fact)
				}
				if e != nil {
					scanErr = ErrPublishIntegrity
					break
				}
				n++
				observed++
			}
			scanErr = errors.Join(scanErr, rows.Err(), rows.Close())
			if scanErr != nil {
				return scanErr
			}
			if n < 16 {
				break
			}
		}
	}
	if observed != count {
		return ErrPublishIntegrity
	}
	if g.Coverage.Status == "unknown" && (len(g.Packages) != 0 || len(g.Symbols) != 0 || len(g.Relations) != 0) || g.Coverage.Status == "partial" && len(g.Coverage.Gaps) == 0 || g.Coverage.Status == "supported_scope_complete" && len(g.Coverage.Gaps) != 0 {
		return ErrPublishIntegrity
	}
	if g.state == "READY" {
		for _, p := range g.Packages {
			if p.SemanticStatus != "complete_in_profile" {
				return ErrPublishIntegrity
			}
		}
		for _, rel := range g.Relations {
			if rel.EvidenceKind == "unresolved" {
				return ErrPublishIntegrity
			}
		}
		for _, diagnostic := range g.Diagnostics {
			if diagnostic.Severity == "error" {
				return ErrPublishIntegrity
			}
		}
	}
	return nil
}

// Validate referential and byte-range integrity against captured bytes, even
// when a damaged database was populated without its usual foreign keys.
func validateGraphSources(g *PublishedGraph) error {
	scan := func(values ...string) error {
		for _, value := range values {
			if err := g.request.Scanner.CheckContent([]byte(value)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := scan(g.Coverage.Status, g.Coverage.ScopeDescription); err != nil {
		return err
	}
	for _, cap := range g.Coverage.Capabilities {
		if err := scan(cap.Name, cap.ProviderID, cap.ProviderVersion, cap.Status); err != nil {
			return err
		}
	}
	packages := make(map[string]bool, len(g.Packages))
	for _, p := range g.Packages {
		if err := scan(p.PackageKey, p.ImportPath, p.ModuleKey, p.Variant, p.SemanticStatus); err != nil {
			return err
		}
		if packages[p.PackageKey] || p.InputDigest != g.request.Member.SnapshotDigest {
			return ErrPublishIntegrity
		}
		packages[p.PackageKey] = true
	}
	checkRange := func(path string, r model.ByteRange) bool {
		file, ok := g.Snapshot.File(path)
		return ok && r.StartByte >= 0 && r.EndByte <= int64(file.Len()) && file.CheckRange(int(r.StartByte), int(r.EndByte)) == nil
	}
	symbols := make(map[string]bool, len(g.Symbols))
	for _, s := range g.Symbols {
		if err := scan(s.SymbolID, s.Kind, s.Name, s.QualifiedName, s.ScopeKey, s.Signature); err != nil {
			return err
		}
		if s.Path != nil {
			if err := scan(*s.Path); err != nil {
				return err
			}
		}
		if s.PackageKey != nil {
			if err := scan(*s.PackageKey); err != nil {
				return err
			}
		}
		if symbols[s.SymbolID] || s.PackageKey != nil && !packages[*s.PackageKey] || s.Path != nil && (s.PackageKey == nil || s.Range == nil || !checkRange(*s.Path, *s.Range)) {
			return ErrPublishIntegrity
		}
		symbols[s.SymbolID] = true
	}
	for _, r := range g.Relations {
		if err := scan(r.RelationID, r.SourceSymbolID, r.Kind, r.EvidenceKind, r.SitePath, r.Reason); err != nil {
			return err
		}
		if r.TargetSymbolID != nil {
			if err := scan(*r.TargetSymbolID); err != nil {
				return err
			}
		}
		if r.UnresolvedLabel != nil {
			if err := scan(*r.UnresolvedLabel); err != nil {
				return err
			}
		}
		if !symbols[r.SourceSymbolID] || r.TargetSymbolID != nil && !symbols[*r.TargetSymbolID] || !checkRange(r.SitePath, r.Range) {
			return ErrPublishIntegrity
		}
	}
	known := make(map[string]bool)
	for _, item := range g.Snapshot.Manifest().Files {
		known[item.Path] = true
	}
	for _, d := range g.Diagnostics {
		if err := scan(d.DiagnosticID, d.Code, d.Severity, d.Message, d.ScopeDescription); err != nil {
			return err
		}
		if d.Path != nil {
			if err := scan(*d.Path); err != nil {
				return err
			}
		}
		if d.PackageKey != nil {
			if err := scan(*d.PackageKey); err != nil {
				return err
			}
		}
		if d.PackageKey != nil && !packages[*d.PackageKey] {
			return ErrPublishIntegrity
		}
		if d.Path != nil {
			if !known[*d.Path] {
				return ErrPublishIntegrity
			}
		}
	}
	for _, gap := range g.Coverage.Gaps {
		if err := scan(gap.Code, gap.Reason); err != nil {
			return err
		}
		for _, ref := range gap.SourceRefs {
			file, ok := g.Snapshot.File(ref.Path)
			if !ok || ref.WorktreeID != g.request.Member.WorktreeID || ref.ProfileID != g.request.Member.ProfileID || ref.GenerationID != g.request.Member.GenerationID || ref.ContentHash != file.ContentHash() || !checkRange(ref.Path, ref.Range) {
				return ErrPublishIntegrity
			}
		}
	}
	return nil
}
