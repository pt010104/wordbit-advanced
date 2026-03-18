package service

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var whitespacePattern = regexp.MustCompile(`\s+`)

func NormalizeWord(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	decomposed := norm.NFKD.String(value)
	var builder strings.Builder
	builder.Grow(len(decomposed))
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		case unicode.IsSpace(r):
			builder.WriteRune(' ')
		}
	}

	cleaned := whitespacePattern.ReplaceAllString(builder.String(), " ")
	return strings.TrimSpace(cleaned)
}

var defaultConfusableClusters = map[string]string{
	"affect":     "affect_effect",
	"effect":     "affect_effect",
	"economic":   "economy_family",
	"economical": "economy_family",
	"economy":    "economy_family",
	"adapt":      "adapt_adopt",
	"adopt":      "adapt_adopt",
}

func ConfusableGroupFor(word string, canonical string, lemma string) string {
	for _, candidate := range []string{NormalizeWord(word), NormalizeWord(canonical), NormalizeWord(lemma)} {
		if group, ok := defaultConfusableClusters[candidate]; ok {
			return group
		}
	}
	return ""
}
