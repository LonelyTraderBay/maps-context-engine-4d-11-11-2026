package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
)

var ErrInvalidSearch = errors.New("invalid bounded search")

// Values are read through bounded CASE expressions so corrupt oversized
// metadata is rejected before it is materialized in the Go process.
const searchSymbolFields = `CASE WHEN length(CAST(s.symbol_id AS BLOB))<=1024 THEN s.symbol_id END,
CASE WHEN length(CAST(s.path AS BLOB))<=16384 THEN s.path END,length(CAST(s.path AS BLOB)),
CASE WHEN length(CAST(s.package_key AS BLOB))<=1024 THEN s.package_key END,length(CAST(s.package_key AS BLOB)),
CASE WHEN length(CAST(s.kind AS BLOB))<=64 THEN s.kind END,
CASE WHEN length(CAST(s.name AS BLOB))<=2048 THEN s.name END,
CASE WHEN length(CAST(s.qualified_name AS BLOB))<=8192 THEN s.qualified_name END,
CASE WHEN length(CAST(s.scope_key AS BLOB))<=1024 THEN s.scope_key END,
s.start_byte,s.end_byte,CASE WHEN length(CAST(s.signature AS BLOB))<=131072 THEN s.signature END,s.exported,
f.content_hash,f.byte_length,f.status`

const textSearchSQL = `SELECT CASE WHEN length(CAST(d.item_id AS BLOB))<=128 THEN d.item_id END,
CASE WHEN length(CAST(d.path AS BLOB))<=16384 THEN d.path END,
CASE WHEN length(CAST(d.kind AS BLOB))<=32 THEN d.kind END,
CASE WHEN length(CAST(d.name AS BLOB))<=16384 THEN d.name END,
CASE WHEN length(CAST(d.qualified_name AS BLOB))<=16384 THEN d.qualified_name END,
f.content_hash,f.byte_length,f.status
FROM search_fts CROSS JOIN search_documents d ON d.rowid=search_fts.rowid
LEFT JOIN files f ON f.generation_id=d.generation_id AND f.path=d.path
WHERE search_fts MATCH ? AND d.generation_id=? ORDER BY d.path,d.item_id LIMIT ?`

var _ search.Reader = (*Store)(nil)

