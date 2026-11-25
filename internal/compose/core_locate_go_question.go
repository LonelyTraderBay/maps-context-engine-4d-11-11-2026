package compose

import (
	"bytes"
	"context"
	"errors"
	"unicode/utf8"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/contextpack"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/search"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite/index"
)

const goLocateExcerptLimit = 24

// locateGoEvidence reuses the pinned scope and the same read authority used
// by locate. The Go question path deliberately does not guess package or kind
// filters from syntax ranges.
func (b *CoreBackend) locateGoEvidence(ctx context.Context, principal app.CorePrincipal, request model.LocateRequest,
	options app.CoreCallOptions, scope ScopeObservation, searchRequest search.Request) ([][]byte, error) {
	if scope.Binding.Profile == nil || request.Query == nil || request.Cursor != nil {
		return nil, app.ErrCoreInput
	}
	if request.Match == "question" && (len(request.Kinds) != 0 || request.Package != nil) {
		return nil, app.ErrCoreInput
	}
	member := scope.Vector.Members[0]
	searchRequest.ProfileID, searchRequest.GenerationID = member.ProfileID, member.GenerationID
	searchRequest.Mode, searchRequest.Query = "exact", *request.Query
	searchRequest.Limit = int(min(3, request.Limit))
	searchRequest.Kinds = request.Kinds
	if request.Path != nil {
		searchRequest.Path = *request.Path
	}
	if request.Package != nil {
		searchRequest.Package = *request.Package
	}
	searchRequest.AuthorizePath = func(ctx context.Context, worktree, name, digest string, epoch int64) error {
		if worktree != member.WorktreeID || digest != member.PolicyDigest || epoch != member.PolicyEpoch {
			return ErrScopeChanged
		}
		return b.Authority.boundPath(ctx, principal, scope.Binding.Policy, name)
	}
	if request.Match == "question" {
		options.MaxOutputBytes = min(options.MaxOutputBytes, 24<<10)
		blocks, err := b.locateGoQuestion(ctx, options, scope, searchRequest)
		if err != nil || blocks != nil {
			return blocks, err
		}
		// A question with no candidate sections still returns a valid response.
		response, err := locateResponse(options, scope.Vector, search.LocatePage{})
		if err != nil {
			return nil, err
		}
		appendGoLocateGap(&response, "GO_QUESTION_CANDIDATES", "No ranked source section matched the question in the scanned snapshot.")
		return encodeResponse(response)
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, 20<<10)
	return b.locateGoName(ctx, options, scope, searchRequest)
}

func (b *CoreBackend) locateGoName(ctx context.Context, options app.CoreCallOptions, scope ScopeObservation,
	request search.Request) ([][]byte, error) {
	member := scope.Vector.Members[0]
	result, err := b.Reader.Index.Search(ctx, request)
	if err != nil {
		return nil, coreStoreError(err)
	}
	if result.WorktreeID != member.WorktreeID || result.ProfileID != member.ProfileID ||
		result.IndexIncarnation != member.IndexIncarnation || result.GenerationID != member.GenerationID ||
		result.SnapshotDigest != member.SnapshotDigest {
		return nil, ErrScopeChanged
	}
	response, err := locateResponse(options, scope.Vector, search.LocatePage{Result: result})
	if err != nil {
		return nil, err
	}
	response.Coverage.ScopeDescription = "Numbered declarations from the captured Go snapshot."
	appendGoLocateGap(&response, "BOUNDED_EVIDENCE", "Source excerpts show selected declarations.")
	if result.Truncated {
		appendGoLocateGap(&response, "CANDIDATES_LIMITED", "Narrow the lookup by file or symbol kind.")
	}
	var snippets []model.LocatedExcerpt
	files := make(map[string]index.PublishedFile)
	for _, hit := range result.Symbols {
		if hit.Source == nil || hit.Symbol.Kind == "file" {
			continue
		}
		ref := *hit.Source
		file, exists := files[ref.Path]
		if !exists {
			file, err = b.Reader.Index.ReadPublishedFile(ctx, index.PublishedFileRequest{
				ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: ref.Path,
				AuthorizeGeneration: request.AuthorizeGeneration, AuthorizePath: request.AuthorizePath,
				Scanner: request.Scanner, MaxManifestBytes: request.MaxManifestBytes, MaxFileBytes: request.MaxFileBytes,
			})
			if err != nil {
				return nil, coreStoreError(err)
			}
			if file.IndexIncarnation != member.IndexIncarnation || file.SnapshotDigest != member.SnapshotDigest {
				return nil, ErrScopeChanged
			}
			files[ref.Path] = file
		}
		if file.ContentHash != ref.ContentHash {
			return nil, ErrScopeChanged
		}
		if !utf8.Valid(file.Bytes) {
			continue
		}
		bounds, line := numberedSourceLines(file.Bytes, ref.Range, 26)
		if line == 0 || bounds.StartByte == bounds.EndByte {
			continue
		}
		ref.Range = bounds
		snippets = append(snippets, model.LocatedExcerpt{SourceRef: ref, StartLine: line,
			Content: string(file.Bytes[bounds.StartByte:bounds.EndByte])})
	}
	return fitGoLocateExcerpts(response, snippets)
}

