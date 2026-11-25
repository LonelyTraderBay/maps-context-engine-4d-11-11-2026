package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

type NeighborhoodRequest struct {
	File                      PublishedFileRequest
	Root, Direction           string
	Kinds                     []string
	Depth, MaxNodes, MaxEdges int
	MaxSourceBytes            int64
	FollowJSXReads            bool
}

type PublishedNeighborhood struct {
	Generation GenerationObservation
	Symbols    []model.SymbolFact
	Relations  []model.RelationFact
	Files      map[string]PublishedFile
	Coverage   model.Coverage
	Limited    bool
	JSXReads   map[string][]JSXVariableRead
}

type JSXVariableRead struct {
	Use         model.ByteRange
	Declaration model.SymbolFact
}

// ReadNeighborhood follows the indexes on relation endpoints in one pinned
// read transaction and verifies only the captured files the result needs.
func (s *Store) ReadNeighborhood(ctx context.Context, r NeighborhoodRequest) (PublishedNeighborhood, error) {
	if r.File.Scanner == nil || r.File.AuthorizePath == nil || r.File.MaxFileBytes < 1 ||
		r.MaxSourceBytes < 1 || r.Root == "" || len(r.Root) > 512 || r.Depth < 1 || r.Depth > 4 ||
		r.MaxNodes < 1 || r.MaxNodes > 48 || r.MaxEdges < 1 || r.MaxEdges > 64 ||
		len(r.Kinds) > 32 || r.Direction != "incoming" && r.Direction != "outgoing" && r.Direction != "both" {
		return PublishedNeighborhood{}, ErrInvalidStore
	}
	for _, kind := range r.Kinds {
		if !model.ValidRelationKind(kind) {
			return PublishedNeighborhood{}, ErrInvalidStore
		}
	}
	view := PublishedNeighborhood{Files: map[string]PublishedFile{}}
	generation, err := s.readGeneration(ctx, r.File.ProfileID, r.File.GenerationID, r.File.AuthorizeGeneration,
		r.File.MaxManifestBytes, func(tx *sql.Tx, generation GenerationObservation, manifestBytes []byte) error {
			manifest, err := model.DecodeCaptureManifestJSON(manifestBytes, r.File.MaxManifestBytes)
			if err != nil {
				return err
			}
			entries := make(map[string]model.ManifestFile, len(manifest.Files))
			for _, file := range manifest.Files {
				entries[file.Path] = file
			}
			var sourceBytes int64
			checkRange := func(path string, span model.ByteRange) error {
				file, ok := view.Files[path]
				if !ok {
					entry, captured := entries[path]
					if !captured || entry.Status != "captured" || entry.ContentHash == nil || entry.ByteLength == nil {
						return ErrCaptureMismatch
					}
					if *entry.ByteLength > r.MaxSourceBytes-sourceBytes {
						return ErrCaptureLimit
					}
					if err := r.File.AuthorizePath(ctx, generation.Identity.WorktreeID, path, generation.PolicyDigest, generation.PolicyEpoch); err != nil {
						return err
					}
					data, err := readPublishedBlob(ctx, tx, generation.GenerationID, entry, r.File.Scanner, r.File.MaxFileBytes)
					if err != nil {
						return err
					}
					file = PublishedFile{Path: path, ContentHash: *entry.ContentHash, Bytes: data}
					view.Files[path] = file
					sourceBytes += int64(len(data))
				}
				start, end := span.StartByte, span.EndByte
				if start < 0 || end < start || end > int64(len(file.Bytes)) ||
					!sourceBoundary(file.Bytes, int(start)) || !sourceBoundary(file.Bytes, int(end)) {
					return ErrCaptureMismatch
				}
				return nil
			}
			root, err := neighborhoodSymbol(ctx, tx, generation.GenerationID, r.Root)
			if err != nil {
				return err
			}
			ids, relations, limited, err := walkNeighborhood(ctx, tx, generation.GenerationID, r,
				root.Kind == "route" && r.Direction == "outgoing")
			if err != nil {
				return err
			}
			view.Limited = limited
			verifySymbol := func(symbol model.SymbolFact) error {
				raw, err := json.Marshal(symbol)
				if err != nil || r.File.Scanner.CheckContent(raw) != nil {
					return ErrPublishIntegrity
				}
				if _, err := model.DecodeSymbolFactJSON(raw, 256<<10); err != nil {
					return ErrPublishIntegrity
				}
				if symbol.Path != nil {
					return checkRange(*symbol.Path, *symbol.Range)
				}
				return nil
			}
			for _, id := range ids {
				symbol := root
				if id != r.Root {
					symbol, err = neighborhoodSymbol(ctx, tx, generation.GenerationID, id)
					if err != nil {
						return err
					}
				}
				if err := verifySymbol(symbol); err != nil {
					return err
				}
				view.Symbols = append(view.Symbols, symbol)
			}
			for _, relation := range relations {
				raw, err := json.Marshal(relation)
				if err != nil || r.File.Scanner.CheckContent(raw) != nil {
					return ErrPublishIntegrity
				}
				if _, err := model.DecodeRelationFactJSON(raw, 16<<10); err != nil || !validRelationEvidence(relation) {
					return ErrPublishIntegrity
				}
				if err := checkRange(relation.SitePath, relation.Range); err != nil {
					return err
				}
				view.Relations = append(view.Relations, relation)
			}
			if r.FollowJSXReads {
				view.JSXReads, err = readRouteJSXVariables(ctx, tx, generation.GenerationID, r.Root, view, checkRange, verifySymbol)
				if err != nil {
					return err
				}
			}
			view.Coverage, _, err = readPublishedCoverage(ctx, tx, generation.GenerationID, generation.State, r.File.Scanner, 16<<10)
			return err
		})
	if err != nil {
		return PublishedNeighborhood{}, err
	}
	view.Generation = generation
	return view, nil
}

