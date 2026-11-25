package search

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

var goGroupedNumber = regexp.MustCompile(`\b[0-9]{1,3}([.,][0-9]{3})+\b`)

type GoQuestionFile struct {
	Path  string
	Bytes []byte
}

type GoQuestionSection struct {
	Path  string
	Range model.ByteRange
}

type GoQuestion struct {
	terms       map[string]float64
	aspects     []map[string]float64
	tests       bool
	preferValue bool
	preferField bool
	qualifier   []string
}

func NewGoQuestion(question string) GoQuestion {
	replacer := strings.NewReplacer(" và ", "\n", " rồi ", "\n", " sau đó ", "\n", " and ", "\n", " then ", "\n", "?", "\n", ";", "\n")
	var aspects []map[string]float64
	for _, part := range strings.Split(replacer.Replace(question), "\n") {
		terms := goQuestionTerms(part)
		if len(terms) >= 2 && len(aspects) < 5 {
			aspects = append(aspects, terms)
		}
	}
	folded := foldVietnameseWord(question)
	words := " " + strings.Join(questionWords(folded), " ") + " "
	var qualifier []string
	if strings.Contains(words, " tham chieu ma nguon ") {
		qualifier = []string{"source", "ref"}
	}
	return GoQuestion{terms: goQuestionTerms(question), aspects: aspects,
		tests:       strings.Contains(folded, "kiem thu") || strings.Contains(folded, "unit test"),
		preferValue: strings.Contains(words, " ttl ") || strings.Contains(words, " hang so "),
		preferField: strings.Contains(words, " ten truong ") || strings.Contains(words, " rename field "),
		qualifier:   qualifier}
}

func goQuestionTerms(question string) map[string]float64 {
	terms := make(map[string]float64)
	for _, raw := range questionWords(question) {
		word := strings.ToLower(raw)
		if word == foldVietnameseWord(raw) {
			for term := range goSearchWords(raw) {
				terms[term] = 2
			}
		}
	}
	folded := foldVietnameseWord(question)
	words := questionWords(folded)
	phraseText := " " + strings.Join(words, " ") + " "
	for phrase, synonyms := range goVietnameseConcepts {
		if strings.Contains(phraseText, " "+phrase+" ") {
			for term := range goSearchWords(synonyms) {
				terms[term] = max(terms[term], 1)
			}
		}
	}
	if strings.Contains(phraseText, " doc chuoi ") {
		for _, term := range []string{"parse", "decode", "unmarshal"} {
			terms[term] = max(terms[term], 4)
		}
	}
	if strings.Contains(phraseText, " dong ho khong tien ") {
		for _, term := range []string{"backwards", "increment", "lasttime", "now"} {
			terms[term] = max(terms[term], 4)
		}
	}
	for i := 0; i+2 < len(words); i++ {
		if words[i] == "phien" && words[i+1] == "ban" && len(words[i+2]) == 1 && words[i+2][0] >= '0' && words[i+2][0] <= '9' {
			terms["version"+words[i+2]], terms["v"+words[i+2]] = 3, 3
		}
	}
	for _, word := range words {
		if len(word) == 2 && word[0] == 'v' && word[1] >= '0' && word[1] <= '9' {
			terms["version"+word[1:]], terms[word] = 6, 6
		}
	}
	// Also consider a grouped number as a Go integer literal. Leave the
	// original terms available for decimal input and quoted messages.
	for _, grouped := range goGroupedNumber.FindAllString(question, -1) {
		digits := strings.ReplaceAll(strings.ReplaceAll(grouped, ".", ""), ",", "")
		terms[digits] = max(terms[digits], 6)
	}
	return terms
}

func (q GoQuestion) AllowsTestFiles() bool { return q.tests }

func (q GoQuestion) PathRank(path string) float64 {
	return q.weight(goSearchWords(path))
}

type goSectionScore struct {
	GoQuestionSection
	unit  int
	score float64
	words map[string]bool
}

type localGoName struct {
	dir, pkg, name string
}

