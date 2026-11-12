package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// validateStoredProviderFacts revalidates staged provider rows in bounded
// pages. It does not establish that the provider actually analyzed its input.
func validateStoredProviderFacts(ctx context.Context, db publicationQueryer, worktree string, capture CaptureWrite, manifest model.CaptureManifest, maxFacts int) error {
	if ctx == nil || db == nil || capture.Snapshot == nil || capture.Scanner == nil || capture.Authorize == nil || maxFacts < 1 || maxFacts > 1000000 {
		return ErrInvalidStore
	}
	if _, ok := ctx.Deadline(); !ok {
		return ErrInvalidStore
	}
	var tooLarge int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM packages WHERE generation_id=? AND (package_key='' OR length(CAST(package_key AS BLOB))+length(CAST(import_path AS BLOB))+length(CAST(module_key AS BLOB))+length(CAST(variant AS BLOB))+length(CAST(input_digest AS BLOB))+length(CAST(semantic_status AS BLOB))>262144) UNION ALL SELECT 1 FROM symbols WHERE generation_id=? AND (symbol_id='' OR length(CAST(symbol_id AS BLOB))+coalesce(length(CAST(path AS BLOB)),0)+coalesce(length(CAST(package_key AS BLOB)),0)+length(CAST(kind AS BLOB))+length(CAST(name AS BLOB))+length(CAST(qualified_name AS BLOB))+length(CAST(scope_key AS BLOB))+length(CAST(signature AS BLOB))>262144) UNION ALL SELECT 1 FROM relations WHERE generation_id=? AND (relation_id='' OR length(CAST(relation_id AS BLOB))+length(CAST(source_symbol_id AS BLOB))+coalesce(length(CAST(target_symbol_id AS BLOB)),0)+length(CAST(kind AS BLOB))+length(CAST(evidence_kind AS BLOB))+length(CAST(site_path AS BLOB))+length(CAST(reason AS BLOB))+coalesce(length(CAST(unresolved_label AS BLOB)),0)>262144) UNION ALL SELECT 1 FROM diagnostics WHERE generation_id=? AND (diagnostic_id='' OR length(CAST(diagnostic_id AS BLOB))+coalesce(length(CAST(path AS BLOB)),0)+coalesce(length(CAST(package_key AS BLOB)),0)+length(CAST(code AS BLOB))+length(CAST(severity AS BLOB))+length(CAST(sanitized_message AS BLOB))+length(CAST(scope_json AS BLOB))>262144) LIMIT 1`, capture.GenerationID, capture.GenerationID, capture.GenerationID, capture.GenerationID).Scan(&tooLarge)
	if err == nil {
		return ErrPublishIntegrity
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	known := make(map[string]bool, len(manifest.Files))
	for _, item := range manifest.Files {
		known[item.Path] = true
	}
	remaining := maxFacts
	validatePage := func(batch model.ProviderBatch) error {
		if !providerBatchWithinBudget(batch, 16*1024*1024) {
			return ErrCaptureLimit
		}
		raw, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		decoded, err := model.DecodeProviderBatchJSON(raw, 16*1024*1024)
		if err != nil {
			return ErrPublishIntegrity
		}
		paths, err := validateProviderBatchWithKnown(ctx, worktree, capture, manifest, known, decoded)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err := capture.Authorize(ctx, worktree, path, manifest.PolicyDigest, capture.PolicyEpoch); err != nil {
				return err
			}
		}
		return nil
	}
	for _, kind := range []string{"packages", "symbols", "relations", "diagnostics"} {
		cursor := ""
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			query := providerPageQuery(kind)
			rows, err := db.QueryContext(ctx, query, capture.GenerationID, cursor)
			if err != nil {
				return err
			}
			batch := model.ProviderBatch{ProviderProtocol: 1, Type: "analyze_batch", JobID: capture.JobID, Sequence: 1, Packages: []model.PackageFact{}, Symbols: []model.SymbolFact{}, Relations: []model.RelationFact{}, Diagnostics: []model.DiagnosticFact{}}
			count := 0
			last := ""
			var scanErr error
			for rows.Next() {
				count++
				remaining--
				if remaining < 0 {
					scanErr = ErrCaptureLimit
					break
				}
				last, scanErr = scanProviderFact(rows, kind, &batch)
				if scanErr != nil {
					break
				}
			}
			if err := rows.Err(); err != nil {
				scanErr = errors.Join(scanErr, err)
			}
			if err := rows.Close(); err != nil {
				scanErr = errors.Join(scanErr, err)
			}
			if scanErr != nil {
				return scanErr
			}
			if count == 0 {
				break
			}
			if err := validatePage(batch); err != nil {
				return err
			}
			cursor = last
			if count < 128 {
				break
			}
		}
	}
	return ctx.Err()
}

func providerPageQuery(kind string) string {
	switch kind {
	case "packages":
		return `SELECT package_key,import_path,module_key,variant,input_digest,semantic_status FROM packages WHERE generation_id=? AND package_key>? ORDER BY package_key LIMIT 128`
	case "symbols":
		return `SELECT symbol_id,path,package_key,kind,name,qualified_name,scope_key,start_byte,end_byte,signature,exported FROM symbols WHERE generation_id=? AND symbol_id>? ORDER BY symbol_id LIMIT 128`
	case "relations":
		return `SELECT relation_id,source_symbol_id,target_symbol_id,kind,evidence_kind,site_path,start_byte,end_byte,reason,unresolved_label FROM relations WHERE generation_id=? AND relation_id>? ORDER BY relation_id LIMIT 128`
	default:
		return `SELECT diagnostic_id,path,package_key,code,severity,sanitized_message,scope_json FROM diagnostics WHERE generation_id=? AND diagnostic_id>? ORDER BY diagnostic_id LIMIT 128`
	}
}

func scanProviderFact(rows *sql.Rows, kind string, batch *model.ProviderBatch) (string, error) {
	switch kind {
	case "packages":
		var p model.PackageFact
		if err := rows.Scan(&p.PackageKey, &p.ImportPath, &p.ModuleKey, &p.Variant, &p.InputDigest, &p.SemanticStatus); err != nil {
			return "", err
		}
		batch.Packages = append(batch.Packages, p)
		return p.PackageKey, nil
	case "symbols":
		var s model.SymbolFact
		var path, pkg sql.NullString
		var start, end sql.NullInt64
		var exported int
		if err := rows.Scan(&s.SymbolID, &path, &pkg, &s.Kind, &s.Name, &s.QualifiedName, &s.ScopeKey, &start, &end, &s.Signature, &exported); err != nil {
			return "", err
		}
		if exported != 0 && exported != 1 || start.Valid != end.Valid {
			return "", ErrPublishIntegrity
		}
		if path.Valid {
			s.Path = &path.String
		}
		if pkg.Valid {
			s.PackageKey = &pkg.String
		}
		if start.Valid {
			s.Range = &model.ByteRange{StartByte: start.Int64, EndByte: end.Int64}
		}
		s.Exported = exported == 1
		batch.Symbols = append(batch.Symbols, s)
		return s.SymbolID, nil
	case "relations":
		var r model.RelationFact
		var target, label sql.NullString
		if err := rows.Scan(&r.RelationID, &r.SourceSymbolID, &target, &r.Kind, &r.EvidenceKind, &r.SitePath, &r.Range.StartByte, &r.Range.EndByte, &r.Reason, &label); err != nil {
			return "", err
		}
		if target.Valid {
			r.TargetSymbolID = &target.String
		}
		if label.Valid {
			r.UnresolvedLabel = &label.String
		}
		batch.Relations = append(batch.Relations, r)
		return r.RelationID, nil
	default:
		var d model.DiagnosticFact
		var path, pkg sql.NullString
		var scope string
		if err := rows.Scan(&d.DiagnosticID, &path, &pkg, &d.Code, &d.Severity, &d.Message, &scope); err != nil {
			return "", err
		}
		if path.Valid {
			d.Path = &path.String
		}
		if pkg.Valid {
			d.PackageKey = &pkg.String
		}
		if _, err := model.DecodeIdentityJSON([]byte(scope), 262144); err != nil {
			return "", ErrPublishIntegrity
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(scope), &fields); err != nil || len(fields) != 1 {
			return "", ErrPublishIntegrity
		}
		if raw, ok := fields["scope_description"]; !ok || json.Unmarshal(raw, &d.ScopeDescription) != nil {
			return "", ErrPublishIntegrity
		}
		batch.Diagnostics = append(batch.Diagnostics, d)
		return d.DiagnosticID, nil
	}
}