// Search reads one active or pinned published generation in one transaction.
// It never returns source bodies and cannot prove live-workspace freshness.
// Authorization callbacks must enforce the current actor/policy and may not
// expose these internal results until their enclosing scope has been checked.
func (s *Store) Search(ctx context.Context, request search.Request) (output search.Result, resultErr error) {
	if s == nil || ctx == nil || s.lease == nil || !withinPoolLimit(s.readers, 4) || !withinPoolLimit(s.writer, 1) ||
		!validHash(request.ProfileID) || request.GenerationID != "" && !validUUID(request.GenerationID) ||
		request.AuthorizeGeneration == nil || request.AuthorizePath == nil || request.Scanner == nil ||
		request.Limit < 1 || request.Limit > 100 || request.MaxOutputBytes < 4096 || request.MaxOutputBytes > 1024*1024 ||
		request.MaxManifestBytes < 1 || request.MaxManifestBytes > 64*1024*1024 || request.MaxFileBytes < 1 || request.MaxFileBytes > 64*1024*1024 {
		return output, ErrInvalidSearch
	}
	if _, ok := ctx.Deadline(); !ok {
		return output, ErrInvalidSearch
	}
	switch request.Mode {
	case "exact", "prefix", "text":
		if request.Position != nil || len(request.Query) < 1 || len(request.Query) > 2048 || !utf8.ValidString(request.Query) || utf8.RuneCountInString(request.Query) > 512 || strings.TrimSpace(request.Query) == "" || request.Mode == "text" && (len(request.Query) > 512 || request.Qualified) {
			return output, ErrInvalidSearch
		}
		for _, r := range request.Query {
			if unicode.IsControl(r) {
				return output, ErrInvalidSearch
			}
		}
		if err := request.Scanner.CheckContent([]byte(request.Query)); err != nil {
			return output, err
		}
	case "position":
		if request.Position == nil || request.Query != "" || request.Qualified || request.Position.Kind != "source_position" || !validHash(request.Position.ContentHash) || request.Position.ByteOffset < 0 || request.Position.ByteOffset > 9007199254740991 {
			return output, ErrInvalidSearch
		}
		if err := policy.CheckPath(request.Position.Path); err != nil {
			return output, err
		}
		if err := request.Scanner.CheckContent([]byte(request.Position.Path)); err != nil {
			return output, err
		}
	default:
		return output, ErrInvalidSearch
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return output, err
	}
	conn, err := borrowConfigured(ctx, s.readers, true)
	if err != nil {
		return output, err
	}
	defer func() {
		if err := releaseConfigured(ctx, conn, true); err != nil {
			output = search.Result{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := requireWAL(ctx, conn, false); err != nil {
		return output, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return output, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			output = search.Result{}
			resultErr = errors.Join(resultErr, err)
		}
	}()
	version, identity, err := inspectStore(ctx, tx, s.identity.WorktreeID)
	if err != nil {
		return output, err
	}
	if version != supportedSchema || identity != s.identity {
		return output, ErrStoreChanged
	}
	var generation, digest, state, policyDigest, provider string
	var epoch, manifestLen int64
	query := `SELECT g.generation_id,g.snapshot_digest,g.state,g.policy_digest,g.policy_epoch,g.provider_fingerprint,length(CAST(g.manifest_json AS BLOB)) FROM generations g JOIN active_generations a ON a.profile_id=g.profile_id AND a.generation_id=g.generation_id WHERE g.profile_id=? AND g.state IN ('READY','READY_PARTIAL')`
	args := []any{request.ProfileID}
	if request.GenerationID != "" {
		query = `SELECT generation_id,snapshot_digest,state,policy_digest,policy_epoch,provider_fingerprint,length(CAST(manifest_json AS BLOB)) FROM generations WHERE profile_id=? AND generation_id=? AND state IN ('READY','READY_PARTIAL')`
		args = append(args, request.GenerationID)
	}
	err = tx.QueryRowContext(ctx, query, args...).Scan(&generation, &digest, &state, &policyDigest, &epoch, &provider, &manifestLen)
	if errors.Is(err, sql.ErrNoRows) {
		return output, ErrSnapshotUnavailable
	}
	if err != nil {
		return output, err
	}
	if !validUUID(generation) || !validHash(digest) || !validHash(policyDigest) || !validHash(provider) || epoch < 1 || manifestLen < 1 {
		return output, ErrCaptureMismatch
	}
	if manifestLen > int64(request.MaxManifestBytes) {
		return output, ErrCaptureLimit
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, request.ProfileID, policyDigest, epoch); err != nil {
		return output, err
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT manifest_json FROM generations WHERE generation_id=?`, generation).Scan(&raw); err != nil {
		return output, err
	}
	if len(raw) != int(manifestLen) {
		return output, ErrCaptureMismatch
	}
	manifest, err := model.DecodeCaptureManifestJSON(raw, request.MaxManifestBytes)
	if err != nil || model.VerifyCaptureDigest(manifest, request.MaxManifestBytes) != nil || manifest.WorktreeID != s.identity.WorktreeID || manifest.ProfileID != request.ProfileID || manifest.PolicyDigest != policyDigest || manifest.ProviderDigest != provider || manifest.SnapshotDigest != digest {
		return output, ErrCaptureMismatch
	}
	entries := make(map[string]model.ManifestFile, len(manifest.Files))
	for _, entry := range manifest.Files {
		entries[entry.Path] = entry
	}
	seenPaths := make(map[string]bool)
	checkPath := func(filename string) error {
		if seenPaths[filename] {
			return nil
		}
		if err := policy.CheckPath(filename); err != nil {
			return err
		}
		if err := request.Scanner.CheckContent([]byte(filename)); err != nil {
			return err
		}
		if err := request.AuthorizePath(ctx, s.identity.WorktreeID, filename, policyDigest, epoch); err != nil {
			return err
		}
		seenPaths[filename] = true
		return nil
	}
	result := search.Result{WorktreeID: s.identity.WorktreeID, IndexIncarnation: identity.Incarnation, ProfileID: request.ProfileID, GenerationID: generation, SnapshotDigest: digest, State: state, Coverage: "unknown", Freshness: "not_checked", Symbols: []search.SymbolHit{}, Text: []search.TextHit{}}
	if request.Mode == "position" {
		if err := checkPath(request.Position.Path); err != nil {
			return output, err
		}
		if err := validateSearchPosition(ctx, tx, generation, entries, request); err != nil {
			return output, err
		}
	}
	if request.Mode == "text" {
		if err := searchTextRows(ctx, tx, generation, entries, request, checkPath, &result); err != nil {
			return output, err
		}
	} else {
		if err := searchSymbolRows(ctx, tx, generation, entries, request, checkPath, &result); err != nil {
			return output, err
		}
		result.Ambiguous = len(result.Symbols) > 1 || result.Truncated
	}
	paths := make([]string, 0, len(seenPaths))
	for filename := range seenPaths {
		paths = append(paths, filename)
	}
	sort.Strings(paths)
	for _, filename := range paths {
		if err := request.AuthorizePath(ctx, s.identity.WorktreeID, filename, policyDigest, epoch); err != nil {
			return output, err
		}
	}
	if err := request.AuthorizeGeneration(ctx, s.identity.WorktreeID, request.ProfileID, policyDigest, epoch); err != nil {
		return output, err
	}
	if err := s.lease.Check(ctx, s.identity.WorktreeID, ports.SharedLease); err != nil {
		return output, err
	}
	if err := ctx.Err(); err != nil {
		return output, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return output, err
	}
	if len(encoded) > request.MaxOutputBytes {
		return output, ErrCaptureLimit
	}
	return result, nil
}

func validateSearchPosition(ctx context.Context, tx *sql.Tx, generation string, entries map[string]model.ManifestFile, request search.Request) error {
	position := request.Position
	entry, ok := entries[position.Path]
	if !ok || entry.Status != "captured" || entry.ContentHash == nil || entry.ByteLength == nil || *entry.ContentHash != position.ContentHash {
		return ErrCaptureMismatch
	}
	var hash, status string
	var length, blobLength int64
	err := tx.QueryRowContext(ctx, `SELECT f.content_hash,f.status,f.byte_length,length(b.bytes) FROM files f JOIN content_blobs b ON b.content_hash=f.content_hash WHERE f.generation_id=? AND f.path=?`, generation, position.Path).Scan(&hash, &status, &length, &blobLength)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPublishedFileUnavailable
	}
	if err != nil {
		return err
	}
	if hash != *entry.ContentHash || length != *entry.ByteLength || blobLength != length || length < 0 || status != "indexed" && status != "parse_error" {
		return ErrCaptureMismatch
	}
	if length > int64(request.MaxFileBytes) {
		return ErrCaptureLimit
	}
	var data []byte
	if err := tx.QueryRowContext(ctx, `SELECT b.bytes FROM files f JOIN content_blobs b ON b.content_hash=f.content_hash WHERE f.generation_id=? AND f.path=?`, generation, position.Path).Scan(&data); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != length || hex.EncodeToString(sum[:]) != hash || !utf8.Valid(data) || position.ByteOffset > length || !sourceBoundary(data, int(position.ByteOffset)) {
		return ErrCaptureMismatch
	}
	return request.Scanner.CheckContent(data)
}

// prefixUpper returns the smallest valid Unicode string above every string
// beginning with prefix; an absent upper bound means prefix is all max runes.
func prefixUpper(prefix string) (string, bool) {
	runes := []rune(prefix)
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] < utf8.MaxRune {
			runes[i]++
			if runes[i] >= 0xd800 && runes[i] <= 0xdfff {
				runes[i] = 0xe000
			}
			return string(runes[:i+1]), true
		}
	}
	return "", false
}

func symbolSearchQuery(request search.Request, generation string) (string, []any) {
	column := "s.name"
	if request.Qualified {
		column = "s.qualified_name"
	}
	where := "s.generation_id=? AND "
	args := []any{generation}
	order := column + ",s.symbol_id"
	switch request.Mode {
	case "exact":
		where += column + "=?"
		args = append(args, request.Query)
	case "prefix":
		where += column + ">=?"
		args = append(args, request.Query)
		if upper, ok := prefixUpper(request.Query); ok {
			where += " AND " + column + "<?"
			args = append(args, upper)
		}
	case "position":
		// A file symbol covers every byte and is not a concrete declaration at
		// the selected position. Preserve ambiguity among actual overlapping
		// symbols rather than choosing an arbitrary nearest declaration.
		where += "s.path=? AND s.kind<>'file' AND s.start_byte<=? AND s.end_byte>?"
		args = append(args, request.Position.Path, request.Position.ByteOffset, request.Position.ByteOffset)
		order = "s.start_byte,s.end_byte,s.symbol_id"
	}
	args = append(args, request.Limit+1)
	return `SELECT ` + searchSymbolFields + ` FROM symbols s LEFT JOIN files f ON f.generation_id=s.generation_id AND f.path=s.path WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ?`, args
}

func searchSymbolRows(ctx context.Context, tx *sql.Tx, generation string, entries map[string]model.ManifestFile, request search.Request, checkPath func(string) error, result *search.Result) (resultErr error) {
	query, args := symbolSearchQuery(request, generation)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	envelope, err := json.Marshal(result)
	if err != nil {
		return err
	}
	used := len(envelope)
	if used > request.MaxOutputBytes {
		return ErrCaptureLimit
	}
	for rows.Next() {
		var id, filename, packageKey, kind, name, qualified, scope, signature, hash, status sql.NullString
		var pathLen, packageLen, start, end, length sql.NullInt64
		var exported int
		if err := rows.Scan(&id, &filename, &pathLen, &packageKey, &packageLen, &kind, &name, &qualified, &scope, &start, &end, &signature, &exported, &hash, &length, &status); err != nil {
			return err
		}
		if !id.Valid || !kind.Valid || !name.Valid || !qualified.Valid || !scope.Valid || !signature.Valid || pathLen.Valid && !filename.Valid || packageLen.Valid && !packageKey.Valid || exported < 0 || exported > 1 {
			return ErrPublishIntegrity
		}
		symbol := model.SymbolFact{SymbolID: id.String, Kind: kind.String, Name: name.String, QualifiedName: qualified.String, ScopeKey: scope.String, Signature: signature.String, Exported: exported == 1}
		if packageKey.Valid {
			symbol.PackageKey = &packageKey.String
		}
		var ref *model.SourceRef
		if filename.Valid {
			if !start.Valid || !end.Valid || !hash.Valid || !length.Valid || !status.Valid {
				return ErrPublishIntegrity
			}
			entry, ok := entries[filename.String]
			if !ok || entry.Status != "captured" || entry.ContentHash == nil || entry.ByteLength == nil || *entry.ContentHash != hash.String || *entry.ByteLength != length.Int64 || !validHash(hash.String) || (status.String != "indexed" && status.String != "parse_error") || start.Int64 < 0 || end.Int64 < start.Int64 || end.Int64 > length.Int64 {
				return ErrCaptureMismatch
			}
			if err := checkPath(filename.String); err != nil {
				return err
			}
			symbol.Path, symbol.Range = &filename.String, &model.ByteRange{StartByte: start.Int64, EndByte: end.Int64}
			ref = &model.SourceRef{WorktreeID: result.WorktreeID, ProfileID: result.ProfileID, GenerationID: generation, Path: filename.String, ContentHash: hash.String, Range: *symbol.Range}
		} else if start.Valid || end.Valid || hash.Valid || length.Valid || status.Valid {
			return ErrPublishIntegrity
		}
		for _, value := range []string{symbol.SymbolID, symbol.Kind, symbol.Name, symbol.QualifiedName, symbol.ScopeKey, symbol.Signature} {
			if err := request.Scanner.CheckContent([]byte(value)); err != nil {
				return err
			}
		}
		if symbol.PackageKey != nil {
			if err := request.Scanner.CheckContent([]byte(*symbol.PackageKey)); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(symbol)
		if err != nil {
			return err
		}
		if _, err := model.DecodeSymbolFactJSON(encoded, 1024*1024); err != nil {
			return ErrPublishIntegrity
		}
		if len(result.Symbols) == request.Limit {
			result.Truncated = true
			break
		}
		hit := search.SymbolHit{Symbol: symbol, Source: ref}
		encoded, err = json.Marshal(hit)
		if err != nil {
			return err
		}
		added := len(encoded)
		if len(result.Symbols) > 0 {
			added++
		}
		if added > request.MaxOutputBytes-used {
			return ErrCaptureLimit
		}
		used += added
		result.Symbols = append(result.Symbols, hit)
	}
	return rows.Err()
}

func searchTextRows(ctx context.Context, tx *sql.Tx, generation string, entries map[string]model.ManifestFile, request search.Request, checkPath func(string) error, result *search.Result) (resultErr error) {
	phrase := `body : "` + strings.ReplaceAll(request.Query, `"`, `""`) + `"`
	rows, err := tx.QueryContext(ctx, textSearchSQL, phrase, generation, request.Limit+1)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	envelope, err := json.Marshal(result)
	if err != nil {
		return err
	}
	used := len(envelope)
	if used > request.MaxOutputBytes {
		return ErrCaptureLimit
	}
	for rows.Next() {
		var id, filename, kind, name, qualified, hash, status sql.NullString
		var length sql.NullInt64
		if err := rows.Scan(&id, &filename, &kind, &name, &qualified, &hash, &length, &status); err != nil {
			return err
		}
		if !id.Valid || !filename.Valid || !kind.Valid || !name.Valid || !qualified.Valid || !hash.Valid || !length.Valid || !status.Valid {
			return ErrPublishIntegrity
		}
		entry, ok := entries[filename.String]
		if !ok || entry.Status != "captured" || entry.ContentHash == nil || entry.ByteLength == nil || *entry.ContentHash != hash.String || *entry.ByteLength != length.Int64 || !validHash(hash.String) || id.String != documentID(filename.String) || kind.String != "file" || name.String != path.Base(filename.String) || qualified.String != filename.String || (status.String != "indexed" && status.String != "parse_error") {
			return ErrCaptureMismatch
		}
		if err := checkPath(filename.String); err != nil {
			return err
		}
		if len(result.Text) == request.Limit {
			result.Truncated = true
			break
		}
		hit := search.TextHit{Path: filename.String, ContentHash: hash.String, EvidenceKind: "text_match"}
		encoded, err := json.Marshal(hit)
		if err != nil {
			return err
		}
		added := len(encoded)
		if len(result.Text) > 0 {
			added++
		}
		if added > request.MaxOutputBytes-used {
			return ErrCaptureLimit
		}
		used += added
		result.Text = append(result.Text, hit)
	}
	return rows.Err()
}
