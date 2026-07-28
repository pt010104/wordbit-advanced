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

func TestSelectConfiguredReviewModeUsesNormalDefinitionFirstFallback(t *testing.T) {
	word := domain.Word{Word: "negotiate", PartOfSpeech: "verb"}
	enabled := []domain.ReviewMode{domain.ReviewModeReveal, domain.ReviewModeDefinitionFirst}

	first := selectConfiguredReviewMode(
		domain.UserWordState{LearningStage: 1, ReviewCount: 1},
		false,
		enabled,
		&word,
	)
	if first != domain.ReviewModeReveal {
		t.Fatalf("expected normal Mode 1 selection, got %q", first)
	}

}

func TestCustomSetMovesAwayFromHardestModeAfterThreeConsecutiveCards(t *testing.T) {
	word := domain.Word{Word: "resilient", PartOfSpeech: "adjective"}
	got := selectConfiguredReviewModeForPreferences(
		domain.UserWordState{
			LearningStage:   3,
			LastMode:        domain.ReviewModeDefinitionFirst,
			ModeStreakCount: 3,
		},
		false,
		ReviewModePreferences{
			EnabledModes: []domain.ReviewMode{
				domain.ReviewModeReveal,
				domain.ReviewModeMultipleChoice,
				domain.ReviewModeDefinitionFirst,
			},
			IsCustomSet: true,
		},
		&word,
	)
	if got != domain.ReviewModeMultipleChoice {
		t.Fatalf("expected Mode 2 after three Mode 6 cards, got %q", got)
	}
}

func TestCustomSetNeverRotatesHardestModeToModeOne(t *testing.T) {
	word := domain.Word{Word: "resilient", PartOfSpeech: "adjective"}
	got := selectConfiguredReviewModeForPreferences(
		domain.UserWordState{
			LearningStage:   3,
			LastMode:        domain.ReviewModeBuildWord,
			ModeStreakCount: 3,
		},
		false,
		ReviewModePreferences{
			EnabledModes: []domain.ReviewMode{
				domain.ReviewModeReveal,
				domain.ReviewModeMultipleChoice,
				domain.ReviewModeBuildWord,
			},
			IsCustomSet: true,
		},
		&word,
	)
	if got != domain.ReviewModeMultipleChoice {
		t.Fatalf("expected Mode 2 rather than Mode 1 after three Mode 3 cards, got %q", got)
	}
}
