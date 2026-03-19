package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type captureEventRepo struct {
	events []domain.LearningEvent
}

func (r *captureEventRepo) Insert(ctx context.Context, event domain.LearningEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (r *captureEventRepo) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}

func TestSubmitReviewBonusPracticeDoesNotChangeNextReviewAt(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wordID := uuid.New()
	itemID := uuid.New()
	now := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	nextReview := now.Add(48 * time.Hour)

	stateRepo := &replenishStateRepo{
		states: map[uuid.UUID]domain.UserWordState{
			wordID: {
				UserID:        userID,
				WordID:        wordID,
				Status:        domain.WordStatusReview,
				NextReviewAt:  &nextReview,
				WeaknessScore: 2.0,
				Stability:     1.1,
				Difficulty:    0.7,
			},
		},
	}
	poolRepo := &replenishPoolRepo{
		items: []domain.DailyLearningPoolItem{
			{
				ID:            itemID,
				UserID:        userID,
				WordID:        wordID,
				ItemType:      domain.PoolItemTypeWeak,
				ReviewMode:    domain.ReviewModeMultipleChoice,
				Status:        domain.PoolItemStatusPending,
				IsReview:      true,
				BonusPractice: true,
				Metadata: domain.JSONMap{
					"bonus_practice": true,
					"weakness_score": 2.0,
				},
			},
		},
	}
	eventRepo := &captureEventRepo{}
	service := NewLearningService(
		&replenishSettingsRepo{settings: domain.DefaultUserSettings(userID)},
		stateRepo,
		poolRepo,
		eventRepo,
		replenishClock{now: now},
		nil,
	)

	err := service.SubmitReview(context.Background(), domain.User{ID: userID}, ReviewRequest{
		PoolItemID:     itemID,
		Rating:         domain.RatingEasy,
		ModeUsed:       domain.ReviewModeMultipleChoice,
		ResponseTimeMs: 1800,
		ClientEventID:  "bonus-practice-1",
	})
	if err != nil {
		t.Fatalf("SubmitReview returned error: %v", err)
	}

	updated := stateRepo.states[wordID]
	if updated.NextReviewAt == nil || !updated.NextReviewAt.Equal(nextReview) {
		t.Fatalf("expected next review to remain unchanged, got %#v", updated.NextReviewAt)
	}
	if updated.WeaknessScore >= 2.0 {
		t.Fatalf("expected weakness score to improve after bonus practice, got %.2f", updated.WeaknessScore)
	}
	if len(eventRepo.events) != 1 {
		t.Fatalf("expected one event, got %d", len(eventRepo.events))
	}
	if eventRepo.events[0].EventType != domain.EventTypeBonusPractice {
		t.Fatalf("expected bonus practice event, got %s", eventRepo.events[0].EventType)
	}
	if eventRepo.events[0].Payload["bonus_practice"] != true {
		t.Fatalf("expected bonus_practice payload flag, got %#v", eventRepo.events[0].Payload["bonus_practice"])
	}
}
