package service

import (
	"context"
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
	finalProgress.SessionTotalCompleted = catchUpSessionTotalCap
	finalProgress.SessionComplete = true
	finalProgress.SessionCompleteReason = sessionCompleteReasonTotalCap
	item, _, reason = findNextCardForSession(items, now, finalProgress, true, 5)
	if item != nil || reason != sessionCompleteReasonTotalCap {
		t.Fatalf("final block selected item=%v reason=%q, want session complete", item, reason)
	}
}

func TestFindNextCardForSessionFallsBackToReviewWhenNewBlockHasNoNewCards(t *testing.T) {
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
	if reason != "" || item == nil || item.ItemType != domain.PoolItemTypeReview {
		t.Fatalf("selected item=%v reason=%q, want review fallback", item, reason)
	}
}

func TestBuildSessionProgressUsesOnlyCurrentSessionID(t *testing.T) {
	userID := uuid.New()
	poolID := uuid.New()
	oldReviewID := uuid.New()
	oldNewID := uuid.New()
	currentReviewID := uuid.New()
	currentNewID := uuid.New()
	now := time.Date(2026, 5, 24, 1, 0, 0, 0, time.UTC)

	service := &PoolService{
		eventRepo: &memorySessionEventRepo{
			events: []domain.LearningEvent{
				{UserID: userID, PoolItemID: &oldReviewID, EventType: domain.EventTypeReviewAnswer, EventTime: now.Add(-30 * time.Minute), ClientSessionID: "old-session"},
				{UserID: userID, PoolItemID: &oldNewID, EventType: domain.EventTypeFirstExposure, EventTime: now.Add(-25 * time.Minute), ClientSessionID: "old-session"},
				{UserID: userID, PoolItemID: &currentReviewID, EventType: domain.EventTypeReviewAnswer, EventTime: now.Add(-10 * time.Minute), ClientSessionID: "current-session"},
				{UserID: userID, PoolItemID: &currentNewID, EventType: domain.EventTypeFirstExposure, EventTime: now.Add(-5 * time.Minute), ClientSessionID: "current-session"},
			},
		},
	}

	progress, err := service.buildSessionProgress(
		t.Context(),
		userID,
		"current-session",
		domain.DailyLearningPool{
			ID:        poolID,
			LocalDate: "2026-05-24",
			Timezone:  domain.DefaultTimezone,
		},
		[]domain.DailyLearningPoolItem{
			testSessionReviewItem(oldReviewID, 1),
			testSessionNewItem(oldNewID, 2),
			testSessionReviewItem(currentReviewID, 3),
			testSessionNewItem(currentNewID, 4),
		},
		now,
	)
	if err != nil {
		t.Fatalf("buildSessionProgress() error = %v", err)
	}
	if progress.SessionTotalCompleted != 2 || progress.SessionReviewCompleted != 1 || progress.SessionNewCompleted != 1 {
		t.Fatalf("unexpected progress: %+v", progress)
	}
}

func TestActionableItemsRemainingCountsOnlySelectableWords(t *testing.T) {
	now := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	mainReviewWordID := uuid.New()
	mainNewWordID := uuid.New()
	shortTermWordID := uuid.New()
	bonusWordID := uuid.New()

	count := actionableItemsRemaining([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   mainReviewWordID,
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
		},
		{
			ID:       uuid.New(),
			WordID:   mainNewWordID,
			ItemType: domain.PoolItemTypeNew,
			Status:   domain.PoolItemStatusPending,
		},
		{
			ID:       uuid.New(),
			WordID:   shortTermWordID,
			ItemType: domain.PoolItemTypeShortTerm,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
		},
		{
			ID:            uuid.New(),
			WordID:        bonusWordID,
			ItemType:      domain.PoolItemTypeWeak,
			Status:        domain.PoolItemStatusPending,
			IsReview:      true,
			BonusPractice: true,
		},
	}, now, 4, 5)

	if count != 3 {
		t.Fatalf("actionableItemsRemaining() = %d, want 3", count)
	}
}

func TestActionableItemsRemainingExcludesNewWordsWhenDailyCapReached(t *testing.T) {
	now := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)

	count := actionableItemsRemaining([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   uuid.New(),
			ItemType: domain.PoolItemTypeNew,
			Status:   domain.PoolItemStatusPending,
		},
		{
			ID:       uuid.New(),
			WordID:   uuid.New(),
			ItemType: domain.PoolItemTypeWeak,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
		},
	}, now, 10, 10)

	if count != 0 {
		t.Fatalf("actionableItemsRemaining() = %d, want 0 when only practice remains after new cap", count)
	}
}

func TestPracticeItemsRemainingCountsOnlyPracticeWords(t *testing.T) {
	now := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)

	count := practiceItemsRemaining([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   uuid.New(),
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
		},
		{
			ID:       uuid.New(),
			WordID:   uuid.New(),
			ItemType: domain.PoolItemTypeShortTerm,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
		},
		{
			ID:            uuid.New(),
			WordID:        uuid.New(),
			ItemType:      domain.PoolItemTypeWeak,
			Status:        domain.PoolItemStatusPending,
			BonusPractice: true,
		},
	}, now)

	if count != 1 {
		t.Fatalf("practiceItemsRemaining() = %d, want 1", count)
	}
}

