package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestFindNextCardForSessionFollowsReviewNewReviewBlocks(t *testing.T) {
	now := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	items := []domain.DailyLearningPoolItem{
		testSessionPoolItem(domain.PoolItemTypeReview, 1),
		testSessionPoolItem(domain.PoolItemTypeReview, 2),
		testSessionPoolItem(domain.PoolItemTypeNew, 3),
	}

	firstProgress := newSessionProgress("session-1")
	item, _, reason := findNextCardForSession(items, now, firstProgress, true, 5)
	if reason != "" || item == nil || item.ItemType != domain.PoolItemTypeReview {
		t.Fatalf("first block selected item=%v reason=%q, want review", item, reason)
	}

	newProgress := newSessionProgress("session-1")
	newProgress.SessionReviewCompleted = catchUpSessionReviewRunCap
	newProgress.SessionTotalCompleted = catchUpSessionReviewRunCap
	newProgress.PreferredKind = completedCardKindNew
	item, _, reason = findNextCardForSession(items, now, newProgress, true, 5)
	if reason != "" || item == nil || item.ItemType != domain.PoolItemTypeNew {
		t.Fatalf("new block selected item=%v reason=%q, want new", item, reason)
	}

	finalProgress := newSessionProgress("session-1")
	finalProgress.SessionReviewCompleted = catchUpSessionReviewRunCap
	finalProgress.SessionNewCompleted = catchUpSessionNewRunCap
	finalProgress.SessionTotalCompleted = catchUpSessionReviewRunCap + catchUpSessionNewRunCap
	finalProgress.PreferredKind = completedCardKindReview
	item, _, reason = findNextCardForSession(items, now, finalProgress, true, 5)
	if reason != "" || item == nil || item.ItemType != domain.PoolItemTypeReview {
		t.Fatalf("final block selected item=%v reason=%q, want review", item, reason)
	}
}

func TestFindNextCardForSessionCompletesWhenNewBlockHasNoNewCards(t *testing.T) {
	now := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	progress := newSessionProgress("session-1")
	progress.SessionReviewCompleted = catchUpSessionReviewRunCap
	progress.SessionTotalCompleted = catchUpSessionReviewRunCap
	progress.PreferredKind = completedCardKindNew

	item, _, reason := findNextCardForSession(
		[]domain.DailyLearningPoolItem{testSessionPoolItem(domain.PoolItemTypeReview, 1)},
		now,
		progress,
		true,
		5,
	)
	if item != nil || reason != sessionCompleteReasonNoNew {
		t.Fatalf("selected item=%v reason=%q, want no-new complete", item, reason)
	}
}

func TestFindNextCardForSessionBlocksReviewAfterDailyCap(t *testing.T) {
	now := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	progress := newSessionProgress("session-1")
	progress.DailyReviewCompleted = catchUpDailyReviewCap

	item, _, reason := findNextCardForSession(
		[]domain.DailyLearningPoolItem{testSessionPoolItem(domain.PoolItemTypeReview, 1)},
		now,
		progress,
		true,
		5,
	)
	if item != nil || reason != sessionCompleteReasonDailyCap {
		t.Fatalf("selected item=%v reason=%q, want daily-cap complete", item, reason)
	}
}

func TestCompletedKindsForEventsSkipsUndoneAnswerEvent(t *testing.T) {
	poolItemID := uuid.New()
	events := []domain.LearningEvent{
		{
			PoolItemID:      &poolItemID,
			EventType:       domain.EventTypeReviewAnswer,
			ClientEventID:   "answer-1",
			ClientSessionID: "session-1",
		},
		{
			EventType: domain.EventTypeAnswerUndo,
			Payload: domain.JSONMap{
				"undone_client_event_id": "answer-1",
			},
		},
	}
	itemKinds := map[uuid.UUID]completedCardKind{
		poolItemID: completedCardKindReview,
	}

	kinds := completedKindsForEvents(events, itemKinds, collectUndoneClientEventIDs(events), "session-1")
	if len(kinds) != 0 {
		t.Fatalf("completedKindsForEvents() = %v, want empty after undo", kinds)
	}
}

func testSessionPoolItem(itemType domain.PoolItemType, ordinal int) domain.DailyLearningPoolItem {
	return domain.DailyLearningPoolItem{
		ID:       uuid.New(),
		WordID:   uuid.New(),
		ItemType: itemType,
		Ordinal:  ordinal,
		Status:   domain.PoolItemStatusPending,
		IsReview: itemType != domain.PoolItemTypeNew,
	}
}
