package search

import (
	"slices"
	"testing"
)

func TestQuestionHintsSeparateScreenAndAction(t *testing.T) {
	question := "On the Order Review screen, what happens when I click Continue?"
	terms, titles := QuestionTerms(question)
	if !slices.Contains(titles, "orderreview") ||
		!slices.ContainsFunc(terms, func(term QuestionTerm) bool { return term.Text == "next" }) ||
		slices.ContainsFunc(terms, func(term QuestionTerm) bool { return term.Text == "order" || term.Text == "review" }) ||
		!QuestionMentionsEvent(question) {
		t.Fatalf("unexpected hints: terms=%v titles=%v", terms, titles)
	}
}

func TestQuestionRouteUsesLiteralPath(t *testing.T) {
	if got := QuestionRoute("What renders `/shop/items/:id`?"); got != "/shop/items/:id" {
		t.Fatalf("got %q", got)
	}
	if got := QuestionRoute("Trace https://example.invalid/other."); got != "" {
		t.Fatalf("took part of an external URL as an application route: %q", got)
	}
}

func TestVietnameseQuestionKeepsScreenNameAndFindsNavigation(t *testing.T) {
	question := "Trong màn hình Order Review, bấm Tiếp tục để qua bước sau và quay lại thế nào?"
	terms, titles := QuestionTerms(question)
	for _, want := range []string{"next", "step", "back"} {
		if !slices.ContainsFunc(terms, func(term QuestionTerm) bool { return term.Text == want }) {
			t.Errorf("missing term %q: %v", want, terms)
		}
	}
	if !slices.Contains(titles, "orderreview") || !QuestionMentionsEvent(question) {
		t.Fatalf("lost screen or event cues: titles=%v", titles)
	}
}

func TestVietnameseQuestionAcceptsUnaccentedAndDecomposedWords(t *testing.T) {
	for _, question := range []string{
		"Nut luu ban nhap ghi vao bo nho cuc bo o dau?",
		"Nu\u0301t lu\u031bu ba\u0309n nha\u0301p ghi va\u0300o bo\u0323\u0302 nho\u031b\u0301 cu\u0323c bo\u0323\u0302 o\u031b\u0309 đa\u0302u?",
	} {
		terms, _ := QuestionTerms(question)
		for _, want := range []string{"save", "draft", "storage"} {
			if !slices.ContainsFunc(terms, func(term QuestionTerm) bool { return term.Text == want }) {
				t.Errorf("missing term %q for %q: %v", want, question, terms)
			}
		}
		if !QuestionMentionsEvent(question) {
			t.Errorf("lost the button cue for %q", question)
		}
	}
}

func TestVietnameseQuestionPreservesQuotedRoute(t *testing.T) {
	if got := QuestionRoute("Trang nào render “/Mục/Chi-tiết”?"); got != "/Mục/Chi-tiết" {
		t.Fatalf("got %q", got)
	}
}
