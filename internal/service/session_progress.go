package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const (
	catchUpDailyReviewCap      = 40
	catchUpSessionReviewRunCap = 5
	catchUpSessionNewRunCap    = 5
	catchUpSessionTotalCap     = 15
)

const (
	sessionCompleteReasonTotalCap = "session_total_cap_reached"
	sessionCompleteReasonNoNew    = "new_block_no_new_available"
	sessionCompleteReasonNoReview = "review_block_no_review_available"
	sessionCompleteReasonNoCards  = "no_cards_available"
	sessionCompleteReasonDailyCap = "daily_review_cap_reached"
)

type learningEventTimeRangeRepository interface {
	ListByUserTimeRange(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time) ([]domain.LearningEvent, error)
}

type completedCardKind string

const (
	completedCardKindReview completedCardKind = "review"
	completedCardKindNew    completedCardKind = "new"
)

type sessionProgress struct {
	SessionID              string
	DailyReviewCap         int
	DailyReviewCompleted   int
	DailyNewCompleted      int
	SessionTotalCompleted  int
	SessionReviewCompleted int
	SessionNewCompleted    int
	SessionComplete        bool
	SessionCompleteReason  string
	PreferredKind          completedCardKind
}

func newSessionProgress(sessionID string) sessionProgress {
	return sessionProgress{
		SessionID:      sessionID,
		DailyReviewCap: catchUpDailyReviewCap,
		PreferredKind:  completedCardKindReview,
	}
}

func (s *PoolService) buildSessionProgress(
	ctx context.Context,
	userID uuid.UUID,
	sessionID string,
	pool domain.DailyLearningPool,
	items []domain.DailyLearningPoolItem,
	now time.Time,
) (sessionProgress, error) {
	progress := newSessionProgress(sessionID)
	repo, ok := s.eventRepo.(learningEventTimeRangeRepository)
	if !ok {
		return progress, nil
	}

	_, startUTC, endUTC, _, err := domain.BoundsForLocalDate(now, pool.Timezone)
	if err != nil {
		return sessionProgress{}, err
	}
	dayEvents, err := repo.ListByUserTimeRange(ctx, userID, startUTC, endUTC)
	if err != nil {
		return sessionProgress{}, err
	}

	itemKinds := buildPoolItemKindMap(items)
	undoneClientEventIDs := collectUndoneClientEventIDs(dayEvents)
	dayKinds := completedKindsForEvents(dayEvents, itemKinds, undoneClientEventIDs, "")
	for _, kind := range dayKinds {
		switch kind {
		case completedCardKindReview:
			progress.DailyReviewCompleted++
		case completedCardKindNew:
			progress.DailyNewCompleted++
		}
	}

	if sessionID == "" {
		return progress, nil
	}

	sessionKinds := completedKindsForEvents(dayEvents, itemKinds, undoneClientEventIDs, sessionID)
	for _, kind := range sessionKinds {
		progress.SessionTotalCompleted++
		switch kind {
		case completedCardKindReview:
			progress.SessionReviewCompleted++
		case completedCardKindNew:
			progress.SessionNewCompleted++
		}
	}
	progress.PreferredKind, progress.SessionComplete, progress.SessionCompleteReason = nextSessionKind(sessionKinds)
	return progress, nil
}

func buildPoolItemKindMap(items []domain.DailyLearningPoolItem) map[uuid.UUID]completedCardKind {
	out := make(map[uuid.UUID]completedCardKind, len(items))
	for _, item := range items {
		if IsReviewPracticeItem(item) {
			out[item.ID] = completedCardKindReview
			continue
		}
		if item.ItemType == domain.PoolItemTypeNew {
			out[item.ID] = completedCardKindNew
		}
	}
	return out
}

func collectUndoneClientEventIDs(events []domain.LearningEvent) map[string]struct{} {
	out := map[string]struct{}{}
	for _, event := range events {
		if event.EventType != domain.EventTypeAnswerUndo {
			continue
		}
		raw, ok := event.Payload["undone_client_event_id"].(string)
		if !ok || raw == "" {
			continue
		}
		out[raw] = struct{}{}
	}
	return out
}

func completedKindsForEvents(
	events []domain.LearningEvent,
	itemKinds map[uuid.UUID]completedCardKind,
	undoneClientEventIDs map[string]struct{},
	sessionID string,
) []completedCardKind {
	kinds := make([]completedCardKind, 0, len(events))
	for _, event := range events {
		if sessionID != "" && event.ClientSessionID != sessionID {
			continue
		}
		if event.ClientEventID != "" {
			if _, undone := undoneClientEventIDs[event.ClientEventID]; undone {
				continue
			}
		}
		kind, ok := completedKindForEvent(event, itemKinds)
		if !ok {
			continue
		}
		kinds = append(kinds, kind)
	}
	return kinds
}

func completedKindForEvent(event domain.LearningEvent, itemKinds map[uuid.UUID]completedCardKind) (completedCardKind, bool) {
	switch event.EventType {
	case domain.EventTypeFirstExposure:
		if event.PoolItemID != nil {
			if kind, ok := itemKinds[*event.PoolItemID]; ok {
				return kind, true
			}
		}
		return completedCardKindNew, true
	case domain.EventTypeReviewAnswer, domain.EventTypeBonusPractice, domain.EventTypeMode4Passage:
		return completedCardKindReview, true
	default:
		return "", false
	}
}

func nextSessionKind(kinds []completedCardKind) (completedCardKind, bool, string) {
	if len(kinds) >= catchUpSessionTotalCap {
		return "", true, sessionCompleteReasonTotalCap
	}
	firstNew := -1
	for i, kind := range kinds {
		if kind == completedCardKindNew {
			firstNew = i
			break
		}
	}
	if firstNew < 0 {
		if countKind(kinds, completedCardKindReview) < catchUpSessionReviewRunCap {
			return completedCardKindReview, false, ""
		}
		return completedCardKindNew, false, ""
	}

	idx := firstNew
	newBlockCount := 0
	for idx < len(kinds) && kinds[idx] == completedCardKindNew {
		newBlockCount++
		idx++
	}
	if idx == len(kinds) && newBlockCount < catchUpSessionNewRunCap {
		return completedCardKindNew, false, ""
	}

	finalReviewCount := countKind(kinds[idx:], completedCardKindReview)
	if finalReviewCount < catchUpSessionReviewRunCap {
		return completedCardKindReview, false, ""
	}
	return "", true, sessionCompleteReasonTotalCap
}

func countKind(kinds []completedCardKind, target completedCardKind) int {
	count := 0
	for _, kind := range kinds {
		if kind == target {
			count++
		}
	}
	return count
}

func IsReviewPracticeItem(item domain.DailyLearningPoolItem) bool {
	if item.BonusPractice {
		return true
	}
	return item.IsReview || item.ItemType == domain.PoolItemTypeReview ||
		item.ItemType == domain.PoolItemTypeShortTerm ||
		item.ItemType == domain.PoolItemTypeWeak
}

func totalReviewPracticeItems(items []domain.DailyLearningPoolItem) int {
	count := 0
	for _, item := range items {
		if IsReviewPracticeItem(item) {
			count++
		}
	}
	return count
}

func totalDueReviewPracticeItems(shortTermStates []domain.UserWordState, reviewStates []domain.UserWordState, weakStates []domain.UserWordState) int {
	return len(shortTermStates) + len(reviewStates) + len(weakStates)
}
