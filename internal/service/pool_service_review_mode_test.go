package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

func TestBuildReviewItemsAppliesProgressiveModeSelection(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	poolID := uuid.New()
	transitionWordID := uuid.New()
	weakReviewWordID := uuid.New()
	forgotMeaningWordID := uuid.New()
	alternatingReviewWordID := uuid.New()

	words := map[uuid.UUID]domain.Word{
		transitionWordID:        {ID: transitionWordID, Word: "stage"},
		weakReviewWordID:        {ID: weakReviewWordID, Word: "fragile"},
		forgotMeaningWordID:     {ID: forgotMeaningWordID, Word: "recall"},
		alternatingReviewWordID: {ID: alternatingReviewWordID, Word: "rotate"},
	}

	items := buildReviewItems(userID, poolID, []domain.UserWordState{
		{
			UserID:        userID,
			WordID:        transitionWordID,
			Status:        domain.WordStatusLearning,
			LearningStage: 3,
			Difficulty:    0.3,
			WeaknessScore: 0.2,
		},
		{
			UserID:             userID,
			WordID:             weakReviewWordID,
			Status:             domain.WordStatusReview,
			Difficulty:         0.3,
			WeaknessScore:      0.2,
			RevealMeaningCount: 4,
		},
		{
			UserID:          userID,
			WordID:          forgotMeaningWordID,
			Status:          domain.WordStatusReview,
			Difficulty:      0.3,
			WeaknessScore:   0.2,
			LastMemoryCause: domain.MemoryCauseForgotMeaning,
		},
		{
			UserID:        userID,
			WordID:        alternatingReviewWordID,
			Status:        domain.WordStatusReview,
			Difficulty:    0.3,
			WeaknessScore: 2.1,
			LastMode:      domain.ReviewModeMultipleChoice,
		},
	}, words, domain.PoolItemTypeReview, true)

	if len(items) != 4 {
		t.Fatalf("expected 4 review items, got %d", len(items))
	}

	for _, item := range items {
		switch item.WordID {
		case transitionWordID:
			if item.ReviewMode != domain.ReviewModeFillBlank && item.ReviewMode != domain.ReviewModeBuildWord {
				t.Fatalf("expected transition word %s to use a word-construction mode, got %s", item.WordID, item.ReviewMode)
			}
		case weakReviewWordID:
			if item.ReviewMode != domain.ReviewModeMultipleChoice {
				t.Fatalf("expected weak review word %s to use multiple_choice, got %s", item.WordID, item.ReviewMode)
			}
		case forgotMeaningWordID:
			if item.ReviewMode != domain.ReviewModeReveal {
				t.Fatalf("expected forgot-meaning word %s to use hidden_meaning, got %s", item.WordID, item.ReviewMode)
			}
		case alternatingReviewWordID:
			if item.ReviewMode != domain.ReviewModeReveal {
				t.Fatalf("expected alternating review word %s to use hidden_meaning, got %s", item.WordID, item.ReviewMode)
			}
		default:
			t.Fatalf("unexpected review item word %s", item.WordID)
		}
	}
}

func TestSyncPendingPoolItemRefreshesStaleRevealMode(t *testing.T) {
	t.Parallel()

	wordID := uuid.New()
	dueAt := time.Date(2026, 5, 28, 6, 9, 7, 0, time.UTC)
	item := domain.DailyLearningPoolItem{
		ID:         uuid.New(),
		WordID:     wordID,
		ItemType:   domain.PoolItemTypeReview,
		ReviewMode: domain.ReviewModeReveal,
		DueAt:      &dueAt,
		Status:     domain.PoolItemStatusPending,
		IsReview:   true,
		Metadata: domain.JSONMap{
			"weakness_score": 2.4,
		},
	}
	state := domain.UserWordState{
		WordID:             wordID,
		Status:             domain.WordStatusReview,
		LearningStage:      0,
		NextReviewAt:       &dueAt,
		LastRating:         domain.RatingEasy,
		LastMode:           domain.ReviewModeMultipleChoice,
		Difficulty:         0.1,
		WeaknessScore:      1.175,
		WrongCount:         0,
		RevealMeaningCount: 2,
		HintUsedCount:      7,
	}

	updated, changed := syncPendingPoolItem(item, state, true)
	if !changed {
		t.Fatalf("expected stale reveal-mode item to be updated")
	}
	if updated.ReviewMode != domain.ReviewModeFillBlank && updated.ReviewMode != domain.ReviewModeBuildWord {
		t.Fatalf("expected review mode to move to a word-construction mode, got %s", updated.ReviewMode)
	}
	if got := jsonMapFloat64(updated.Metadata, "weakness_score"); got != state.WeaknessScore {
		t.Fatalf("expected weakness score %.3f, got %.3f", state.WeaknessScore, got)
	}
	if updated.DueAt == nil || !updated.DueAt.Equal(dueAt) {
		t.Fatalf("expected dueAt to stay the same, got %#v", updated.DueAt)
	}
}

func TestCapListeningReviewItemsDowngradesBeyondLimit(t *testing.T) {
	t.Parallel()

	listening := func() domain.DailyLearningPoolItem {
		return domain.DailyLearningPoolItem{
			WordID:     uuid.New(),
			IsReview:   true,
			ReviewMode: domain.ReviewModeListening,
		}
	}
	items := []domain.DailyLearningPoolItem{
		listening(),
		{WordID: uuid.New(), IsReview: true, ReviewMode: domain.ReviewModeFillBlank},
		listening(),
		listening(),
		// A non-review listening item must be ignored by the cap.
		{WordID: uuid.New(), IsReview: false, ReviewMode: domain.ReviewModeListening},
	}

	capListeningReviewItems(items, dailyListeningItemLimit)

	listeningReview := 0
	for _, item := range items {
		if item.IsReview && item.ReviewMode == domain.ReviewModeListening {
			listeningReview++
		}
	}
	if listeningReview != dailyListeningItemLimit {
		t.Fatalf("expected %d listening review items after cap, got %d", dailyListeningItemLimit, listeningReview)
	}
	// The first two listening review items keep the mode; the third is downgraded.
	if items[0].ReviewMode != domain.ReviewModeListening || items[2].ReviewMode != domain.ReviewModeListening {
		t.Fatalf("expected earliest listening items to be preserved")
	}
	if items[3].ReviewMode != domain.ReviewModeFillBlank {
		t.Fatalf("expected over-limit listening item to be downgraded to fill_in_blank, got %s", items[3].ReviewMode)
	}
	if items[4].ReviewMode != domain.ReviewModeListening {
		t.Fatalf("expected non-review listening item to be untouched, got %s", items[4].ReviewMode)
	}
}