func appendGoLocateGap(response *model.Response, code, reason string) {
	response.Status = "partial"
	response.Coverage.Status = "partial"
	if len(response.Coverage.Gaps) < 64 {
		response.Coverage.Gaps = append(response.Coverage.Gaps, model.Gap{
			Code: code, Reason: reason, SourceRefs: []model.SourceRef{},
		})
	}
}

func (b *CoreBackend) locateGoQuestion(ctx context.Context, options app.CoreCallOptions, scope ScopeObservation,
	request search.Request) ([][]byte, error) {
	member := scope.Vector.Members[0]
	query := search.NewGoQuestion(request.Query)
	if err := request.Scanner.CheckContent([]byte(request.Query)); err != nil {
		return nil, err
	}
	generation, files, limited, err := b.Reader.Index.ReadGoQuestionSources(ctx, index.PublishedFileRequest{
		ProfileID: member.ProfileID, GenerationID: member.GenerationID, Path: request.Path,
		AuthorizeGeneration: request.AuthorizeGeneration, AuthorizePath: request.AuthorizePath,
		Scanner: request.Scanner, MaxManifestBytes: request.MaxManifestBytes, MaxFileBytes: request.MaxFileBytes,
	}, query, min(2<<20, scope.Binding.Policy.MaxSourceBytes, b.limits.MaxSourceBytesPerSnapshot))
	if err != nil {
		return nil, coreStoreError(err)
	}
	if generation.Identity.Incarnation != member.IndexIncarnation || generation.SnapshotDigest != member.SnapshotDigest {
		return nil, ErrScopeChanged
	}
	sources := make([]search.GoQuestionFile, 0, len(files))
	byPath := make(map[string]index.PublishedFile, len(files))
	for _, file := range files {
		if utf8.Valid(file.Bytes) {
			sources = append(sources, search.GoQuestionFile{Path: file.Path, Bytes: file.Bytes})
			byPath[file.Path] = file
		}
	}
	sections, err := query.Sections(ctx, sources, goLocateExcerptLimit)
	if err != nil || len(sections) == 0 {
		return nil, err
	}
	var snippets []model.LocatedExcerpt
	paths := make(map[string]bool)
	var hits []search.TextHit
	for _, section := range sections {
		file := byPath[section.Path]
		bounds, line := numberedSourceLines(file.Bytes, section.Range, 56)
		if line == 0 || bounds.StartByte == bounds.EndByte {
			continue
		}
		ref := model.SourceRef{WorktreeID: member.WorktreeID, ProfileID: member.ProfileID, GenerationID: member.GenerationID,
			Path: file.Path, ContentHash: file.ContentHash, Range: bounds}
		snippets = append(snippets, model.LocatedExcerpt{SourceRef: ref, StartLine: line, Content: string(file.Bytes[bounds.StartByte:bounds.EndByte])})
		if !paths[file.Path] && len(hits) < request.Limit {
			paths[file.Path] = true
			hits = append(hits, search.TextHit{Path: file.Path, ContentHash: file.ContentHash, EvidenceKind: "text_match"})
		}
	}
	if len(snippets) == 0 {
		return nil, nil
	}
	response, err := locateResponse(options, scope.Vector, search.LocatePage{Result: search.Result{Text: hits}})
	if err != nil {
		return nil, err
	}
	response.Coverage.ScopeDescription = "Ranked, line-numbered Go declarations and statements from captured source."
	appendGoLocateGap(&response, "GO_QUESTION_CANDIDATES", "Lexical and syntax ranking returns source candidates, without proving full coverage.")
	if limited {
		appendGoLocateGap(&response, "GO_SOURCE_DISCOVERY_LIMIT", "The source scan was limited to a bounded part of the selected generation.")
	}
	return fitGoLocateExcerpts(response, snippets)
}

func fitGoLocateExcerpts(response model.Response, snippets []model.LocatedExcerpt) ([][]byte, error) {
	data := response.Data.(model.LocateData)
	for {
		data.Excerpts = snippets
		response.Data = data
		blocks, err := encodeResponse(response)
		if !errors.Is(err, contextpack.ErrBudgetTooSmall) || len(snippets) == 0 {
			return blocks, err
		}
		snippets = snippets[:len(snippets)-1]
	}
}

func numberedSourceLines(source []byte, span model.ByteRange, lines int) (model.ByteRange, int64) {
	if lines < 1 || span.StartByte < 0 || span.StartByte >= int64(len(source)) ||
		span.EndByte <= span.StartByte || span.EndByte > int64(len(source)) {
		return model.ByteRange{}, 0
	}
	start := bytes.LastIndexByte(source[:span.StartByte], '\n') + 1
	end := start
	for range lines {
		next := bytes.IndexByte(source[end:], '\n')
		if next < 0 {
			end = len(source)
			break
		}
		end += next + 1
		if end >= int(span.EndByte) {
			break
		}
	}
	end = min(end, start+1800)
	for end > start && end < len(source) && !utf8.RuneStart(source[end]) {
		end--
	}
	return model.ByteRange{StartByte: int64(start), EndByte: int64(end)}, int64(bytes.Count(source[:start], []byte{'\n'})) + 1
}
