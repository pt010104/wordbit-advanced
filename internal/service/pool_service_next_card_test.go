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

func TestFindNextCardInItemsFallsBackToHighestWeaknessWhenNothingIsDue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	reviewWordID := uuid.New()
	weakWordID := uuid.New()
	reviewDue := now.Add(5 * time.Minute)
	weakDue := now.Add(25 * time.Minute)

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
		{
			ID:       uuid.New(),
			WordID:   weakWordID,
			Ordinal:  2,
			ItemType: domain.PoolItemTypeWeak,
			Status:   domain.PoolItemStatusPending,
			DueAt:    &weakDue,
			Metadata: domain.JSONMap{"weakness_score": 2.1},
		},
	}, now)

	if item == nil {
		t.Fatalf("expected fallback card, got nil")
	}
	if item.WordID != weakWordID {
		t.Fatalf("expected highest-weakness fallback %s, got %s", weakWordID, item.WordID)
	}
	if nextDue != nil {
		t.Fatalf("expected nil next due when a fallback card is returned, got %v", *nextDue)
	}
}

func TestFindNextCardInItemsUsesSoonestDueAsTieBreaker(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	firstWordID := uuid.New()
	secondWordID := uuid.New()
	laterDue := now.Add(40 * time.Minute)
	soonerDue := now.Add(10 * time.Minute)

	item, _ := findNextCardInItems([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   firstWordID,
			Ordinal:  1,
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			DueAt:    &laterDue,
			Metadata: domain.JSONMap{"weakness_score": 1.0},
		},
		{
			ID:       uuid.New(),
			WordID:   secondWordID,
			Ordinal:  2,
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			DueAt:    &soonerDue,
			Metadata: domain.JSONMap{"weakness_score": 1.0},
		},
	}, now)

	if item == nil {
		t.Fatalf("expected fallback card, got nil")
	}
	if item.WordID != secondWordID {
		t.Fatalf("expected sooner due fallback %s, got %s", secondWordID, item.WordID)
	}
}
