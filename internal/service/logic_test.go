package service

import (
	"testing"
	"time"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestComputeNewWordQuota(t *testing.T) {
	t.Parallel()

	got := ComputeNewWordQuota(10, 6, 2, 3)
	if got != 5 {
		t.Fatalf("expected quota 5, got %d", got)
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
	if state.NextReviewAt == nil || state.NextReviewAt.Sub(now) != 24*time.Hour {
		t.Fatalf("expected +1d review, got %#v", state.NextReviewAt)
	}

	state.LearningStage = 3
	state = ApplyReviewOutcome(state, domain.RatingEasy, domain.ReviewModeReveal, now, 3000)
	if state.LearningStage != 0 {
		t.Fatalf("expected standard review stage, got %d", state.LearningStage)
	}
	if state.Status != domain.WordStatusReview {
		t.Fatalf("expected review status, got %s", state.Status)
	}
}

func TestSelectReviewMode(t *testing.T) {
	t.Parallel()

	if mode := SelectReviewMode(domain.UserWordState{LearningStage: 1, Stability: 0.7}); mode != domain.ReviewModeReveal {
		t.Fatalf("expected reveal mode, got %s", mode)
	}
	if mode := SelectReviewMode(domain.UserWordState{LearningStage: 0, Stability: 2.0, Difficulty: 0.8}); mode != domain.ReviewModeMultipleChoice {
		t.Fatalf("expected multiple choice mode, got %s", mode)
	}
	if mode := SelectReviewMode(domain.UserWordState{LearningStage: 0, Stability: 3.0, Difficulty: 0.3}); mode != domain.ReviewModeFillBlank {
		t.Fatalf("expected fill-in-blank mode, got %s", mode)
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
