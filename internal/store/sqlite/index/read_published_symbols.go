package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type PublishedFileSymbolsRequest struct {
	File          PublishedFileRequest
	MaxSymbols    int
	MaxFactsBytes int
}

type PublishedFileSymbols struct {
	File    PublishedFile
	Symbols []model.SymbolFact
}

// ReadPublishedFileSymbols reads source and local symbols in one snapshot.
// It is an internal bounded primitive, not a live freshness or graph query.
func (s *Store) ReadPublishedFileSymbols(ctx context.Context, request PublishedFileSymbolsRequest) (PublishedFileSymbols, error) {
	if request.MaxSymbols < 1 || request.MaxSymbols > 128 || request.MaxFactsBytes < 1 || request.MaxFactsBytes > 16*1024*1024 {
		return PublishedFileSymbols{}, ErrInvalidStore
	}
	var symbols []model.SymbolFact
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
		rows, err := tx.QueryContext(ctx, `SELECT symbol_id,path,package_key,kind,name,qualified_name,scope_key,start_byte,end_byte,signature,exported FROM symbols WHERE generation_id=? AND path=? ORDER BY symbol_id LIMIT ?`, file.GenerationID, file.Path, request.MaxSymbols+1)
		if err != nil {
			return err
		}
		batch := model.ProviderBatch{Symbols: []model.SymbolFact{}}
		var resultErr error
		total := 0
		for rows.Next() {
			if len(batch.Symbols) == request.MaxSymbols {
				resultErr = ErrCaptureLimit
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
			if !providerBatchWithinBudget(one, request.MaxFactsBytes-total) {
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
		return nil
	})
	if err != nil {
		return PublishedFileSymbols{}, err
	}
	return PublishedFileSymbols{File: file, Symbols: symbols}, nil
}

func sourceBoundary(source []byte, offset int) bool {
	return offset == len(source) || utf8.RuneStart(source[offset])
}
