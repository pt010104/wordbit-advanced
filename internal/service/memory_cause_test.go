package service

import (
	"testing"
	"time"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestInferMemoryCause(t *testing.T) {
	t.Parallel()

	target := domain.Word{
		Word:               "adapt",
		CanonicalForm:      "adapt",
		Lemma:              "adapt",
		ConfusableGroupKey: "adapt_adopt",
	}

	tests := []struct {
		name  string
		input MemoryCauseInput
		want  domain.MemoryCause
	}{
		{
			name: "forgot meaning after reveal",
			input: MemoryCauseInput{
				TargetWord:                  target,
				ModeUsed:                    domain.ReviewModeReveal,
				AnswerCorrect:               true,
				RevealedMeaningBeforeAnswer: true,
			},
			want: domain.MemoryCauseForgotMeaning,
		},
		{
			name: "mixed up word on confusable mcq miss",
			input: MemoryCauseInput{
				TargetWord:                       target,
				ModeUsed:                         domain.ReviewModeMultipleChoice,
				AnswerCorrect:                    false,
				SelectedChoiceConfusableGroupKey: "adapt_adopt",
			},
			want: domain.MemoryCauseMixedUpWord,
		},
		{
			name: "spelling issue on near miss fill blank",
			input: MemoryCauseInput{
				TargetWord:            target,
				ModeUsed:              domain.ReviewModeFillBlank,
				AnswerCorrect:         false,
				NormalizedTypedAnswer: "adpat",
			},
			want: domain.MemoryCauseSpellingIssue,
		},
		{
			name: "spelling issue after wrong fill then mcq success",
			input: MemoryCauseInput{
				TargetWord:    target,
				ModeUsed:      domain.ReviewModeMultipleChoice,
				AnswerCorrect: true,
				State: domain.UserWordState{
					LastMode:          domain.ReviewModeFillBlank,
					LastAnswerCorrect: boolPointer(false),
				},
			},
			want: domain.MemoryCauseSpellingIssue,
		},
		{
			name: "slow recall on correct delayed answer",
			input: MemoryCauseInput{
				TargetWord:     target,
				ModeUsed:       domain.ReviewModeFillBlank,
				AnswerCorrect:  true,
				ResponseTimeMs: 9500,
			},
			want: domain.MemoryCauseSlowRecall,
		},
		{
			name: "guessed correct on fast difficult mcq",
			input: MemoryCauseInput{
				TargetWord:     target,
				ModeUsed:       domain.ReviewModeMultipleChoice,
				AnswerCorrect:  true,
				ResponseTimeMs: 1800,
				State: domain.UserWordState{
					Difficulty:    0.8,
					WeaknessScore: 1.6,
				},
			},
			want: domain.MemoryCauseGuessedCorrect,
		},
		{
			name: "no cause when nothing stands out",
			input: MemoryCauseInput{
				TargetWord:     target,
				ModeUsed:       domain.ReviewModeMultipleChoice,
				AnswerCorrect:  true,
				ResponseTimeMs: 3200,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := InferMemoryCause(tt.input); got != tt.want {
				t.Fatalf("InferMemoryCause() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyMemoryCauseIntervalBias(t *testing.T) {
	t.Parallel()

	now := testTime(2026, 3, 21, 10, 0)
	next := now.Add(24 * time.Hour)
	state := domain.UserWordState{
		Status:          domain.WordStatusReview,
		IntervalSeconds: int((24 * 60 * 60)),
		NextReviewAt:    &next,
	}

	slow := ApplyMemoryCauseIntervalBias(state, domain.MemoryCauseSlowRecall, now)
	if slow.IntervalSeconds != int(float64(24*60*60)*0.85) {
		t.Fatalf("expected slow recall interval bias, got %d", slow.IntervalSeconds)
	}

	guess := ApplyMemoryCauseIntervalBias(state, domain.MemoryCauseGuessedCorrect, now)
	if guess.IntervalSeconds != int(float64(24*60*60)*0.90) {
		t.Fatalf("expected guessed correct interval bias, got %d", guess.IntervalSeconds)
	}
}

func testTime(year int, month int, day int, hour int, minute int) time.Time {
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
}
