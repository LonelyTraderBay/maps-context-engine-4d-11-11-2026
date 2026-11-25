package search

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const questionTermLimit = 16

type QuestionTerm struct {
	Text   string
	Weight int
}

// QuestionRoute extracts a literal absolute application route, including
// dynamic segments, without interpreting the rest of the question as SQL.
func QuestionRoute(question string) string {
	for _, field := range strings.Fields(question) {
		token := strings.Trim(field, "\"'`“”‘’(),;.!?")
		if len(token) < 2 || token[0] != '/' || strings.HasPrefix(token, "//") {
			continue
		}
		if strings.IndexFunc(token, func(r rune) bool {
			return !(unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Mn, r) || strings.ContainsRune("/_:.@-*", r))
		}) == -1 {
			return token
		}
	}
	return ""
}

// QuestionTerms collects a small set of lexical terms. Adjacent title words
// can identify an owning file, while action words rank symbol names.
func QuestionTerms(question string) (terms []QuestionTerm, titles []string) {
	const stopWords = " a an and are as at be before by can cite code component could did do does find for from had has have how i in is it its me of on or our route screen show tell that the their then this to trace was were what when where which who will with would wrapper you your actual binding button call delegate generated happens inside meaning same state symbol web wizard "
	add := func(word string, weight int) {
		for i, prior := range terms {
			if prior.Text == word {
				terms[i].Weight = max(prior.Weight, weight)
				return
			}
		}
		if len(terms) < questionTermLimit {
			terms = append(terms, QuestionTerm{Text: word, Weight: weight})
		}
	}
	words := questionWords(question)
	addVietnameseQuestionTerms(words, add)
	var previousTitle string
	titleWords := make(map[string]bool)
	for _, token := range words {
		word := strings.ToLower(token)
		if len(word) < 3 || len(word) > 64 || strings.Contains(stopWords, " "+word+" ") {
			previousTitle = ""
			continue
		}
		first, _ := utf8.DecodeRuneInString(token)
		if unicode.IsUpper(first) {
			if previousTitle != "" && len(titles) < 4 {
				titles = append(titles, previousTitle+word)
				titleWords[previousTitle], titleWords[word] = true, true
			}
			previousTitle = word
		} else {
			previousTitle = ""
		}
		add(word, 10)
		switch word {
		case "storage":
			add(word, 24)
		case "continue":
			add("next", 22)
		case "backward", "backwards", "previous":
			add("back", 12)
		case "saves", "saved", "saving":
			add("save", 14)
		case "exports", "exported", "exporting":
			add("export", 14)
		}
	}
	// A screen title is an owning-file hint when there is also an action or
	// state word. It should not favor a large page declaration over a handler.
	filtered := make([]QuestionTerm, 0, len(terms))
	for _, term := range terms {
		if !titleWords[term.Text] {
			filtered = append(filtered, term)
		}
	}
	if len(filtered) != 0 {
		terms = filtered
	}
	return terms, titles
}

func QuestionMentionsEvent(question string) bool {
	words := questionWords(strings.ToLower(question))
	for i, token := range words {
		switch foldVietnameseWord(token) {
		case "binding", "button", "callback", "click", "event", "handler", "nut", "bam":
			return true
		case "su":
			if i+1 < len(words) && foldVietnameseWord(words[i+1]) == "kien" {
				return true
			}
		}
	}
	return false
}