func neighborhoodSymbol(ctx context.Context, tx *sql.Tx, generation, id string) (_ model.SymbolFact, resultErr error) {
	rows, err := tx.QueryContext(ctx, providerSymbolFactSelect+` WHERE generation_id=? AND symbol_id=?
		AND length(CAST(name AS BLOB))+length(CAST(qualified_name AS BLOB))+length(CAST(scope_key AS BLOB))+
		length(CAST(signature AS BLOB))+coalesce(length(CAST(path AS BLOB)),0)+coalesce(length(CAST(package_key AS BLOB)),0)<=262144`,
		generation, id)
	if err != nil {
		return model.SymbolFact{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	if !rows.Next() {
		return model.SymbolFact{}, errors.Join(ErrPublishedFileUnavailable, rows.Err())
	}
	batch := model.ProviderBatch{}
	if _, err := scanProviderFact(rows, "symbols", &batch); err != nil {
		return model.SymbolFact{}, err
	}
	return batch.Symbols[0], rows.Err()
}

func walkNeighborhood(ctx context.Context, tx *sql.Tx, generation string, r NeighborhoodRequest, followPaths bool) ([]string, []model.RelationFact, bool, error) {
	ids := []string{r.Root}
	known := map[string]bool{r.Root: true}
	seen := make(map[string]bool)
	var edges []model.RelationFact
	limited := false
	nodeCount := 1
	directions := []string{r.Direction}
	if r.Direction == "both" {
		directions = []string{"outgoing", "incoming"}
	}
	expand := func(id string, next func(string) error) error {
		for _, direction := range directions {
			found, err := neighborhoodEdges(ctx, tx, generation, id, direction, r.Kinds, r.MaxEdges+1)
			if err != nil {
				return err
			}
			for _, edge := range found {
				if seen[edge.RelationID] {
					continue
				}
				other := edge.SourceSymbolID
				if direction == "outgoing" {
					if edge.TargetSymbolID == nil {
						// The projector reserves one node per unresolved site.
						other = ""
					} else {
						other = *edge.TargetSymbolID
					}
				}
				newNode := other == "" || !known[other]
				if len(edges) == r.MaxEdges || newNode && nodeCount == r.MaxNodes {
					limited = true
					continue
				}
				edges = append(edges, edge)
				seen[edge.RelationID] = true
				if newNode {
					nodeCount++
				}
				if other != "" && newNode {
					known[other] = true
					ids = append(ids, other)
					if err := next(other); err != nil {
						return err
					}
				}
			}
			limited = limited || len(found) > r.MaxEdges
		}
		return nil
	}
	if followPaths {
		// Finish a route's first connected paths before spending the bounded
		// result on siblings. Parent edges precede every descendant.
		var visit func(string, int) error
		visit = func(id string, depth int) error {
			if depth == r.Depth {
				return ctx.Err()
			}
			return expand(id, func(other string) error { return visit(other, depth+1) })
		}
		if err := visit(r.Root, 0); err != nil {
			return nil, nil, false, err
		}
	} else {
		frontier := []string{r.Root}
		for depth := 0; depth < r.Depth && len(frontier) != 0; depth++ {
			next := []string{}
			for _, id := range frontier {
				if err := expand(id, func(other string) error { next = append(next, other); return nil }); err != nil {
					return nil, nil, false, err
				}
			}
			frontier = next
		}
	}
	return ids, edges, limited, nil
}

func neighborhoodEdges(ctx context.Context, tx *sql.Tx, generation, id, direction string, kinds []string, limit int) (_ []model.RelationFact, resultErr error) {
	endpoint := "source_symbol_id"
	if direction == "incoming" {
		endpoint = "target_symbol_id"
	}
	query := `SELECT relation_id,source_symbol_id,target_symbol_id,kind,evidence_kind,site_path,start_byte,end_byte,reason,unresolved_label
		FROM relations WHERE generation_id=? AND ` + endpoint + `=?`
	args := []any{generation, id}
	if len(kinds) != 0 {
		query += " AND kind IN (" + strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + ")"
		for _, kind := range kinds {
			args = append(args, kind)
		}
	}
	query += ` ORDER BY CASE kind
		WHEN 'ROUTE_COMPONENT' THEN 0 WHEN 'BINDS_EVENT' THEN 1 WHEN 'RENDERS_JSX' THEN 2
		WHEN 'RENDERS_COMPONENT' THEN 3 WHEN 'CHECKS_CONDITION' THEN 4 WHEN 'BRANCH_TRUE' THEN 5
		WHEN 'BRANCH_FALSE' THEN 6 WHEN 'CALLS_DIRECT' THEN 7 WHEN 'CALLS_INTERFACE' THEN 8
		WHEN 'READS' THEN 9 WHEN 'WRITES' THEN 10 WHEN 'IMPORTS' THEN 11 WHEN 'REFERS_TO' THEN 12
		ELSE 13 END, site_path,start_byte,relation_id LIMIT ?`
	rows, err := tx.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	batch := model.ProviderBatch{}
	for rows.Next() {
		if _, err := scanProviderFact(rows, "relations", &batch); err != nil {
			return nil, err
		}
	}
	return batch.Relations, rows.Err()
}
