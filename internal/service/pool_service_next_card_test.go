package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestFindNextCardInItemsPrefersDueCard(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	dueWordID := uuid.New()
	futureWeakWordID := uuid.New()
	futureDue := now.Add(20 * time.Minute)

	item, nextDue := findNextCardInItems([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   dueWordID,
			Ordinal:  1,
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			DueAt:    nil,
			Metadata: domain.JSONMap{"weakness_score": 0.2},
		},
		{
			ID:       uuid.New(),
			WordID:   futureWeakWordID,
			Ordinal:  2,
			ItemType: domain.PoolItemTypeWeak,
			Status:   domain.PoolItemStatusPending,
			DueAt:    &futureDue,
			Metadata: domain.JSONMap{"weakness_score": 2.5},
		},
	}, now)

	if item == nil {
		t.Fatalf("expected due card, got nil")
	}
	if item.WordID != dueWordID {
		t.Fatalf("expected due card %s, got %s", dueWordID, item.WordID)
	}
	if nextDue != nil {
		t.Fatalf("expected nil next due when a due card is available, got %v", *nextDue)
	}
}

func TestFindNextCardInItemsReturnsNextDueWhenNothingIsActionableYet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	reviewWordID := uuid.New()
	reviewDue := now.Add(5 * time.Minute)

	item, nextDue := findNextCardInItems([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   reviewWordID,
			Ordinal:  1,
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			DueAt:    &reviewDue,
			Metadata: domain.JSONMap{"weakness_score": 0.8},
		},
	}, now)

	if item != nil {
		t.Fatalf("expected no actionable card, got %s", item.WordID)
	}
	if nextDue == nil || !nextDue.Equal(reviewDue) {
		t.Fatalf("expected next due %v, got %#v", reviewDue, nextDue)
	}
}

func TestFindNextCardInItemsReturnsPendingBonusPracticeImmediately(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	bonusWordID := uuid.New()
	normalWordID := uuid.New()
	futureDue := now.Add(10 * time.Minute)

	item, nextDue := findNextCardInItems([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   normalWordID,
			Ordinal:  1,
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			DueAt:    &futureDue,
			Metadata: domain.JSONMap{"weakness_score": 1.0},
		},
		{
			ID:            uuid.New(),
			WordID:        bonusWordID,
			Ordinal:       2,
			ItemType:      domain.PoolItemTypeWeak,
			Status:        domain.PoolItemStatusPending,
			DueAt:         nil,
			BonusPractice: true,
			Metadata:      domain.JSONMap{"bonus_practice": true, "weakness_score": 2.0},
		},
	}, now)

	if item == nil {
		t.Fatalf("expected bonus practice card, got nil")
	}
	if item.WordID != bonusWordID {
		t.Fatalf("expected bonus practice word %s, got %s", bonusWordID, item.WordID)
	}
	if nextDue != nil {
		t.Fatalf("expected nil next due when bonus practice is actionable, got %v", *nextDue)
	}
}

func TestFindNextCardInItemsPrioritizesScheduledReviewOverWeakAndNew(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)
	scheduledWordID := uuid.New()
	weakWordID := uuid.New()
	newWordID := uuid.New()
	scheduledDue := now.Add(-15 * time.Minute)

	item, nextDue := findNextCardInItems([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   weakWordID,
			Ordinal:  1,
			ItemType: domain.PoolItemTypeWeak,
			Status:   domain.PoolItemStatusPending,
			DueAt:    nil,
			Metadata: domain.JSONMap{"bonus_practice": true},
		},
		{
			ID:                    uuid.New(),
			WordID:                newWordID,
			Ordinal:               2,
			ItemType:              domain.PoolItemTypeNew,
			Status:                domain.PoolItemStatusPending,
			FirstExposureRequired: true,
		},
		{
			ID:       uuid.New(),
			WordID:   scheduledWordID,
			Ordinal:  99,
			ItemType: domain.PoolItemTypeShortTerm,
			Status:   domain.PoolItemStatusPending,
			DueAt:    &scheduledDue,
		},
	}, now)

	if item == nil {
		t.Fatalf("expected scheduled short_term card, got nil")
	}
	if item.WordID != scheduledWordID {
		t.Fatalf("expected scheduled word %s, got %s", scheduledWordID, item.WordID)
	}
	if nextDue != nil {
		t.Fatalf("expected nil next due when an actionable scheduled card exists, got %v", *nextDue)
	}
}