func TestFindNextCardForSessionSelectsShortTermReviewItems(t *testing.T) {
	now := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	futureDue := now.Add(10 * time.Minute)
	shortTermWordID := uuid.New()

	item, nextDue, reason := findNextCardForSession([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   shortTermWordID,
			ItemType: domain.PoolItemTypeShortTerm,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
		},
		{
			ID:       uuid.New(),
			WordID:   uuid.New(),
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
			DueAt:    &futureDue,
		},
	}, now, newSessionProgress("session-1"), true, 10)

	if reason != "" || item == nil || item.WordID != shortTermWordID || nextDue != nil {
		t.Fatalf("findNextCardForSession() item=%v nextDue=%v reason=%q, want short_term item as main review", item, nextDue, reason)
	}
}

func TestFindNextPracticeCardForSessionSelectsWeakPracticeItems(t *testing.T) {
	now := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	weakWordID := uuid.New()

	item, reason := findNextPracticeCardForSession([]domain.DailyLearningPoolItem{
		{
			ID:       uuid.New(),
			WordID:   uuid.New(),
			ItemType: domain.PoolItemTypeReview,
			Status:   domain.PoolItemStatusPending,
			IsReview: true,
		},
		{
			ID:       uuid.New(),
			WordID:   weakWordID,
			ItemType: domain.PoolItemTypeWeak,
			Status:   domain.PoolItemStatusPending,
			BonusPractice: true,
		},
	}, now, newSessionProgress("practice-session"))

	if reason != "" || item == nil || item.WordID != weakWordID {
		t.Fatalf("findNextPracticeCardForSession() item=%v reason=%q, want weak practice item", item, reason)
	}
}

func TestFindNextCardForCompletedSessionStillExposesNearestNextDue(t *testing.T) {
	now := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	nextDue := now.Add(12 * time.Minute)

	events := make([]domain.LearningEvent, 0, catchUpSessionTotalCap)
	for i := 0; i < catchUpSessionTotalCap; i++ {
		events = append(events, domain.LearningEvent{
			UserID:          uuid.New(),
			EventType:       domain.EventTypeReviewAnswer,
			EventTime:       now.Add(-time.Duration(i+1) * time.Minute),
			ClientSessionID: "session-1",
		})
	}
	service := &PoolService{
		eventRepo: &memorySessionEventRepo{events: events},
	}
	card, err := service.nextCardFromView(
		context.Background(),
		uuid.New(),
		DailyPoolView{
			Pool: domain.DailyLearningPool{
				LocalDate: "2026-05-25",
				Timezone:  domain.DefaultTimezone,
			},
			Items: []domain.DailyLearningPoolItem{
				{
					ID:       uuid.New(),
					WordID:   uuid.New(),
					ItemType: domain.PoolItemTypeReview,
					Status:   domain.PoolItemStatusPending,
					DueAt:    &nextDue,
				},
			},
		},
		now,
		"session-1",
		10,
		false,
	)
	if err != nil {
		t.Fatalf("nextCardFromView() error = %v", err)
	}
	if card.NextDueAt == nil || !card.NextDueAt.Equal(nextDue) {
		t.Fatalf("nextCardFromView() nextDueAt = %v, want %v", card.NextDueAt, nextDue)
	}
	if !card.SessionComplete {
		t.Fatalf("nextCardFromView() sessionComplete = false, want true")
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

func testSessionReviewItem(id uuid.UUID, ordinal int) domain.DailyLearningPoolItem {
	return domain.DailyLearningPoolItem{
		ID:       id,
		WordID:   uuid.New(),
		ItemType: domain.PoolItemTypeReview,
		Ordinal:  ordinal,
		Status:   domain.PoolItemStatusPending,
		IsReview: true,
	}
}

func testSessionNewItem(id uuid.UUID, ordinal int) domain.DailyLearningPoolItem {
	return domain.DailyLearningPoolItem{
		ID:       id,
		WordID:   uuid.New(),
		ItemType: domain.PoolItemTypeNew,
		Ordinal:  ordinal,
		Status:   domain.PoolItemStatusPending,
		IsReview: false,
	}
}

type memorySessionEventRepo struct {
	events []domain.LearningEvent
}

func (m *memorySessionEventRepo) Insert(ctx context.Context, event domain.LearningEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *memorySessionEventRepo) ListRecentByPoolItem(ctx context.Context, itemID uuid.UUID) ([]domain.LearningEvent, error) {
	return nil, nil
}

func (m *memorySessionEventRepo) ListByUserTimeRange(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time) ([]domain.LearningEvent, error) {
	out := make([]domain.LearningEvent, 0, len(m.events))
	for _, event := range m.events {
		if event.UserID != userID {
			continue
		}
		if event.EventTime.Before(start) || !event.EventTime.Before(end) {
			continue
		}
		out = append(out, event)
	}
	return out, nil
}
