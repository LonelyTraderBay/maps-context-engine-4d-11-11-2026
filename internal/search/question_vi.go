package search

import "strings"

// These phrases add likely identifier terms. The original words are still
// available for matching literal names, screen titles and paths.
var vietnameseQuestionTerms = map[string]QuestionTerm{
	"tiep tuc":      {Text: "next", Weight: 22},
	"tiep theo":     {Text: "next", Weight: 22},
	"quay lai":      {Text: "back", Weight: 12},
	"tro lai":       {Text: "back", Weight: 12},
	"buoc":          {Text: "step", Weight: 10},
	"khoi tao":      {Text: "init", Weight: 10},
	"ban nhap":      {Text: "draft", Weight: 10},
	"luu":           {Text: "save", Weight: 14},
	"luu tru":       {Text: "storage", Weight: 24},
	"bo nho cuc bo": {Text: "storage", Weight: 24},
	"xuat mau":      {Text: "export", Weight: 14},
	"xuat":          {Text: "export", Weight: 14},
	"tai xuong":     {Text: "download", Weight: 10},
	"loai du lieu":  {Text: "type", Weight: 10},
	"cap nhat":      {Text: "update", Weight: 10},
	"tim kiem":      {Text: "search", Weight: 10},
	"dang nhap":     {Text: "login", Weight: 10},
	"xoa":           {Text: "delete", Weight: 10},
}

func addVietnameseQuestionTerms(words []string, add func(string, int)) {
	folded := make([]string, len(words))
	for i, word := range words {
		folded[i] = foldVietnameseWord(word)
	}
	for i := 0; i < len(folded); i++ {
		for count := min(4, len(folded)-i); count > 0; count-- {
			if term, ok := vietnameseQuestionTerms[strings.Join(folded[i:i+count], " ")]; ok {
				add(term.Text, term.Weight)
				i += count - 1
				break
			}
		}
	}
}
