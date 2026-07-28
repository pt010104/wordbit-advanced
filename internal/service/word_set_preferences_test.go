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

func TestSelectConfiguredReviewModeAlternatesDefinitionFirstWithHiddenMeaning(t *testing.T) {
	word := domain.Word{Word: "negotiate", PartOfSpeech: "verb"}
	enabled := []domain.ReviewMode{domain.ReviewModeReveal, domain.ReviewModeDefinitionFirst}

	first := selectConfiguredReviewMode(
		domain.UserWordState{LearningStage: 1, ReviewCount: 1},
		false,
		enabled,
		&word,
	)
	if first != domain.ReviewModeDefinitionFirst {
		t.Fatalf("expected Mode 6 when Mode 1 is selected first, got %q", first)
	}

	second := selectConfiguredReviewMode(
		domain.UserWordState{LearningStage: 1, ReviewCount: 2, LastMode: domain.ReviewModeDefinitionFirst},
		false,
		enabled,
		&word,
	)
	if second != domain.ReviewModeReveal {
		t.Fatalf("expected Mode 1 after Mode 6, got %q", second)
	}
}
