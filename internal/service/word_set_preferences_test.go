package service

import (
	"testing"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestSelectEnabledReviewModeUsesEnabledFallback(t *testing.T) {
	word := domain.Word{Word: "negotiate", PartOfSpeech: "verb"}
	got := selectEnabledReviewMode(
		domain.ReviewModeBuildWord,
		[]domain.ReviewMode{domain.ReviewModeReveal, domain.ReviewModeMultipleChoice},
		&word,
	)
	if got != domain.ReviewModeMultipleChoice {
		t.Fatalf("expected enabled Mode 2 fallback, got %q", got)
	}
}

func TestSelectEnabledReviewModeSkipsBuildWordForPhrases(t *testing.T) {
	phrase := domain.Word{Word: "look after", PartOfSpeech: "phrasal verb"}
	got := selectEnabledReviewMode(
		domain.ReviewModeBuildWord,
		[]domain.ReviewMode{domain.ReviewModeReveal, domain.ReviewModeBuildWord, domain.ReviewModeFillBlank},
		&phrase,
	)
	if got != domain.ReviewModeFillBlank {
		t.Fatalf("expected compatible Mode 4 fallback, got %q", got)
	}
}

func TestNormalizeEnabledReviewModesRequiresModeOne(t *testing.T) {
	if _, err := normalizeEnabledReviewModes([]domain.ReviewMode{domain.ReviewModeMultipleChoice}); err == nil {
		t.Fatal("expected Mode 1 validation error")
	}
}
