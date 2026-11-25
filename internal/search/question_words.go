package search

import (
	"strings"
	"unicode"
)

func questionWords(question string) []string {
	return strings.FieldsFunc(question, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.Is(unicode.Mn, r) && r != '_'
	})
}

// Accent folding is used only for matching the phrase table. Never use it
// for route identity or to replace a spelling in a source citation.
func foldVietnameseWord(word string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		switch {
		case r <= unicode.MaxASCII:
			return r
		case unicode.Is(unicode.Mn, r):
			return -1
		case strings.ContainsRune("àáảãạăằắẳẵặâầấẩẫậ", r):
			return 'a'
		case r == 'đ':
			return 'd'
		case strings.ContainsRune("èéẻẽẹêềếểễệ", r):
			return 'e'
		case strings.ContainsRune("ìíỉĩị", r):
			return 'i'
		case strings.ContainsRune("òóỏõọôồốổỗộơờớởỡợ", r):
			return 'o'
		case strings.ContainsRune("ùúủũụưừứửữự", r):
			return 'u'
		case strings.ContainsRune("ỳýỷỹỵ", r):
			return 'y'
		default:
			return r
		}
	}, word)
}
