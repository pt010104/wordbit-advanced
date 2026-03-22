package service

import (
	"testing"
	"time"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestComputeNewWordQuota(t *testing.T) {
	t.Parallel()

	got := ComputeNewWordQuota(10, 6, 2, 3)
	if got != 10 {
		t.Fatalf("expected quota 10, got %d", got)
	}
}

func TestFirstExposureUnknownSchedulesTenMinutes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := ApplyFirstExposureUnknown(domain.UserWordState{}, now, 3200)
	if state.LearningStage != 1 {
		t.Fatalf("expected learning stage 1, got %d", state.LearningStage)
	}
	if state.NextReviewAt == nil || state.NextReviewAt.Sub(now) != 10*time.Minute {
		t.Fatalf("expected next review at +10m, got %#v", state.NextReviewAt)
	}
}

func TestReviewOutcomeMovesThroughConsolidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	state := domain.UserWordState{
		Status:        domain.WordStatusLearning,
		LearningStage: 1,
		Stability:     0.5,
		Difficulty:    0.5,
	}
	state = ApplyReviewOutcome(state, domain.RatingMedium, domain.ReviewModeReveal, now, 4100)
	if state.LearningStage != 2 {
		t.Fatalf("expected stage 2, got %d", state.LearningStage)
	}
	if state.NextReviewAt == nil || state.NextReviewAt.Sub(now) != 8*time.Hour {
		t.Fatalf("expected +8h review, got %#v", state.NextReviewAt)
	}

	state.LearningStage = 3
	state = ApplyReviewOutcome(state, domain.RatingEasy, domain.ReviewModeReveal, now, 3000)
	if state.LearningStage != 0 {
		t.Fatalf("expected standard review stage, got %d", state.LearningStage)
	}
	if state.Status != domain.WordStatusReview {
		t.Fatalf("expected review status, got %s", state.Status)
	}
	if state.NextReviewAt == nil || state.NextReviewAt.Sub(now) != 2*24*time.Hour {
		t.Fatalf("expected +2d review, got %#v", state.NextReviewAt)
	}
}

func TestApplyReviewOutcomeUsesShorterStandardIntervals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	state := domain.UserWordState{
		Status:          domain.WordStatusReview,
		IntervalSeconds: int((24 * time.Hour).Seconds()),
		Stability:       2.0,
		Difficulty:      0.55,
	}

	medium := ApplyReviewOutcome(state, domain.RatingMedium, domain.ReviewModeMultipleChoice, now, 2800)
	if medium.NextReviewAt == nil {
		t.Fatalf("expected medium review to schedule next review")
	}
	if got := medium.NextReviewAt.Sub(now); got <= 12*time.Hour || got >= 18*time.Hour {
		t.Fatalf("expected medium review interval between 12h and 18h, got %s", got)
	}

	easy := ApplyReviewOutcome(state, domain.RatingEasy, domain.ReviewModeFillBlank, now, 2200)
	if easy.NextReviewAt == nil {
		t.Fatalf("expected easy review to schedule next review")
	}
	if got := easy.NextReviewAt.Sub(now); got <= 24*time.Hour || got >= 30*time.Hour {
		t.Fatalf("expected easy review interval between 24h and 30h, got %s", got)
	}

	hard := ApplyReviewOutcome(state, domain.RatingHard, domain.ReviewModeReveal, now, 5200)
	if hard.NextReviewAt == nil {
		t.Fatalf("expected hard review to schedule next review")
	}
	if got := hard.NextReviewAt.Sub(now); got < 4*time.Hour || got >= 7*time.Hour {
		t.Fatalf("expected hard review interval between 4h and 7h, got %s", got)
	}
}

func TestSelectReviewMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		state                  domain.UserWordState
		memoryCauseBiasEnabled bool
		want                   domain.ReviewMode
	}{
		{
			name:                   "learning stage 1 stays reveal",
			state:                  domain.UserWordState{LearningStage: 1, Difficulty: 0.9, WeaknessScore: 2.5},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeReveal,
		},
		{
			name:                   "learning stage 2 stays reveal",
			state:                  domain.UserWordState{LearningStage: 2, Difficulty: 0.9, WeaknessScore: 2.5},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeReveal,
		},
		{
			name:                   "transition stage low weakness uses fill blank",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.3, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "transition stage previous threshold now stays fill blank",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.60, WeaknessScore: 1.05},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "transition stage higher difficulty uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.65, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "transition stage borderline mode 2 alternates back to reveal after multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.65, WeaknessScore: 0.2, LastMode: domain.ReviewModeMultipleChoice},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeReveal,
		},
		{
			name:                   "transition stage higher weakness uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.3, WeaknessScore: 1.20},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "transition stage harder previous answer uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.3, WeaknessScore: 0.2, LastRating: domain.RatingHard},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "transition stage mixed up cause uses multiple choice when enabled",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.3, WeaknessScore: 0.2, LastMemoryCause: domain.MemoryCauseMixedUpWord},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "transition stage ignores mixed up cause when bias disabled",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.3, WeaknessScore: 0.2, LastMemoryCause: domain.MemoryCauseMixedUpWord},
			memoryCauseBiasEnabled: false,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "standard review forgot meaning returns reveal",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, LastMemoryCause: domain.MemoryCauseForgotMeaning},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeReveal,
		},
		{
			name:                   "standard review mixed up cause returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, LastMemoryCause: domain.MemoryCauseMixedUpWord},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review spelling issue returns fill blank",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.8, LastMemoryCause: domain.MemoryCauseSpellingIssue},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "standard review previous difficulty threshold now stays fill blank",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.75, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "standard review high difficulty returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.80, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review previous weakness threshold now stays fill blank",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 1.6},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "standard review high weakness returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 1.8},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review borderline mode 2 alternates back to reveal after multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 1.8, LastMode: domain.ReviewModeMultipleChoice},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeReveal,
		},
		{
			name:                   "standard review wrong history returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, WrongCount: 2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review two meaning reveals now stay fill blank",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, RevealMeaningCount: 3},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "standard review meaning reveal history returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, RevealMeaningCount: 4},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review clean history returns fill blank",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeFillBlank,
		},
		{
			name:                   "standard review ignores mixed up cause when bias disabled",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, LastMemoryCause: domain.MemoryCauseMixedUpWord},
			memoryCauseBiasEnabled: false,
			want:                   domain.ReviewModeFillBlank,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if mode := SelectReviewMode(tc.state, tc.memoryCauseBiasEnabled); mode != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, mode)
			}
		})
	}
}

func TestApplyBonusPracticeOutcomeDoesNotMoveNextReviewAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	nextReview := now.Add(48 * time.Hour)
	state := domain.UserWordState{
		Status:        domain.WordStatusReview,
		NextReviewAt:  &nextReview,
		WeaknessScore: 2.0,
		Stability:     1.2,
		Difficulty:    0.7,
	}

	updated := ApplyBonusPracticeOutcome(state, domain.RatingEasy, domain.ReviewModeMultipleChoice, now, 2400)
	if updated.NextReviewAt == nil || !updated.NextReviewAt.Equal(nextReview) {
		t.Fatalf("expected next review to stay unchanged, got %#v", updated.NextReviewAt)
	}
	if updated.WeaknessScore >= state.WeaknessScore {
		t.Fatalf("expected bonus practice easy rating to reduce weakness, got %.2f from %.2f", updated.WeaknessScore, state.WeaknessScore)
	}
}

func TestApplyBonusPracticeOutcomeEasyDoesNotIncreaseStoredWeakness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	state := domain.UserWordState{
		Status:            domain.WordStatusReview,
		WeaknessScore:     0.3,
		WrongCount:        2,
		HintUsedCount:     1,
		Stability:         0.8,
		AvgResponseTimeMs: 8200,
	}

	updated := ApplyBonusPracticeOutcome(state, domain.RatingEasy, domain.ReviewModeMultipleChoice, now, 1800)
	if updated.WeaknessScore >= state.WeaknessScore {
		t.Fatalf("expected easy bonus practice to keep improving stored weakness, got %.2f from %.2f", updated.WeaknessScore, state.WeaknessScore)
	}
}

func TestApplyBonusPracticeOutcomeEasyKeepsReducingAcrossRepeats(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	state := domain.UserWordState{
		Status:        domain.WordStatusReview,
		WeaknessScore: 2.0,
		WrongCount:    2,
		Stability:     1.1,
		Difficulty:    0.7,
	}

	first := ApplyBonusPracticeOutcome(state, domain.RatingEasy, domain.ReviewModeMultipleChoice, now, 1800)
	second := ApplyBonusPracticeOutcome(first, domain.RatingEasy, domain.ReviewModeMultipleChoice, now.Add(time.Minute), 1800)

	if first.WeaknessScore >= state.WeaknessScore {
		t.Fatalf("expected first easy bonus practice to reduce weakness, got %.2f from %.2f", first.WeaknessScore, state.WeaknessScore)
	}
	if second.WeaknessScore >= first.WeaknessScore {
		t.Fatalf("expected repeated easy bonus practice to continue reducing weakness, got %.2f from %.2f", second.WeaknessScore, first.WeaknessScore)
	}
}
