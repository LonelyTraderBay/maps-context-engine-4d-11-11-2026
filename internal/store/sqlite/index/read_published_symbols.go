package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type PublishedFileSymbolsRequest struct {
	File          PublishedFileRequest
	MaxSymbols    int
	MaxFactsBytes int
	MaxRelations  int
}

type PublishedFileSymbols struct {
	File      PublishedFile
	Symbols   []model.SymbolFact
	Relations []model.RelationFact
	Truncated bool
}

// ReadPublishedFileSymbols reads source and local symbols in one snapshot.
// It is an internal bounded primitive, not a live freshness or graph query.
func (s *Store) ReadPublishedFileSymbols(ctx context.Context, request PublishedFileSymbolsRequest) (PublishedFileSymbols, error) {
	if request.MaxSymbols < 1 || request.MaxSymbols > 128 || request.MaxFactsBytes < 1 || request.MaxFactsBytes > 16*1024*1024 ||
		request.MaxRelations < 0 || request.MaxRelations > 1500 {
		return PublishedFileSymbols{}, ErrInvalidStore
	}
	var symbols []model.SymbolFact
	var relations []model.RelationFact
	var truncated bool
	file, err := s.readPublishedFile(ctx, request.File, func(ctx context.Context, tx *sql.Tx, file PublishedFile) error {
		if !utf8.Valid(file.Bytes) {
			return ErrCaptureMismatch
		}
		var oversized int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM symbols WHERE generation_id=? AND path=? AND (length(CAST(symbol_id AS BLOB))+coalesce(length(CAST(package_key AS BLOB)),0)+length(CAST(kind AS BLOB))+length(CAST(name AS BLOB))+length(CAST(qualified_name AS BLOB))+length(CAST(scope_key AS BLOB))+length(CAST(signature AS BLOB))>?) LIMIT 1`, file.GenerationID, file.Path, request.MaxFactsBytes).Scan(&oversized)
		if err == nil {
			return ErrCaptureLimit
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		order := "symbol_id"
		if request.MaxRelations != 0 {
			order = "CASE WHEN kind='file' THEN 0 WHEN exported=1 THEN 1 ELSE 2 END,start_byte,symbol_id"
		}
		rows, err := tx.QueryContext(ctx, providerSymbolFactSelect+" WHERE generation_id=? AND path=? ORDER BY "+order+" LIMIT ?",
			file.GenerationID, file.Path, request.MaxSymbols+1)
		if err != nil {
			return err
		}
		batch := model.ProviderBatch{Symbols: []model.SymbolFact{}}
		var resultErr error
		total := 0
		for rows.Next() {
			if len(batch.Symbols) == request.MaxSymbols {
				if request.MaxRelations == 0 {
					resultErr = ErrCaptureLimit
				} else {
					truncated = true
				}
				break
			}
			_, err := scanProviderFact(rows, "symbols", &batch)
			if err != nil {
				resultErr = err
				break
			}
			symbol := batch.Symbols[len(batch.Symbols)-1]
			if symbol.Path == nil || *symbol.Path != file.Path || symbol.Range == nil || symbol.Range.StartByte < 0 || symbol.Range.EndByte < symbol.Range.StartByte || symbol.Range.EndByte > int64(len(file.Bytes)) || !sourceBoundary(file.Bytes, int(symbol.Range.StartByte)) || !sourceBoundary(file.Bytes, int(symbol.Range.EndByte)) {
				resultErr = ErrCaptureMismatch
				break
			}
			values := []string{symbol.SymbolID, symbol.Kind, symbol.Name, symbol.QualifiedName, symbol.ScopeKey, symbol.Signature}
			if symbol.PackageKey != nil {
				values = append(values, *symbol.PackageKey)
			}
			for _, value := range values {
				if err := request.File.Scanner.CheckContent([]byte(value)); err != nil {
					resultErr = err
					break
				}
			}
			if resultErr != nil {
				break
			}
			// Bound JSON expansion before materializing the wire representation.
			one := model.ProviderBatch{Symbols: []model.SymbolFact{symbol}}
			if !ProviderBatchWithinBudget(one, request.MaxFactsBytes-total) {
				resultErr = ErrCaptureLimit
				break
			}
			raw, err := json.Marshal(symbol)
			if err != nil {
				resultErr = err
				break
			}
			if _, err := model.DecodeSymbolFactJSON(raw, request.MaxFactsBytes-total); err != nil {
				resultErr = ErrPublishIntegrity
				break
			}
			total += len(raw)
		}
		resultErr = errors.Join(resultErr, rows.Err(), rows.Close())
		if resultErr != nil {
			return resultErr
		}
		symbols = batch.Symbols
		if request.MaxRelations != 0 && len(symbols) != 0 {
			var limited bool
			relations, limited, err = readLocalRelations(ctx, tx, request, file, symbols, total)
			truncated = truncated || limited
			return err
		}
		return nil
	})
	if err != nil {
		return PublishedFileSymbols{}, err
	}
	return PublishedFileSymbols{File: file, Symbols: symbols, Relations: relations, Truncated: truncated}, nil
}

func readLocalRelations(ctx context.Context, tx *sql.Tx, request PublishedFileSymbolsRequest, file PublishedFile,
	symbols []model.SymbolFact, total int) (_ []model.RelationFact, _ bool, resultErr error) {
	ids := strings.TrimSuffix(strings.Repeat("?,", len(symbols)), ",")
	args := make([]any, 0, 3+2*len(symbols))
	args = append(args, file.GenerationID, file.Path)
	for _, symbol := range symbols {
		args = append(args, symbol.SymbolID)
	}
	for _, symbol := range symbols {
		args = append(args, symbol.SymbolID)
	}
	args = append(args, request.MaxRelations+1)
	rows, err := tx.QueryContext(ctx, `SELECT relation_id,source_symbol_id,target_symbol_id,kind,evidence_kind,
		site_path,start_byte,end_byte,reason,unresolved_label FROM relations WHERE generation_id=? AND site_path=?
		AND source_symbol_id IN (`+ids+`) AND target_symbol_id IN (`+ids+`) ORDER BY relation_id LIMIT ?`, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	batch := model.ProviderBatch{}
	for rows.Next() {
		if len(batch.Relations) == request.MaxRelations {
			return batch.Relations, true, rows.Err()
		}
		if _, err := scanProviderFact(rows, "relations", &batch); err != nil {
			return nil, false, err
		}
		relation := batch.Relations[len(batch.Relations)-1]
		start, end := relation.Range.StartByte, relation.Range.EndByte
		if relation.SitePath != file.Path || start < 0 || end < start || end > int64(len(file.Bytes)) ||
			!sourceBoundary(file.Bytes, int(start)) || !sourceBoundary(file.Bytes, int(end)) {
			return nil, false, ErrCaptureMismatch
		}
		one := model.ProviderBatch{Relations: []model.RelationFact{relation}}
		if !ProviderBatchWithinBudget(one, request.MaxFactsBytes-total) {
			return batch.Relations[:len(batch.Relations)-1], true, rows.Err()
		}
		raw, err := json.Marshal(relation)
		if err != nil || request.File.Scanner.CheckContent(raw) != nil {
			return nil, false, ErrPublishIntegrity
		}
		if _, err := model.DecodeRelationFactJSON(raw, request.MaxFactsBytes-total); err != nil {
			return nil, false, ErrPublishIntegrity
		}
		total += len(raw)
	}
	return batch.Relations, false, rows.Err()
}

func sourceBoundary(source []byte, offset int) bool {
	return offset == len(source) || utf8.RuneStart(source[offset])
}
