package service

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestComputeNewWordQuota(t *testing.T) {
	t.Parallel()

	got := ComputeNewWordQuota(10, 6, 2, 3)
	if got != 20 {
		t.Fatalf("expected buffered quota 20, got %d", got)
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

func TestApplyReviewOutcomeRebalancesStandardIntervals(t *testing.T) {
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
	if got := medium.NextReviewAt.Sub(now); got <= 36*time.Hour || got >= 38*time.Hour {
		t.Fatalf("expected medium review interval between 36h and 38h, got %s", got)
	}

	easy := ApplyReviewOutcome(state, domain.RatingEasy, domain.ReviewModeFillBlank, now, 2200)
	if easy.NextReviewAt == nil {
		t.Fatalf("expected easy review to schedule next review")
	}
	if got := easy.NextReviewAt.Sub(now); got <= 59*time.Hour || got >= 61*time.Hour {
		t.Fatalf("expected easy review interval between 59h and 61h, got %s", got)
	}

	hard := ApplyReviewOutcome(state, domain.RatingHard, domain.ReviewModeReveal, now, 5200)
	if hard.NextReviewAt == nil {
		t.Fatalf("expected hard review to schedule next review")
	}
	if got := hard.NextReviewAt.Sub(now); got <= 9*time.Hour || got >= 10*time.Hour {
		t.Fatalf("expected hard review interval between 9h and 10h, got %s", got)
	}
}

func TestApplyReviewOutcomeEasyReducesWeaknessAcrossRepeats(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	state := domain.UserWordState{
		Status:          domain.WordStatusReview,
		LastRating:      domain.RatingHard,
		IntervalSeconds: int((24 * time.Hour).Seconds()),
		Stability:       2.2,
		Difficulty:      0.62,
		ReviewCount:     9,
		WrongCount:      4,
		HintUsedCount:   3,
		EasyCount:       1,
		MediumCount:     2,
		HardCount:       4,
	}
	state.WeaknessScore = computeWeaknessScoreFromRatings(state)

	first := ApplyReviewOutcome(state, domain.RatingEasy, domain.ReviewModeMultipleChoice, now, 2100)
	second := ApplyReviewOutcome(first, domain.RatingEasy, domain.ReviewModeFillBlank, now.Add(24*time.Hour), 1900)

	if first.WeaknessScore >= state.WeaknessScore {
		t.Fatalf("expected first easy review to reduce weakness, got %.2f from %.2f", first.WeaknessScore, state.WeaknessScore)
	}
	if second.WeaknessScore >= first.WeaknessScore {
		t.Fatalf("expected repeated easy review to keep reducing weakness, got %.2f from %.2f", second.WeaknessScore, first.WeaknessScore)
	}
}

func TestComputeWeaknessScoreIgnoresRevealAndTimingSignals(t *testing.T) {
	t.Parallel()

	base := domain.UserWordState{
		LastRating:    domain.RatingMedium,
		EasyCount:     2,
		MediumCount:   3,
		HardCount:     1,
		WrongCount:    0,
		HintUsedCount: 0,
	}
	behaviorHeavy := base
	behaviorHeavy.WrongCount = 99
	behaviorHeavy.HintUsedCount = 99
	behaviorHeavy.RevealMeaningCount = 99
	behaviorHeavy.RevealExampleCount = 99
	behaviorHeavy.AvgResponseTimeMs = 60000
	behaviorHeavy.Stability = 0.1

	if got, want := computeWeaknessScoreFromRatings(behaviorHeavy), computeWeaknessScoreFromRatings(base); got != want {
		t.Fatalf("expected reveal/timing signals to be ignored, got %.2f want %.2f", got, want)
	}
}

func TestComputeWeaknessScoreIncludesConstructionStruggle(t *testing.T) {
	t.Parallel()

	base := domain.UserWordState{
		LastRating:  domain.RatingEasy,
		EasyCount:   3,
		MediumCount: 1,
	}
	struggled := base
	struggled.WordConstructionStruggleCount = 3

	if got, wantMinimum := computeWeaknessScoreFromRatings(struggled), computeWeaknessScoreFromRatings(base); got <= wantMinimum {
		t.Fatalf("expected construction struggle to increase weakness, got %.2f from %.2f", got, wantMinimum)
	}
}

func TestComputeWeaknessScoreEasyRecoverySoftensRepeatedEasyReviews(t *testing.T) {
	t.Parallel()

	state := domain.UserWordState{
		LastRating:  domain.RatingMedium,
		EasyCount:   7,
		MediumCount: 7,
		HardCount:   14,
	}

	if got := computeWeaknessScoreFromRatings(state); math.Abs(got-13.4) > 1e-9 {
		t.Fatalf("expected repeated easy reviews to soften weakness to 13.40, got %.2f", got)
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
			name:                   "transition stage after one build word stays build word",
			state:                  domain.UserWordState{WordID: uuid.MustParse("02000000-0000-0000-0000-000000000000"), LearningStage: 3, Difficulty: 0.3, WeaknessScore: 0.2, LastMode: domain.ReviewModeBuildWord, LastRating: domain.RatingEasy},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeBuildWord,
		},
		{
			name:                   "transition stage threshold now uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.60, WeaknessScore: 1.05},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "transition stage higher difficulty uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.70, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "transition stage borderline mode 2 alternates back to reveal after multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.70, WeaknessScore: 0.2, LastMode: domain.ReviewModeMultipleChoice},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeReveal,
		},
		{
			name:                   "transition stage higher weakness uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 3, Difficulty: 0.3, WeaknessScore: 1.10},
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
			state:                  domain.UserWordState{WordID: uuid.MustParse("01000000-0000-0000-0000-000000000000"), LearningStage: 3, Difficulty: 0.3, WeaknessScore: 0.2, LastMemoryCause: domain.MemoryCauseMixedUpWord, LastMode: domain.ReviewModeBuildWord, LastRating: domain.RatingEasy},
			memoryCauseBiasEnabled: false,
			want:                   domain.ReviewModeBuildWord,
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
			name:                   "standard review spelling issue returns build word",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.8, LastMemoryCause: domain.MemoryCauseSpellingIssue},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeBuildWord,
		},
		{
			name:                   "standard review difficulty threshold uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.78, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review high difficulty returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.86, WeaknessScore: 0.2},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review weakness threshold uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 1.75},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review high weakness returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 2.1},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review borderline mode 2 alternates back to reveal after multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 2.1, LastMode: domain.ReviewModeMultipleChoice},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeReveal,
		},
		{
			name:                   "standard review wrong history returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, WrongCount: 3},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review meaning reveal threshold uses multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, RevealMeaningCount: 4},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review meaning reveal history returns multiple choice",
			state:                  domain.UserWordState{LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, RevealMeaningCount: 5},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeMultipleChoice,
		},
		{
			name:                   "standard review after one build word stays build word",
			state:                  domain.UserWordState{WordID: uuid.MustParse("02000000-0000-0000-0000-000000000000"), LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, LastMode: domain.ReviewModeBuildWord, LastRating: domain.RatingEasy},
			memoryCauseBiasEnabled: true,
			want:                   domain.ReviewModeBuildWord,
		},
		{
			name:                   "standard review ignores mixed up cause when bias disabled",
			state:                  domain.UserWordState{WordID: uuid.MustParse("02000000-0000-0000-0000-000000000000"), LearningStage: 0, Difficulty: 0.3, WeaknessScore: 0.2, LastMemoryCause: domain.MemoryCauseMixedUpWord, LastMode: domain.ReviewModeBuildWord, LastRating: domain.RatingEasy},
			memoryCauseBiasEnabled: false,
			want:                   domain.ReviewModeBuildWord,
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

func TestSelectReviewModeForcesBuildWordAfterProlongedMode12Struggle(t *testing.T) {
	t.Parallel()

	firstSeen := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	lastSeen := firstSeen.Add(72 * time.Hour)
	state := domain.UserWordState{
		LearningStage:        0,
		Difficulty:           0.88,
		WeaknessScore:        2.2,
		LastMode:             domain.ReviewModeMultipleChoice,
		LastRating:           domain.RatingHard,
		ReviewCount:          6,
		HardCount:            3,
		WrongCount:           3,
		RevealMeaningCount:   4,
		MeaningForgetCount:   2,
		ConfusableMixupCount: 1,
		FirstSeenAt:          &firstSeen,
		LastSeenAt:           &lastSeen,
	}

	if mode := SelectReviewMode(state, true); mode != domain.ReviewModeBuildWord {
		t.Fatalf("expected prolonged hard mode1/2 word to force build_word, got %s", mode)
	}
}

func TestSelectReviewModePromotesStuckHiddenMeaningToMultipleChoice(t *testing.T) {
	t.Parallel()

	state := domain.UserWordState{
		LearningStage: 0,
		Difficulty:    0.30,
		WeaknessScore: 0.20,
		LastMode:      domain.ReviewModeReveal,
		LastRating:    domain.RatingHard,
		ReviewCount:   3,
		HardCount:     2,
		WrongCount:    2,
	}

	if mode := SelectReviewMode(state, true); mode != domain.ReviewModeMultipleChoice {
		t.Fatalf("expected stuck hidden-meaning word to move to multiple_choice, got %s", mode)
	}
}

func TestSelectReviewModeKeepsBuildWordBeforeAnyCleanConstructionTurn(t *testing.T) {
	t.Parallel()

	state := domain.UserWordState{
		WordID:                        uuid.MustParse("02000000-0000-0000-0000-000000000000"),
		LearningStage:                 0,
		Difficulty:                    0.30,
		WeaknessScore:                 0.20,
		LastMode:                      domain.ReviewModeBuildWord,
		LastRating:                    domain.RatingEasy,
		WordConstructionSuccessStreak: 0,
	}

	if mode := SelectReviewMode(state, true); mode != domain.ReviewModeBuildWord {
		t.Fatalf("expected no clean build_word turn yet to stay in build_word, got %s", mode)
	}
}

func TestSelectReviewModePromotesCleanBuildWordTurnToAdvancedConstruction(t *testing.T) {
	t.Parallel()

	state := domain.UserWordState{
		WordID:                        uuid.MustParse("02000000-0000-0000-0000-000000000000"),
		LearningStage:                 0,
		Difficulty:                    0.30,
		WeaknessScore:                 0.20,
		LastMode:                      domain.ReviewModeBuildWord,
		LastRating:                    domain.RatingEasy,
		WordConstructionSuccessStreak: advancedConstructionSuccessStreak,
	}

	mode := SelectReviewMode(state, true)
	if mode != domain.ReviewModeFillBlank && mode != domain.ReviewModeListening {
		t.Fatalf("expected a clean build_word turn to promote to mode 4/5, got %s", mode)
	}
}

func TestSelectReviewModeDoesNotPromoteHardBuildWordTurnToFillBlank(t *testing.T) {
	t.Parallel()

	answerCorrect := false
	state := domain.UserWordState{
		WordID:            uuid.MustParse("02000000-0000-0000-0000-000000000000"),
		LearningStage:     0,
		Difficulty:        0.30,
		WeaknessScore:     0.20,
		LastMode:          domain.ReviewModeBuildWord,
		LastRating:        domain.RatingHard,
		LastAnswerCorrect: &answerCorrect,
	}

	if mode := SelectReviewMode(state, true); mode != domain.ReviewModeBuildWord {
		t.Fatalf("expected hard build_word turn to stay in build_word, got %s", mode)
	}
}

func TestSelectReviewModeTransitionFromMode12EntersBuildWord(t *testing.T) {
	t.Parallel()

	state := domain.UserWordState{
		LearningStage: 0,
		LastMode:      domain.ReviewModeMultipleChoice,
		LastRating:    domain.RatingEasy,
		Difficulty:    0.30,
		WeaknessScore: 0.20,
	}

	if mode := SelectReviewMode(state, true); mode != domain.ReviewModeBuildWord {
		t.Fatalf("expected normal mode1/2 transition to enter build_word first, got %s", mode)
	}
}

func TestSelectReviewModeKeepsBuildWordUntilConstructionMastery(t *testing.T) {
	t.Parallel()

	state := domain.UserWordState{
		WordID:        uuid.MustParse("01000000-0000-0000-0000-000000000000"),
		LearningStage: 0,
		LastMode:      domain.ReviewModeBuildWord,
		LastRating:    domain.RatingEasy,
		Difficulty:    0.18,
		WeaknessScore: 0.20,
	}

	if mode := SelectReviewMode(state, true); mode != domain.ReviewModeBuildWord {
		t.Fatalf("expected word-construction follow-up to stay in build_word before mastery, got %s", mode)
	}
}

func TestApplyWordConstructionFeedbackTracksCleanSuccessStreak(t *testing.T) {
	t.Parallel()

	state := domain.UserWordState{
		LastRating: domain.RatingMedium,
		LastMode:   domain.ReviewModeBuildWord,
	}

	updated := ApplyWordConstructionFeedback(state, domain.ReviewModeBuildWord, true, 0, 3, 0, 2400, time.Now())
	if updated.WordConstructionSuccessStreak != 1 {
		t.Fatalf("expected clean success streak 1, got %d", updated.WordConstructionSuccessStreak)
	}
	if updated.WordConstructionStruggleCount != 0 {
		t.Fatalf("expected no construction struggle, got %d", updated.WordConstructionStruggleCount)
	}
}

func TestApplyWordConstructionFeedbackShortensEasyReviewAfterRetries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	nextReview := now.Add(5 * 24 * time.Hour)
	state := domain.UserWordState{
		Status:          domain.WordStatusReview,
		LastRating:      domain.RatingEasy,
		LastMode:        domain.ReviewModeBuildWord,
		NextReviewAt:    &nextReview,
		IntervalSeconds: int((5 * 24 * time.Hour).Seconds()),
		Stability:       2.0,
		Difficulty:      0.30,
		WeaknessScore:   0.20,
	}

	updated := ApplyWordConstructionFeedback(state, domain.ReviewModeBuildWord, true, 3, 3, 1, 4200, now)
	if updated.WordConstructionSuccessStreak != 0 {
		t.Fatalf("expected streak reset after retry/hints, got %d", updated.WordConstructionSuccessStreak)
	}
	if updated.WordConstructionStruggleCount != 1 {
		t.Fatalf("expected construction struggle count 1, got %d", updated.WordConstructionStruggleCount)
	}
	if updated.NextReviewAt == nil || !updated.NextReviewAt.Before(nextReview) {
		t.Fatalf("expected next review to be pulled earlier, got %#v from %#v", updated.NextReviewAt, nextReview)
	}
	if updated.IntervalSeconds >= state.IntervalSeconds {
		t.Fatalf("expected interval to shrink from %d, got %d", state.IntervalSeconds, updated.IntervalSeconds)
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