// Sections ranks real syntax ranges and overlapping windows in long declarations.
// It needs neither a type-checking subprocess nor a live filesystem read.
func (q GoQuestion) Sections(ctx context.Context, files []GoQuestionFile, limit int) ([]GoQuestionSection, error) {
	var candidates []goSectionScore
	presence := make(map[string]int)
	localUnits := make(map[localGoName]int)
	localCalls := make(map[int][]localGoName)
	units := 0
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		set := token.NewFileSet()
		parsed, _ := parser.ParseFile(set, file.Path, file.Bytes, parser.ParseComments|parser.SkipObjectResolution)
		if parsed == nil || parsed.Name == nil {
			continue
		}
		pathWords := goSearchWords(file.Path)
		pathWeight := .45 * q.weight(pathWords)
		if len(q.qualifier) == 2 && pathWords[q.qualifier[0]] && pathWords[q.qualifier[1]] {
			pathWeight += 10
		}
		for _, decl := range parsed.Decls {
			start, end := set.Position(decl.Pos()).Offset, set.Position(decl.End()).Offset
			if start < 0 || start >= end || end > len(file.Bytes) {
				continue
			}
			names, docs := declarationWords(decl)
			body := goSearchWords(string(file.Bytes[start:end]))
			relevant := q.weight(body) + q.weight(names) + q.weight(docs)
			if relevant == 0 {
				continue
			}
			units++
			if fn, ok := decl.(*ast.FuncDecl); ok {
				id := localGoName{dir: path.Dir(file.Path), pkg: parsed.Name.Name}
				if fn.Recv == nil {
					name := id
					name.name = fn.Name.Name
					if _, duplicate := localUnits[name]; duplicate {
						localUnits[name] = 0
					} else {
						localUnits[name] = units
					}
				}
				ast.Inspect(fn, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok {
						if ident, ok := call.Fun.(*ast.Ident); ok {
							name := id
							name.name = ident.Name
							localCalls[units] = append(localCalls[units], name)
						}
					}
					return true
				})
			}
			for term := range q.terms {
				if body[term] || names[term] || docs[term] {
					presence[term]++
				}
			}
			for _, span := range goDeclarationWindows(file.Bytes, start, end) {
				words := goSearchWords(string(file.Bytes[span.StartByte:span.EndByte]))
				// Keep declarations with a strong heading even if their body
				// uses terminology different from the question.
				score := q.weight(words) + 1.1*q.weight(names) + .7*q.weight(docs) + pathWeight
				score += q.editDeclarationRank(decl, words)
				if end-start <= 420 && q.weight(names) > 0 {
					// Short, named implementations often carry a precise
					// discriminator such as a unit, field, or conversion.
					score += 1.2*q.weight(names) + .9*q.weight(docs)
				}
				if len(words) != 0 && score > 0 {
					candidates = append(candidates, goSectionScore{
						GoQuestionSection: GoQuestionSection{Path: file.Path, Range: span},
						unit:              units, score: score, words: words,
					})
				}
			}
		}
	}
	keys := make([]string, 0, len(q.terms))
	for term := range q.terms {
		keys = append(keys, term)
	}
	sort.Strings(keys)
	for i := range candidates {
		for _, term := range keys {
			if candidates[i].words[term] && presence[term] > 0 {
				candidates[i].score += q.terms[term] * math.Log1p(float64(units)/float64(presence[term]))
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) == 0 || limit < 1 {
		return nil, nil
	}
	top := candidates[0].score
	chosenUnits, chosenFiles := make(map[int]int), make(map[string]int)
	var result []GoQuestionSection
	selectCandidate := func(i int) {
		hit := &candidates[i]
		result = append(result, hit.GoQuestionSection)
		chosenUnits[hit.unit]++
		chosenFiles[hit.Path]++
		hit.score = 0
	}
	leadingUnit := candidates[0].unit
	selectCandidate(0)
	// A short function called by the leading declaration can contain a
	// requested check that is absent from the caller's own source.
	helpers := make(map[int]bool)
	for _, call := range localCalls[leadingUnit] {
		if unit := localUnits[call]; unit != 0 && unit != leadingUnit {
			helpers[unit] = true
		}
	}
	for i, candidate := range candidates {
		if len(result) < limit && helpers[candidate.unit] && candidate.Range.EndByte-candidate.Range.StartByte <= 900 {
			selectCandidate(i)
			break
		}
	}
	if len(q.aspects) > 1 {
		for _, aspect := range q.aspects {
			best, bestScore := -1, 0.0
			for i, candidate := range candidates {
				// Include a locally focused section for each substantive part
				// of the question before filling the remaining budget.
				if candidate.score == 0 || goSectionOverlaps(candidate.GoQuestionSection, result) {
					continue
				}
				var matches int
				score := 0.0
				for _, term := range keys {
					if candidate.words[term] && aspect[term] > 0 {
						matches++
						score += aspect[term] * math.Log1p(float64(units)/float64(max(1, presence[term])))
					}
				}
				score /= 4 + math.Sqrt(float64(len(candidate.words)))
				if matches >= 2 && score > bestScore {
					best, bestScore = i, score
				}
			}
			if best >= 0 && len(result) < limit {
				selectCandidate(best)
			}
		}
	}
	for len(result) < limit {
		best, score := -1, 0.0
		for i, candidate := range candidates {
			if candidate.score < .15*top || chosenUnits[candidate.unit] >= 3 || chosenFiles[candidate.Path] >= 6 {
				continue
			}
			value := candidate.score / (1 + .4*float64(chosenUnits[candidate.unit]) + .16*float64(chosenFiles[candidate.Path]))
			if value > score && !goSectionOverlaps(candidate.GoQuestionSection, result) {
				best, score = i, value
			}
		}
		if best < 0 {
			break
		}
		selectCandidate(best)
	}
	return result, ctx.Err()
}

// Prefer definitions of values and fields for questions about changing their
// declarations. Require matching identifier words in the selected window.
func (q GoQuestion) editDeclarationRank(decl ast.Decl, window map[string]bool) float64 {
	group, ok := decl.(*ast.GenDecl)
	if !ok || !q.preferValue && !q.preferField {
		return 0
	}
	rank := func(name string) float64 {
		var score float64
		for word := range goSearchWords(name) {
			if window[word] {
				score += q.terms[word]
			}
		}
		return score
	}
	var best float64
	for _, spec := range group.Specs {
		switch node := spec.(type) {
		case *ast.ValueSpec:
			if q.preferValue {
				for _, name := range node.Names {
					best = max(best, rank(name.Name))
				}
			}
		case *ast.TypeSpec:
			if fields, ok := node.Type.(*ast.StructType); ok && q.preferField {
				for _, field := range fields.Fields.List {
					for _, name := range field.Names {
						best = max(best, rank(name.Name))
					}
				}
			}
		}
	}
	return 9 * best
}

func goSectionOverlaps(candidate GoQuestionSection, selected []GoQuestionSection) bool {
	for _, section := range selected {
		overlap := min(candidate.Range.EndByte, section.Range.EndByte) - max(candidate.Range.StartByte, section.Range.StartByte)
		if candidate.Path == section.Path && overlap > (candidate.Range.EndByte-candidate.Range.StartByte)/2 {
			return true
		}
	}
	return false
}

func (q GoQuestion) weight(words map[string]bool) float64 {
	var score float64
	for term, weight := range q.terms {
		if words[term] {
			score += weight
		}
	}
	return score
}

func declarationWords(decl ast.Decl) (names, docs map[string]bool) {
	switch node := decl.(type) {
	case *ast.FuncDecl:
		return goSearchWords(node.Name.Name), goSearchWords(node.Doc.Text())
	case *ast.GenDecl:
		if node.Tok == token.IMPORT {
			return nil, nil
		}
		return nil, goSearchWords(node.Doc.Text())
	default:
		return nil, nil
	}
}

func goDeclarationWindows(data []byte, start, end int) []model.ByteRange {
	lineStarts := []int{start}
	for i, b := range data[start:end] {
		if b == '\n' && start+i+1 < end {
			lineStarts = append(lineStarts, start+i+1)
		}
	}
	lineStarts = append(lineStarts, end)
	if end-start <= 1650 && len(lineStarts) <= 57 {
		return []model.ByteRange{{StartByte: int64(start), EndByte: int64(end)}}
	}
	var windows []model.ByteRange
	for first := 0; first+1 < len(lineStarts); {
		last := min(first+20, len(lineStarts)-1)
		for last > first+1 && lineStarts[last]-lineStarts[first] > 1650 {
			last--
		}
		windows = append(windows, model.ByteRange{StartByte: int64(lineStarts[first]), EndByte: int64(lineStarts[last])})
		if last == len(lineStarts)-1 {
			break
		}
		first = min(first+14, last)
	}
	return windows
}

func goSearchWords(text string) map[string]bool {
	words := make(map[string]bool)
	const stop = " go func package internal case return true false nil if for the and that with from then into this all when can are not was will any "
	add := func(word string) {
		word = strings.ToLower(word)
		if len(word) >= 2 && len(word) <= 80 && !strings.Contains(stop, " "+word+" ") {
			words[word] = true
			if len(word) >= 5 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
				words[strings.TrimSuffix(word, "s")] = true
			}
		}
	}
	for _, id := range bytes.FieldsFunc([]byte(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		add(string(id))
		runes := []rune(string(id))
		start := 0
		for i := 1; i < len(runes); i++ {
			current, previous := runes[i], runes[i-1]
			if (previous == 'v' || previous == 'V') && current >= '0' && current <= '9' &&
				(i+1 == len(runes) || !unicode.IsDigit(runes[i+1])) {
				add(string(runes[i-1 : i+1]))
			}
			if unicode.IsDigit(current) != unicode.IsDigit(previous) ||
				unicode.IsUpper(current) && (unicode.IsLower(previous) || i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
				add(string(runes[start:i]))
				start = i
			}
		}
		add(string(runes[start:]))
	}
	return words
}
