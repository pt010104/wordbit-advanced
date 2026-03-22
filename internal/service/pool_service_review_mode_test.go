package service

import (
	"testing"

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
			WeaknessScore: 1.8,
			LastMode:      domain.ReviewModeMultipleChoice,
		},
	}, words, domain.PoolItemTypeReview, true)

	if len(items) != 4 {
		t.Fatalf("expected 4 review items, got %d", len(items))
	}

	wantModes := map[uuid.UUID]domain.ReviewMode{
		transitionWordID:        domain.ReviewModeFillBlank,
		weakReviewWordID:        domain.ReviewModeMultipleChoice,
		forgotMeaningWordID:     domain.ReviewModeReveal,
		alternatingReviewWordID: domain.ReviewModeReveal,
	}

	for _, item := range items {
		if item.ReviewMode != wantModes[item.WordID] {
			t.Fatalf("expected word %s to use %s, got %s", item.WordID, wantModes[item.WordID], item.ReviewMode)
		}
	}
}
