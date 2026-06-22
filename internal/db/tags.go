package db

import (
	"regexp"
	"strings"
)

var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "in": true, "on": true,
	"is": true, "it": true, "at": true, "to": true, "for": true, "and": true,
	"with": true, "by": true, "from": true, "or": true, "as": true, "but": true,
	"be": true, "was": true, "are": true, "this": true, "that": true, "not": true,
	"no": true, "has": true, "had": true, "will": true, "can": true, "all": true,
	"his": true, "her": true, "its": true, "who": true, "one": true, "two": true,
	"been": true, "more": true, "some": true, "any": true, "my": true,
	"your": true, "our": true, "their": true, "new": true, "just": true, "than": true,
	"if": true, "so": true, "up": true, "out": true, "when": true, "what": true,
	"which": true, "how": true, "do": true, "does": true, "did": true,
	"i": true, "you": true, "he": true, "she": true, "we": true, "they": true,
	"me": true, "him": true, "us": true, "them": true,
	"also": true, "only": true, "very": true, "then": true, "now": true,
	"into": true, "over": true, "after": true, "before": true, "between": true,
	"about": true, "other": true, "each": true, "these": true, "those": true,
}

var delimRe = regexp.MustCompile(`[_\-\s.,;:!?()\[\]{}"'/\\|&]+`)

func extractWords(text string) []string {
	if text == "" {
		return nil
	}
	tokens := delimRe.Split(strings.ToLower(text), -1)
	var words []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if len(t) <= 2 || stopwords[t] {
			continue
		}
		words = append(words, t)
	}
	return words
}

func dedupe(seen map[string]bool) []string {
	result := make([]string, 0, len(seen))
	for word := range seen {
		result = append(result, word)
	}
	return result
}

func GenerateTags(identifier, title, creator string, subjects, genres []string) string {
	seen := make(map[string]bool)

	for _, w := range extractWords(identifier) {
		seen[w] = true
	}
	for _, w := range extractWords(title) {
		seen[w] = true
	}
	for _, w := range extractWords(creator) {
		seen[w] = true
	}
	for _, subject := range subjects {
		for _, w := range extractWords(subject) {
			seen[w] = true
		}
	}
	for _, genre := range genres {
		for _, w := range extractWords(genre) {
			seen[w] = true
		}
	}

	return strings.Join(dedupe(seen), ", ")
}
